//go:build darwin

package scan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The darwin walker reads each directory with getattrlistbulk(2), which returns
// name, type, ids, link count and sizes for a whole batch of entries in one
// syscall. That removes the per-file lstat that dominates fastwalk on macOS:
// fastwalk's DirEntry.Info() resolves a full path and takes a vnode lock for
// every entry, which showed up as ~10 s of system time on ~/Documents.
//
// Symlink-following walks keep using fastwalk, whose loop detection we do not
// reimplement here.

const (
	attrBitMapCount = 5

	cmnReturned = 0x80000000
	cmnName     = 0x00000001
	cmnDevID    = 0x00000002
	cmnObjType  = 0x00000008
	cmnModTime  = 0x00000400
	cmnFileID   = 0x02000000
	cmnError    = 0x20000000

	fileLinkCount = 0x00000001
	fileAllocSize = 0x00000004
	fileDataLen   = 0x00000200

	dirAllocSize = 0x00000008
	dirDataLen   = 0x00000020

	vdir = 2
	vlnk = 5

	// bulkBuf is the per-worker attribute buffer, reused across directories so
	// the walk does not zero a fresh buffer per directory.
	bulkBufSize = 256 << 10
	bulkBufMax  = 4 << 20
)

var (
	errUnsupported = errors.New("getattrlistbulk unsupported")
	errStopped     = errors.New("scan stopped")
	errBadRecord   = errors.New("malformed getattrlistbulk record")
)

// attrlist mirrors struct attrlist from <sys/attr.h>.
type attrlist struct {
	bitmapcount uint16
	reserved    uint16
	commonattr  uint32
	volattr     uint32
	dirattr     uint32
	fileattr    uint32
	forkattr    uint32
}

// walk is the traversal driver on darwin.
func walk(root string, opts Options, fn fs.WalkDirFunc) error {
	if opts.FollowSymlinks {
		return fastwalkWalk(root, opts, fn)
	}
	return bulkWalk(root, opts, fn)
}

// batchSupported reports that this platform has a folded per-directory driver.
func batchSupported() bool { return true }

// walkBatched drives a measure-only walk that folds each directory into one
// event. Implemented here; other platforms have no such driver.
func walkBatched(root string, opts Options, send func(event) error) error {
	return bulkWalkBatch(root, opts, send)
}

// bulkWorkers returns the traversal worker count: the requested override, or a
// tier that mirrors fastwalk's darwin schedule but caps the top rung at 8.
// More workers than 8 measured slower on APFS.
func bulkWorkers(requested int) int {
	if requested > 0 {
		return requested
	}
	switch n := runtime.GOMAXPROCS(0); {
	case n <= 8:
		return 4
	case n <= 10:
		return 6
	default:
		return 8
	}
}

func bulkWalk(root string, opts Options, fn fs.WalkDirFunc) error {
	root = filepath.Clean(root)
	ri, err := os.Lstat(root)
	if err != nil {
		return err
	}

	w := &bulkWalker{fn: fn, q: newDirQueue()}
	descend, stop := w.emit(root, infoEntry{name: filepath.Base(root), fi: ri}, nil)
	if stop || !descend {
		return w.result()
	}

	w.q.push(root)
	var wg sync.WaitGroup
	for i := 0; i < bulkWorkers(opts.Workers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.worker()
		}()
	}
	wg.Wait()
	return w.result()
}

// bulkWalkBatch is bulkWalk for a measure-only consumer: it emits the root's
// own entry and then one folded event per directory, never a path per file.
func bulkWalkBatch(root string, opts Options, send func(event) error) error {
	root = filepath.Clean(root)
	ri, err := os.Lstat(root)
	if err != nil {
		return err
	}
	dev, ino, hasID := fileID(ri)
	apparent, allocated, nlink := statSizes(ri)
	if err := send(event{
		kind:     evEntry,
		path:     root,
		own:      allocated,
		apparent: apparent,
		mtime:    ri.ModTime().Unix(),
		nlink:    nlink,
		dev:      dev,
		ino:      ino,
		hasID:    hasID,
		isDir:    ri.IsDir(),
	}); err != nil {
		return nil
	}
	if !ri.IsDir() {
		return nil
	}

	w := &bulkWalker{
		send:       send,
		opts:       opts,
		rootDev:    dev,
		rootHasID:  hasID,
		hasExclude: len(opts.Exclude) > 0,
		q:          newDirQueue(),
	}
	w.q.push(root)
	var wg sync.WaitGroup
	for i := 0; i < bulkWorkers(opts.Workers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.batchWorker()
		}()
	}
	wg.Wait()
	return w.result()
}

