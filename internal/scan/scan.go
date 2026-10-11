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
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/rahul-17280/sweep/internal/tree"
)

const eventBuffer = 8192

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
}

type Result struct {
	Sizes
	Tree *tree.Tree
}

const (
	evEntry = iota
	evError
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
}

// consumer receives events on a single goroutine, so implementations need no
// synchronization.
type consumer interface {
	handle(event)
	stats() (entries, dirs, bytes int64)
	result() Sizes
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

// runFast drives one fastwalk pass. cons is invoked only from the consumer
// goroutine, so it needs no locking.
func runFast(ctx context.Context, root string, opts Options, cons consumer) (Sizes, error) {
	started := time.Now()
	rootClean := filepath.Clean(root)

	info, err := os.Lstat(rootClean)
	if err != nil {
		return Sizes{Root: rootClean, Duration: time.Since(started)}, err
	}
	rootDev, _, rootHasID := fileID(info)

	ch := make(chan event, eventBuffer)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		every := opts.ProgressEvery
		if every <= 0 {
			every = 8192
		}
		var n int64
		for e := range ch {
			cons.handle(e)
			n++
			if opts.Progress != nil && n%every == 0 {
				entries, dirs, bytes := cons.stats()
				opts.Progress(Progress{Entries: entries, Dirs: dirs, Bytes: bytes, Current: e.path})
			}
		}
	}()

	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if walkErr != nil {
			return emit(ctx, ch, event{kind: evError, path: path})
		}

		isRoot := path == rootClean
		isDir := d.IsDir()

		if !isRoot {
			name := d.Name()
			if opts.NoHidden && strings.HasPrefix(name, ".") {
				if isDir {
					return fs.SkipDir
				}
				return nil
			}
			if excluded(opts.Exclude, name, path) {
				if isDir {
					return fs.SkipDir
				}
				return nil
			}
		}

		fi, infoErr := d.Info()
		if infoErr != nil {
			return emit(ctx, ch, event{kind: evError, path: path})
		}

		if !isRoot && opts.OneFileSystem && isDir && rootHasID {
			if dev, _, ok := fileID(fi); ok && dev != rootDev {
				return fs.SkipDir
			}
		}

		apparent, allocated, nlink := statSizes(fi)
		dev, ino, hasID := fileID(fi)
		return emit(ctx, ch, event{
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

	cfg := fastwalk.Config{Follow: opts.FollowSymlinks}
	walkErr := fastwalk.Walk(&cfg, rootClean, walkFn)

	close(ch)
	wg.Wait()

	s := cons.result()
	s.Root = rootClean
	s.Duration = time.Since(started)

	if cerr := ctx.Err(); cerr != nil {
		return s, cerr
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
