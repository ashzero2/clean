// Package scan walks a directory tree and accounts for disk usage.
//
// fastwalk calls the walk callback from several goroutines at once, so the
// callback does only the parallelisable work — the stat syscalls — and hands a
// small immutable event to a bounded channel. A single consumer goroutine owns
// all mutable state, so the hot path needs no locks. The bounded channel applies
// backpressure, and every send selects on ctx.Done() so a cancelled walk cannot
// leave a producer blocked forever.
//
// fastwalk preserves filepath.WalkDir ordering — a directory's callback runs
// before its entries' — so a parent's index always precedes its children's.
// tree.Rollup depends on that.
package scan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/rahul-17280/sweep/internal/tree"
)

const (
	eventBuffer = 8192
	// maxErrorPaths bounds how many failing paths are retained for display.
	maxErrorPaths = 64
	// defaultStallTimeout is how long a scan may make no progress before the
	// watchdog aborts it: long enough not to trip on one huge directory, short
	// enough that a stuck syscall cannot freeze the UI for minutes.
	defaultStallTimeout = 45 * time.Second
	// stallPollInterval is how often the watchdog checks for progress.
	stallPollInterval = time.Second
)

// noteError counts a failing path, keeping the first few for display so a short
// total can be explained rather than shown as a bare count.
func (s *Sizes) noteError(path string) {
	s.Errors++
	if path != "" && len(s.ErrorPaths) < maxErrorPaths {
		s.ErrorPaths = append(s.ErrorPaths, path)
	}
}

type Options struct {
	FollowSymlinks bool
	OneFileSystem  bool
	NoHidden       bool
	// Exclude holds glob patterns matched against an entry's base name and its
	// full path; a matching directory is not descended into.
	Exclude []string

	// Progress, when non-nil, is called from the single consumer goroutine.
	Progress      func(Progress)
	ProgressEvery int64 // events between progress callbacks; default 8192

	// StallTimeout aborts a scan that makes no progress for this long. A blocked
	// syscall cannot be cancelled from Go, so without it a single stuck
	// directory open would freeze the UI indefinitely. Zero means the default;
	// negative disables the watchdog.
	StallTimeout time.Duration

	// Workers overrides the number of traversal goroutines. Zero picks a
	// platform default (tuned per OS).
	Workers int
}

type Progress struct {
	Entries int64
	Dirs    int64
	Bytes   int64
	Current string
}

// Sizes summarizes a scan without per-entry detail, so Measure costs memory
// proportional to hard links, not entries.
type Sizes struct {
	Root      string
	Allocated int64
	Apparent  int64
	Entries   int64
	Dirs      int64
	Errors    int64
	Duration  time.Duration

	// Stalled reports that the scan was aborted by the stall watchdog, so the
	// totals are partial and the tree may be missing subtrees.
	Stalled bool
	// ErrorPaths holds the first few paths that could not be read, so the UI can
	// explain a shortfall instead of showing a bare error count.
	ErrorPaths []string
}

type Result struct {
	Sizes
	Tree *tree.Tree
}

const (
	evEntry = iota
	evError
	evDir
)

type event struct {
	kind     int8
	path     string
	own      int64 // allocated bytes for this entry alone
	apparent int64
	mtime    int64
	nlink    uint32
	dev      uint64
	ino      uint64
	hasID    bool
	isDir    bool
	symlink  bool

	// batch is set on evDir: the folded totals for one directory, replacing the
	// per-entry events when the consumer only needs summary numbers.
	batch *dirBatch
}

// linkRec is a hard-link candidate: a file whose blocks are shared with another
// name, so its size is counted only once across the whole scan.
type linkRec struct {
	dev      uint64
	ino      uint64
	nlink    uint32
	alloc    int64
	apparent int64
}

// dirBatch carries the totals for one directory so a measure-only scan can send
// one message per directory instead of one per entry, and never build a path for
// a file. hard-link candidates are kept separate because their size depends on
// dedup, which only the consumer can do.
type dirBatch struct {
	files    int64
	dirs     int64
	alloc    int64
	apparent int64
	links    []linkRec
}

// consumer receives events on a single goroutine, so implementations need no
// synchronization.
type consumer interface {
	handle(event)
	stats() (entries, dirs, bytes int64)
	result() Sizes
	// leaves reports whether the consumer needs one event per entry (a tree
	// builder) rather than folded per-directory batches (a measurer).
	leaves() bool
}