type bulkWalker struct {
	fn         fs.WalkDirFunc
	send       func(event) error // set in batch mode; nil for per-entry walks
	opts       Options
	rootDev    uint64
	rootHasID  bool
	hasExclude bool
	q          *dirQueue
	stopped    atomic.Bool
	mu         sync.Mutex
	err        error
}

func (w *bulkWalker) result() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *bulkWalker) stop() {
	w.stopped.Store(true)
	w.q.stop()
}

func (w *bulkWalker) fail(err error) {
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
	w.stop()
}

// emit calls fn for one entry and translates its return into control flow:
// descend is true when a directory should be entered, stop is true when the
// walk is over (fs.SkipAll or a fatal error).
func (w *bulkWalker) emit(path string, de fs.DirEntry, walkErr error) (descend, stop bool) {
	err := w.fn(path, de, walkErr)
	switch {
	case err == nil:
		return de != nil && de.IsDir(), false
	case errors.Is(err, fs.SkipDir):
		return false, false
	case errors.Is(err, fs.SkipAll):
		w.stop()
		return false, true
	default:
		w.fail(err)
		return false, true
	}
}

func (w *bulkWalker) worker() {
	buf := make([]byte, bulkBufSize)
	for {
		dir, ok := w.q.pop()
		if !ok {
			return
		}
		if !w.stopped.Load() {
			buf = w.scanDir(dir, buf)
		}
		w.q.finish()
	}
}

// batchWorker folds each directory's entries into one message. The batch is
// worker-local, so it needs no locking and is never shared with the consumer
// after it is sent.
func (w *bulkWalker) batchWorker() {
	buf := make([]byte, bulkBufSize)
	for {
		dir, ok := w.q.pop()
		if !ok {
			return
		}
		if !w.stopped.Load() {
			buf = w.scanDirBatch(dir, buf, &dirBatch{})
		}
		w.q.finish()
	}
}

func (w *bulkWalker) scanDirBatch(dir string, buf []byte, b *dirBatch) []byte {
	fd, err := openDir(dir)
	if err != nil {
		w.report(dir)
		return buf
	}
	defer syscall.Close(fd)

	buf, err = w.bulkRead(fd, dir, buf, b)
	switch {
	case errors.Is(err, errUnsupported):
		w.fallbackBatch(dir, b)
	case err != nil && !errors.Is(err, errStopped):
		w.report(dir)
	}
	w.sendBatch(dir, b)
	return buf
}

func (w *bulkWalker) sendBatch(dir string, b *dirBatch) {
	if w.stopped.Load() {
		return
	}
	if err := w.send(event{kind: evDir, path: dir, batch: b}); err != nil {
		w.stop()
	}
}

// report emits a path-level error (used in batch mode, where there is no fn).
// Only the path is kept, matching how per-entry error events carry it.
func (w *bulkWalker) report(path string) {
	if w.stopped.Load() {
		return
	}
	if err := w.send(event{kind: evError, path: path}); err != nil {
		w.stop()
	}
}

func (w *bulkWalker) scanDir(dir string, buf []byte) []byte {
	fd, err := openDir(dir)
	if err != nil {
		w.emit(dir, nil, err)
		return buf
	}
	defer syscall.Close(fd)

	buf, err = w.bulkRead(fd, dir, buf, nil)
	switch {
	case errors.Is(err, errUnsupported):
		// Some network and FUSE mounts lack getattrlistbulk; read them the
		// ordinary way rather than dropping the subtree.
		w.fallbackRead(dir)
	case err != nil && !errors.Is(err, errStopped):
		w.emit(dir, nil, err)
	}
	return buf
}

