// Package tree stores a filesystem hierarchy in a flat arena: nodes reference
// each other by int32 index in a first-child / next-sibling structure, so the
// tree is one contiguous slice with no per-directory child slices.
//
// Invariant: a node's index always exceeds its parent's, because the scanner
// appends a directory before its entries. Rollup relies on this to be a single
// backwards pass instead of recursion.
package tree

import "sort"

const NoNode int32 = -1

type Flags uint8

const (
	FlagDir Flags = 1 << iota
	FlagSymlink
	// FlagHardlinkDup marks an entry whose blocks were already counted under another name.
	FlagHardlinkDup
	FlagError
)

type Node struct {
	Name        string
	Size        int64 // subtree allocated size, after Rollup
	Apparent    int64 // subtree logical size, after Rollup
	Parent      int32
	FirstChild  int32
	NextSibling int32
	LastChild   int32 // kept so Add can append in O(1)
	Items       int32 // descendant count, after Rollup
	Flags       Flags
}

func (n *Node) IsDir() bool { return n.Flags&FlagDir != 0 }

type Tree struct {
	Nodes []Node
	Root  int32
}

func New() *Tree { return &Tree{Root: NoNode} }

func (t *Tree) Len() int { return len(t.Nodes) }

// Node returns a pointer to node i; it is invalidated by any later Add.
func (t *Tree) Node(i int32) *Node { return &t.Nodes[i] }

// Add appends a node under parent and returns its index; NoNode detaches it.
func (t *Tree) Add(parent int32, name string, size, apparent int64, flags Flags) int32 {
	idx := int32(len(t.Nodes))
	t.Nodes = append(t.Nodes, Node{
		Name:        name,
		Size:        size,
		Apparent:    apparent,
		Parent:      parent,
		FirstChild:  NoNode,
		NextSibling: NoNode,
		LastChild:   NoNode,
		Flags:       flags,
	})
	if parent >= 0 && parent < idx {
		p := &t.Nodes[parent]
		if p.FirstChild == NoNode {
			p.FirstChild = idx
		} else {
			t.Nodes[p.LastChild].NextSibling = idx
		}
		p.LastChild = idx
	}
	return idx
}

func (t *Tree) Children(parent int32) []int32 {
	var out []int32
	for c := t.Nodes[parent].FirstChild; c != NoNode; c = t.Nodes[c].NextSibling {
		out = append(out, c)
	}
	return out
}

// Rollup folds descendant sizes and item counts into each parent, relying on
// the parent-index < child-index invariant.
func (t *Tree) Rollup() {
	for i := len(t.Nodes) - 1; i >= 1; i-- {
		p := t.Nodes[i].Parent
		if p < 0 || int(p) >= i {
			continue // root, detached, or (defensively) out of order
		}
		t.Nodes[p].Size += t.Nodes[i].Size
		t.Nodes[p].Apparent += t.Nodes[i].Apparent
		t.Nodes[p].Items += t.Nodes[i].Items + 1
	}
}

// SortChildren orders each directory's children by descending size, ties by name.
func (t *Tree) SortChildren() {
	var scratch []int32
	for i := range t.Nodes {
		if t.Nodes[i].FirstChild == NoNode {
			continue
		}
		scratch = scratch[:0]
		for c := t.Nodes[i].FirstChild; c != NoNode; c = t.Nodes[c].NextSibling {
			scratch = append(scratch, c)
		}
		kids := scratch
		sort.Slice(kids, func(a, b int) bool {
			na, nb := &t.Nodes[kids[a]], &t.Nodes[kids[b]]
			if na.Size != nb.Size {
				return na.Size > nb.Size
			}
			return na.Name < nb.Name
		})
		for j := range kids {
			if j+1 < len(kids) {
				t.Nodes[kids[j]].NextSibling = kids[j+1]
			} else {
				t.Nodes[kids[j]].NextSibling = NoNode
			}
		}
		t.Nodes[i].FirstChild = kids[0]
		t.Nodes[i].LastChild = kids[len(kids)-1]
	}
}

// Remove unlinks node i, keeping LastChild consistent so later Add calls still
// work. Ancestor totals are not adjusted — call Rollup again.
func (t *Tree) Remove(i int32) {
	n := &t.Nodes[i]
	if n.Parent < 0 {
		return
	}
	p := &t.Nodes[n.Parent]
	var prev int32 = NoNode
	for c := p.FirstChild; c != NoNode; c = t.Nodes[c].NextSibling {
		if c == i {
			break
		}
		prev = c
	}
	if prev == NoNode {
		p.FirstChild = n.NextSibling
	} else {
		t.Nodes[prev].NextSibling = n.NextSibling
	}
	if p.LastChild == i {
		p.LastChild = prev
	}
	n.Parent = NoNode
	n.NextSibling = NoNode
}

// CheckInvariant returns the first node whose parent index is not smaller than
// its own, or -1.
func (t *Tree) CheckInvariant() int32 {
	for i := 1; i < len(t.Nodes); i++ {
		p := t.Nodes[i].Parent
		if p >= 0 && int(p) >= i {
			return int32(i)
		}
	}
	return -1
}