// Measure walks root and returns totals only, without building a tree.
func Measure(ctx context.Context, root string, opts Options) (Sizes, error) {
	return runFast(ctx, root, opts, newMeasurer())
}

// MeasureAll measures several roots, sharing one hard-link set so a file
// hard-linked across roots is counted only once overall.
func MeasureAll(ctx context.Context, roots []string, opts Options) (Sizes, error) {
	m := newMeasurer()
	started := time.Now()
	var last Sizes
	for _, r := range roots {
		s, err := runFast(ctx, r, opts, m)
		if err != nil {
			s.Duration = time.Since(started)
			return s, err
		}
		last = s
	}
	last.Duration = time.Since(started)
	return last, nil
}

// ScanTree walks root and returns a fully built, size-sorted tree.
func ScanTree(ctx context.Context, root string, opts Options) (*Result, error) {
	b := newBuilder(filepath.Clean(root))
	s, err := runFast(ctx, root, opts, b)
	b.finish()
	return &Result{Sizes: s, Tree: b.tr}, err
}

// ErrStalled reports that the stall watchdog aborted a scan. The returned
// Sizes are partial: subtrees may be missing. Callers should surface this so a
// short total is never shown as if it were complete.
var ErrStalled = errors.New("scan stalled: no progress within the timeout")

// walkFunc is the per-entry traversal driver: getattrlistbulk on darwin,
// fastwalk elsewhere (and a stub in tests). It mirrors the fs.WalkDirFunc
// contract.
type walkFunc func(root string, opts Options, fn fs.WalkDirFunc) error

// batchFunc is the folded per-directory driver used for measure-only scans. It
// is nil on platforms that do not implement one.
type batchFunc func(root string, opts Options, send func(event) error) error

// driver pairs the two traversal strategies a platform offers.
type driver struct {
	events walkFunc
	batch  batchFunc
}

// platformDriver returns the driver for the running platform. batch is nil when
// the platform has no folded driver.
func platformDriver() driver {
	d := driver{events: walk}
	if batchSupported() {
		d.batch = walkBatched
	}
	return d
}

// fastwalkWalk drives a walk with fastwalk. It is the non-darwin driver and the
// darwin fallback for symlink-following walks; the bulk driver in walk_darwin.go
// handles the common case.
func fastwalkWalk(root string, opts Options, fn fs.WalkDirFunc) error {
	return fastwalk.Walk(&fastwalk.Config{Follow: opts.FollowSymlinks, NumWorkers: opts.Workers}, root, fn)
}

// runFast drives one walk over the platform driver. cons is invoked only from
// the consumer goroutine, so it needs no locking.
func runFast(ctx context.Context, root string, opts Options, cons consumer) (Sizes, error) {
	return runWalk(ctx, root, opts, cons, platformDriver())
}