// bulkRead drains a directory through getattrlistbulk, parsing each batch. It
// returns the (possibly grown) buffer for reuse. b is non-nil in batch mode.
func (w *bulkWalker) bulkRead(fd int, dir string, buf []byte, b *dirBatch) ([]byte, error) {
	// Common, directory and file attributes are packed in that group order, and
	// within each group in increasing bit order; see handleRecord.
	al := attrlist{
		bitmapcount: attrBitMapCount,
		commonattr:  cmnReturned | cmnName | cmnDevID | cmnObjType | cmnModTime | cmnFileID | cmnError,
		dirattr:     dirAllocSize | dirDataLen,
		fileattr:    fileLinkCount | fileAllocSize | fileDataLen,
	}
	for {
		if w.stopped.Load() {
			return buf, errStopped
		}
		n, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTBULK,
			uintptr(fd), uintptr(unsafe.Pointer(&al)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
		switch errno {
		case 0:
		case unix.ENOTSUP:
			return buf, errUnsupported
		case unix.ERANGE:
			if len(buf) >= bulkBufMax {
				return buf, &os.PathError{Op: "getattrlistbulk", Path: dir, Err: errno}
			}
			buf = make([]byte, len(buf)*2)
			if _, err := unix.Seek(fd, 0, 0); err != nil {
				return buf, &os.PathError{Op: "seek", Path: dir, Err: err}
			}
			continue
		default:
			return buf, &os.PathError{Op: "getattrlistbulk", Path: dir, Err: errno}
		}
		if n == 0 {
			return buf, nil
		}
		if err := w.parseRecords(buf, int(n), dir, b); err != nil {
			return buf, err
		}
	}
}

// parseRecords walks the n records packed into buf, stepping by each record's
// leading length field. n is an entry count, not a byte length.
func (w *bulkWalker) parseRecords(buf []byte, n int, dir string, b *dirBatch) error {
	off := 0
	for i := 0; i < n; i++ {
		if off+4 > len(buf) {
			return &os.PathError{Op: "getattrlistbulk", Path: dir, Err: errBadRecord}
		}
		length := int(le32(buf, off))
		if length < 4 || off+length > len(buf) {
			return &os.PathError{Op: "getattrlistbulk", Path: dir, Err: errBadRecord}
		}
		if w.handleRecord(buf[off:off+length], dir, b) {
			return errStopped
		}
		off += length
	}
	return nil
}

// handleRecord parses one packed entry and emits it. It returns true when the
// walk should stop. Layout: u32 length, the 20-byte returned attribute_set, an
// optional u32 error (ATTR_CMN_ERROR is reported right after the set), then the
// common, directory and file attributes that were returned, each group in
// increasing bit order. The name is a variable-length blob referenced by an
// attrreference_t and is read by absolute offset, so trailing padding is fine.
func (w *bulkWalker) handleRecord(rec []byte, dir string, b *dirBatch) bool {
	if len(rec) < 24 {
		return false
	}
	retCommon := le32(rec, 4)
	p := 24

	var entryErr uint32
	if retCommon&cmnError != 0 {
		entryErr = le32(rec, p)
		p += 4
	}

	var name string
	if retCommon&cmnName != 0 && p+8 <= len(rec) {
		refPos := p
		dataOff := int(int32(le32(rec, p)))
		nameLen := int(le32(rec, p+4))
		p += 8
		if start := refPos + dataOff; nameLen > 0 && start >= 0 && start+nameLen <= len(rec) {
			name = string(rec[start : start+nameLen-1])
		}
	}

	var dev uint32
	if retCommon&cmnDevID != 0 && p+4 <= len(rec) {
		dev = le32(rec, p)
		p += 4
	}
	var objType uint32
	if retCommon&cmnObjType != 0 && p+4 <= len(rec) {
		objType = le32(rec, p)
		p += 4
	}
	var mtime int64
	if retCommon&cmnModTime != 0 && p+16 <= len(rec) {
		mtime = int64(le64(rec, p))
		p += 16 // timespec: tv_sec + tv_nsec
	}
	var ino uint64
	if retCommon&cmnFileID != 0 && p+8 <= len(rec) {
		ino = le64(rec, p)
		p += 8
	}

	// Directory group precedes the file group; a given entry returns only one.
	var alloc, data int64
	var nlink uint32
	retDir := le32(rec, 12)
	retFile := le32(rec, 16)
	if retDir&dirAllocSize != 0 && p+8 <= len(rec) {
		alloc = int64(le64(rec, p))
		p += 8
	}
	if retDir&dirDataLen != 0 && p+8 <= len(rec) {
		data = int64(le64(rec, p))
		p += 8
	}
	if retFile&fileLinkCount != 0 && p+4 <= len(rec) {
		nlink = le32(rec, p)
		p += 4
	}
	if retFile&fileAllocSize != 0 && p+8 <= len(rec) {
		alloc = int64(le64(rec, p))
		p += 8
	}
	if retFile&fileDataLen != 0 && p+8 <= len(rec) {
		data = int64(le64(rec, p))
		p += 8
	}

	if b != nil {
		return w.foldRecord(dir, name, objType, dev, ino, nlink, alloc, data, entryErr, b)
	}

	path := joinPath(dir, name)
	if entryErr != 0 {
		w.emit(path, nil, syscall.Errno(entryErr))
		return w.stopped.Load()
	}

	mode := modeForType(objType)
	de := &bulkDirEntry{name: name, typ: mode, info: bulkInfo{
		name:  name,
		mode:  mode,
		size:  data,
		mtime: time.Unix(mtime, 0),
		stat: syscall.Stat_t{
			Dev:    int32(dev),
			Ino:    ino,
			Nlink:  uint16(nlink),
			Size:   data,
			Blocks: alloc / 512,
		},
	}}
	descend, stop := w.emit(path, de, nil)
	if descend {
		w.q.push(path)
	}
	return stop
}

// fallbackRead reads a directory with os.ReadDir for filesystems where
// getattrlistbulk is not supported. os.DirEntry.Info() stats lazily, so the
// totals stay correct.
func (w *bulkWalker) fallbackRead(dir string) {
	des, err := os.ReadDir(dir)
	if err != nil {
		w.emit(dir, nil, err)
		return
	}
	for _, de := range des {
		path := joinPath(dir, de.Name())
		descend, stop := w.emit(path, de, nil)
		if descend {
			w.q.push(path)
		}
		if stop {
			return
		}
	}
}

// foldRecord accumulates one entry into its directory's batch. A path is built
// only for directories (to descend into) or when exclude patterns are set, so a
// measure-only scan never allocates a path per file.
func (w *bulkWalker) foldRecord(dir, name string, objType uint32, dev uint32, ino uint64, nlink uint32, alloc, data int64, entryErr uint32, b *dirBatch) bool {
	isDir := objType == vdir
	if entryErr != 0 {
		w.report(joinPath(dir, name))
		return w.stopped.Load()
	}

	var path string
	if isDir || w.hasExclude {
		path = joinPath(dir, name)
	}
	if skipEntry(w.opts, name, path) {
		return false
	}
	if isDir && w.opts.OneFileSystem && w.rootHasID && uint64(int32(dev)) != w.rootDev {
		return false
	}

	if isDir {
		b.dirs++
		b.alloc += alloc
		b.apparent += data
		w.q.push(path)
		return false
	}
	b.files++
	if nlink > 1 {
		b.links = append(b.links, linkRec{dev: uint64(int32(dev)), ino: ino, nlink: nlink, alloc: alloc, apparent: data})
	} else {
		b.alloc += alloc
		b.apparent += data
	}
	return false
}

// fallbackBatch is fallbackRead for measure mode: it folds the same totals
// without emitting per-entry events.
func (w *bulkWalker) fallbackBatch(dir string, b *dirBatch) {
	des, err := os.ReadDir(dir)
	if err != nil {
		w.report(dir)
		return
	}
	for _, de := range des {
		name := de.Name()
		isDir := de.IsDir()
		var path string
		if isDir || w.hasExclude {
			path = joinPath(dir, name)
		}
		if skipEntry(w.opts, name, path) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			w.report(joinPath(dir, name))
			continue
		}
		apparent, allocated, nlink := statSizes(fi)
		dev, ino, hasID := fileID(fi)
		if isDir {
			if w.opts.OneFileSystem && w.rootHasID && hasID && dev != w.rootDev {
				continue
			}
			b.dirs++
			b.alloc += allocated
			b.apparent += apparent
			w.q.push(path)
			continue
		}
		b.files++
		if nlink > 1 && hasID {
			b.links = append(b.links, linkRec{dev: dev, ino: ino, nlink: nlink, alloc: allocated, apparent: apparent})
		} else {
			b.alloc += allocated
			b.apparent += apparent
		}
	}
}

