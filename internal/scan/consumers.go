package scan

import (
	"strings"

	"github.com/rahul-17280/sweep/internal/tree"
)

// measurer accumulates totals without per-entry structures; its only heap use
// is the hard-link set, so measuring any tree costs near-constant memory.
type measurer struct {
	sz    Sizes
	links hardlinkSet
}

func newMeasurer() *measurer { return &measurer{links: newHardlinkSet()} }

func (m *measurer) handle(e event) {
	if e.kind == evError {
		m.sz.Errors++
		return
	}
	m.sz.Entries++
	if e.isDir {
		m.sz.Dirs++
	}

	own, app := e.own, e.apparent
	if e.hasID {
		if count, _ := m.links.dedupe(e.dev, e.ino, e.nlink); !count {
			own, app = 0, 0
		}
	}
	m.sz.Allocated += own
	m.sz.Apparent += app
}

func (m *measurer) stats() (int64, int64, int64) {
	return m.sz.Entries, m.sz.Dirs, m.sz.Allocated
}

func (m *measurer) result() Sizes { return m.sz }

// builder owns the arena tree; it is called only from the single consumer
// goroutine, so its maps and tree need no locking.
type builder struct {
	tr        *tree.Tree
	rootClean string
	// dirs indexes only directories (far fewer than entries); it is dropped at
	// finish() so those path strings can be collected.
	dirs  map[string]int32
	links hardlinkSet
	sz    Sizes
}

func newBuilder(rootClean string) *builder {
	return &builder{
		tr:        tree.New(),
		rootClean: rootClean,
		dirs:      make(map[string]int32, 1024),
		links:     newHardlinkSet(),
	}
}

func (b *builder) handle(e event) {
	if e.kind == evError {
		b.sz.Errors++
		return
	}
	b.sz.Entries++
	if e.isDir {
		b.sz.Dirs++
	}

	own, app := e.own, e.apparent
	var flags tree.Flags
	if e.isDir {
		flags |= tree.FlagDir
	}
	if e.symlink {
		flags |= tree.FlagSymlink
	}
	if e.hasID {
		if count, dup := b.links.dedupe(e.dev, e.ino, e.nlink); !count {
			own, app = 0, 0
			if dup {
				flags |= tree.FlagHardlinkDup
			}
		}
	}
	b.sz.Allocated += own
	b.sz.Apparent += app

	if e.path == b.rootClean {
		idx := b.tr.Add(tree.NoNode, e.path, own, app, flags)
		b.tr.Root = idx
		b.dirs[e.path] = idx
		return
	}

	parent, base := splitParent(e.path)
	pi, ok := b.dirs[parent]
	if !ok {
		// Defensive: fastwalk guarantees a directory's callback precedes its
		// entries, so this is unreachable. Attach to the root and flag it
		// rather than dropping the entry or panicking.
		pi = b.tr.Root
		flags |= tree.FlagError
	}
	// base is a substring of e.path; clone it so the node does not pin the path.
	idx := b.tr.Add(pi, strings.Clone(base), own, app, flags)
	if e.isDir {
		b.dirs[e.path] = idx
	}
}

func (b *builder) finish() {
	b.tr.Rollup()
	b.tr.SortChildren()
	b.dirs = nil
	b.links = nil
}

func (b *builder) stats() (int64, int64, int64) {
	return b.sz.Entries, b.sz.Dirs, b.sz.Allocated
}

func (b *builder) result() Sizes { return b.sz }