// runWalk owns the event pipeline plus a stall watchdog. A blocked syscall
// cannot be cancelled from Go, so without the watchdog a single directory stuck
// in open(2) would freeze the caller indefinitely. On a stall the caller gets
// partial results and ErrStalled; the abandoned walk goroutine (a thread stuck
// in the kernel) is the price of being able to give up on it at all.
func runWalk(ctx context.Context, root string, opts Options, cons consumer, dr driver) (Sizes, error) {
	started := time.Now()
	rootClean := filepath.Clean(root)

	info, err := os.Lstat(rootClean)
	if err != nil {
		return Sizes{Root: rootClean, Duration: time.Since(started)}, err
	}
	rootDev, _, rootHasID := fileID(info)

	// wctx stops the consumer and makes emit give up; as a child of ctx it also
	// propagates a caller cancel.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan event, eventBuffer)

	// lastProgress is bumped by the consumer on every event; the watchdog reads it.
	var lastProgress atomic.Int64
	lastProgress.Store(started.UnixNano())

	var consWG sync.WaitGroup
	consWG.Add(1)
	go func() {
		defer consWG.Done()
		every := opts.ProgressEvery
		if every <= 0 {
			every = 8192
		}
		var n int64
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return
				}
				cons.handle(e)
				lastProgress.Store(time.Now().UnixNano())
				n++
				if opts.Progress != nil && n%every == 0 {
					entries, dirs, bytes := cons.stats()
					opts.Progress(Progress{Entries: entries, Dirs: dirs, Bytes: bytes, Current: e.path})
				}
			case <-wctx.Done():
				return
			}
		}
	}()

	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if wctx.Err() != nil {
			return fs.SkipAll
		}
		if walkErr != nil {
			return emit(wctx, ch, event{kind: evError, path: path})
		}

		isRoot := path == rootClean
		isDir := d.IsDir()

		if !isRoot {
			if skipEntry(opts, d.Name(), path) {
				if isDir {
					return fs.SkipDir
				}
				return nil
			}
		}

		fi, infoErr := d.Info()
		if infoErr != nil {
			return emit(wctx, ch, event{kind: evError, path: path})
		}

		if !isRoot && opts.OneFileSystem && isDir && rootHasID {
			if dev, _, ok := fileID(fi); ok && dev != rootDev {
				return fs.SkipDir
			}
		}

		apparent, allocated, nlink := statSizes(fi)
		dev, ino, hasID := fileID(fi)
		return emit(wctx, ch, event{
			kind:     evEntry,
			path:     path,
			own:      allocated,
			apparent: apparent,
			mtime:    fi.ModTime().Unix(),
			nlink:    nlink,
			dev:      dev,
			ino:      ino,
			hasID:    hasID,
			isDir:    isDir,
			symlink:  fi.Mode()&fs.ModeSymlink != 0,
		})
	}

	stall := opts.StallTimeout
	if stall == 0 {
		stall = defaultStallTimeout
	}
	var stalled atomic.Bool
	if stall > 0 {
		go func() {
			poll := stall / 4
			if poll < 50*time.Millisecond {
				poll = 50 * time.Millisecond
			}
			if poll > stallPollInterval {
				poll = stallPollInterval
			}
			t := time.NewTicker(poll)
			defer t.Stop()
			for {
				select {
				case <-wctx.Done():
					return
				case <-t.C:
					if time.Since(time.Unix(0, lastProgress.Load())) >= stall {
						stalled.Store(true)
						cancel()
						return
					}
				}
			}
		}()
	}

	// A measure-only consumer takes folded per-directory batches, which skip a
	// path and a channel send per file. Symlink-following walks keep the
	// per-entry path (fastwalk), so batch mode is off for those.
	batch := !cons.leaves() && dr.batch != nil && !opts.FollowSymlinks
	walkDone := make(chan error, 1)
	go func() {
		if batch {
			walkDone <- dr.batch(rootClean, opts, func(e event) error { return emit(wctx, ch, e) })
			return
		}
		walkDone <- dr.events(rootClean, opts, walkFn)
	}()

	var walkErr error
	completed := false
	select {
	case walkErr = <-walkDone:
		completed = true
	case <-wctx.Done():
		// Cancelled by the caller or the watchdog. Pick up a walk that finished
		// at the same instant so a completed scan is not reported as aborted.
		select {
		case walkErr = <-walkDone:
			completed = true
		default:
		}
	}

	if completed {
		// fastwalk.Walk returns only after every worker has stopped, so no
		// goroutine can still send and it is safe to close the channel.
		close(ch)
	} else {
		cancel()
	}
	consWG.Wait()

	s := cons.result()
	s.Root = rootClean
	s.Duration = time.Since(started)
	s.Stalled = stalled.Load()

	if cerr := ctx.Err(); cerr != nil {
		return s, cerr
	}
	if !completed {
		return s, ErrStalled
	}
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return s, walkErr
	}
	return s, nil
}

// emit returns fs.SkipAll instead of blocking once the context is done.
func emit(ctx context.Context, ch chan<- event, e event) error {
	select {
	case ch <- e:
		return nil
	case <-ctx.Done():
		return fs.SkipAll
	}
}

// skipEntry reports whether an entry is filtered out before any stat work: a
// hidden name, or an exclude match on its base name or full path. path is only
// consulted when exclude patterns are set.
func skipEntry(opts Options, name, path string) bool {
	if opts.NoHidden && strings.HasPrefix(name, ".") {
		return true
	}
	return excluded(opts.Exclude, name, path)
}

func excluded(patterns []string, name, path string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
		if ok, _ := filepath.Match(p, path); ok {
			return true
		}
	}
	return false
}

// splitParent returns parent and base as substrings of path, so the caller must
// clone base if it needs to outlive the event.
func splitParent(path string) (parent, base string) {
	i := strings.LastIndexByte(path, '/')
	switch {
	case i < 0:
		return "", path
	case i == 0:
		return "/", path[1:]
	default:
		return path[:i], path[i+1:]
	}
}