// openDir opens a directory ourselves rather than through libc opendir, whose
// retry loop is not interruptible. EINTR is retried a bounded number of times.
func openDir(path string) (int, error) {
	const attempts = 3
	for i := 0; i < attempts; i++ {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if err != unix.EINTR {
			return -1, &os.PathError{Op: "open", Path: path, Err: err}
		}
	}
	return -1, &os.PathError{Op: "open", Path: path, Err: unix.EINTR}
}

func modeForType(t uint32) fs.FileMode {
	switch t {
	case vdir:
		return fs.ModeDir
	case 3, 4:
		return fs.ModeDevice
	case vlnk:
		return fs.ModeSymlink
	case 6:
		return fs.ModeSocket
	case 7:
		return fs.ModeNamedPipe
	default:
		return 0
	}
}

func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

func le32(b []byte, o int) uint32 {
	return uint32(b[o]) | uint32(b[o+1])<<8 | uint32(b[o+2])<<16 | uint32(b[o+3])<<24
}

func le64(b []byte, o int) uint64 {
	return uint64(le32(b, o)) | uint64(le32(b, o+4))<<32
}

// dirQueue is a LIFO work queue shared by the workers, which both consume and
// produce. An in-flight counter gives termination: empty stack plus zero
// in-flight means every directory has been drained.
type dirQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	stack    []string
	inflight int
	stopped  bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *dirQueue) push(p string) {
	q.mu.Lock()
	q.stack = append(q.stack, p)
	q.inflight++
	q.mu.Unlock()
	q.cond.Signal()
}

func (q *dirQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.stack) == 0 && q.inflight > 0 && !q.stopped {
		q.cond.Wait()
	}
	if q.stopped || len(q.stack) == 0 {
		return "", false
	}
	p := q.stack[len(q.stack)-1]
	q.stack = q.stack[:len(q.stack)-1]
	return p, true
}

func (q *dirQueue) finish() {
	q.mu.Lock()
	q.inflight--
	if q.inflight == 0 {
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

func (q *dirQueue) stop() {
	q.mu.Lock()
	q.stopped = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// bulkDirEntry implements fs.DirEntry for one getattrlistbulk record.
type bulkDirEntry struct {
	name string
	typ  fs.FileMode
	info bulkInfo
}

func (d *bulkDirEntry) Name() string               { return d.name }
func (d *bulkDirEntry) IsDir() bool                { return d.typ.IsDir() }
func (d *bulkDirEntry) Type() fs.FileMode          { return d.typ }
func (d *bulkDirEntry) Info() (fs.FileInfo, error) { return &d.info, nil }

// bulkInfo implements fs.FileInfo over a getattrlistbulk record. Sys returns a
// synthesised Stat_t so the shared statSizes/fileID helpers work unchanged;
// st_blocks is in 512-byte units, matching ATTR_FILE_ALLOCSIZE / 512.
type bulkInfo struct {
	name  string
	mode  fs.FileMode
	size  int64
	mtime time.Time
	stat  syscall.Stat_t
}

func (i *bulkInfo) Name() string       { return i.name }
func (i *bulkInfo) Size() int64        { return i.size }
func (i *bulkInfo) Mode() fs.FileMode  { return i.mode }
func (i *bulkInfo) ModTime() time.Time { return i.mtime }
func (i *bulkInfo) IsDir() bool        { return i.mode.IsDir() }
func (i *bulkInfo) Sys() any           { return &i.stat }

// infoEntry adapts an fs.FileInfo (used for the root) to fs.DirEntry.
type infoEntry struct {
	name string
	fi   fs.FileInfo
}

func (e infoEntry) Name() string               { return e.name }
func (e infoEntry) IsDir() bool                { return e.fi.IsDir() }
func (e infoEntry) Type() fs.FileMode          { return e.fi.Mode().Type() }
func (e infoEntry) Info() (fs.FileInfo, error) { return e.fi, nil }
