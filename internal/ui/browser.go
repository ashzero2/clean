package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/tree"
)

type sortKey uint8

const (
	sortSize sortKey = iota
	sortName
	sortItems
	sortMtime
)

const (
	maxColumns  = 3
	panelWidth  = 30
	topFileList = 12
)

type treeMsg struct {
	path  string
	tree  *tree.Tree
	sizes scan.Sizes
	err   error
}

type browser struct {
	root     string
	apparent bool
	exclude  []string
	tr       *tree.Tree
	stack    []int32
	marks    map[string]mark
	filter   string
	sortKey  sortKey
	sortAsc  bool
	cursor   int
	top      []int32
	sizes    scan.Sizes
	progress scan.Progress
	spin     spinner.Model
	loading  bool
	err      error
	width    int
	height   int
}

// millerCol is one column: a directory, its visible children, and the selected row.
type millerCol struct {
	dir      int32
	rows     []int32
	selected int
}

func newBrowser(root string, w, h int, apparent bool) browser {
	if w < 40 {
		w = 80
	}
	return browser{root: root, apparent: apparent, loading: true, width: w, height: h, sortKey: sortSize, spin: spinner.New()}
}

func (b *browser) apply(msg treeMsg) {
	b.loading = false
	b.err = msg.err
	b.tr = msg.tree
	b.sizes = msg.sizes
	b.stack = nil
	if b.tr != nil && b.tr.Root >= 0 {
		b.stack = []int32{b.tr.Root}
	}
	b.rebuild()
}

func (b *browser) setSort(key sortKey) {
	if b.sortKey == key {
		b.sortAsc = !b.sortAsc
	} else {
		b.sortKey = key
		b.sortAsc = key == sortName
	}
	b.rebuild()
}

func (b browser) sortLabel() string {
	name := map[sortKey]string{sortSize: "size", sortName: "name", sortItems: "items", sortMtime: "mtime"}[b.sortKey]
	dir := "↓"
	if b.sortAsc {
		dir = "↑"
	}
	return name + dir
}

func (b *browser) resize(w, h int) {
	b.width, b.height = w, h
}

func (b browser) currentDir() (int32, bool) {
	if b.tr == nil || len(b.stack) == 0 {
		return 0, false
	}
	return b.stack[len(b.stack)-1], true
}

func (b browser) childrenOf(dir int32) []int32 {
	kids := b.tr.Children(dir)
	if b.filter != "" {
		kept := kids[:0]
		for _, k := range kids {
			if strings.Contains(b.tr.Node(k).Name, b.filter) {
				kept = append(kept, k)
			}
		}
		kids = kept
	}
	sort.SliceStable(kids, func(i, j int) bool {
		ni, nj := b.tr.Node(kids[i]), b.tr.Node(kids[j])
		var c int
		switch b.sortKey {
		case sortName:
			c = strings.Compare(ni.Name, nj.Name)
		case sortItems:
			c = compareInt(int64(ni.Items), int64(nj.Items))
		case sortMtime:
			c = compareInt(ni.Mtime, nj.Mtime)
		default:
			c = compareInt(ni.Size, nj.Size)
		}
		if b.sortAsc {
			return c < 0
		}
		return c > 0
	})
	return kids
}

func (b browser) children() []int32 {
	cur, ok := b.currentDir()
	if !ok {
		return nil
	}
	return b.childrenOf(cur)
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// columns returns the visible Miller columns, ending at the current directory.
func (b browser) columns() []millerCol {
	if b.tr == nil || len(b.stack) == 0 {
		return nil
	}
	dirs := b.stack
	if len(dirs) > maxColumns {
		dirs = dirs[len(dirs)-maxColumns:]
	}
	cols := make([]millerCol, 0, len(dirs))
	for i, dir := range dirs {
		rows := b.childrenOf(dir)
		sel := -1
		if i < len(dirs)-1 {
			next := dirs[i+1]
			for j, c := range rows {
				if c == next {
					sel = j
					break
				}
			}
		} else {
			sel = b.cursor
			if sel >= len(rows) {
				sel = len(rows) - 1
			}
		}
		cols = append(cols, millerCol{dir: dir, rows: rows, selected: sel})
	}
	return cols
}

func (b *browser) move(delta int) {
	n := len(b.children())
	if n == 0 {
		return
	}
	b.cursor += delta
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= n {
		b.cursor = n - 1
	}
}

func (b *browser) down() {
	kids := b.children()
	if b.cursor < 0 || b.cursor >= len(kids) || !b.tr.Node(kids[b.cursor]).IsDir() {
		return
	}
	b.stack = append(b.stack, kids[b.cursor])
	b.rebuild()
}

func (b *browser) up() bool {
	if len(b.stack) <= 1 {
		return false
	}
	b.stack = b.stack[:len(b.stack)-1]
	b.rebuild()
	return true
}

func (b *browser) rebuild() {
	b.cursor = 0
	b.recomputeTop()
}

// recomputeTop finds the largest files in the current subtree, cached until the
// directory changes.
func (b *browser) recomputeTop() {
	b.top = b.top[:0]
	cur, ok := b.currentDir()
	if !ok {
		return
	}
	stack := []int32{cur}
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for c := b.tr.Node(dir).FirstChild; c != tree.NoNode; c = b.tr.Node(c).NextSibling {
			n := b.tr.Node(c)
			if n.IsDir() {
				stack = append(stack, c)
				continue
			}
			b.consider(c)
		}
	}
}

func (b *browser) consider(idx int32) {
	sz := b.tr.Node(idx).Size
	pos := len(b.top)
	for i := range b.top {
		if b.tr.Node(b.top[i]).Size < sz {
			pos = i
			break
		}
	}
	if pos >= topFileList {
		return
	}
	b.top = append(b.top, 0)
	copy(b.top[pos+1:], b.top[pos:])
	b.top[pos] = idx
	if len(b.top) > topFileList {
		b.top = b.top[:topFileList]
	}
}

// removeNodes unlinks each node, subtracting its subtree from every ancestor
// first so the rolled-up totals stay correct without a full recompute.
func (b *browser) removeNodes(nodes []int32) {
	for _, idx := range nodes {
		n := b.tr.Node(idx)
		parent := n.Parent
		if parent < 0 {
			continue
		}
		size, apparent, items := n.Size, n.Apparent, n.Items+1
		for p := parent; p >= 0; p = b.tr.Node(p).Parent {
			b.tr.Node(p).Size -= size
			b.tr.Node(p).Apparent -= apparent
			b.tr.Node(p).Items -= items
		}
		b.tr.Remove(idx)
	}
	b.rebuild()
}

func (b browser) currentChild() (int32, string, bool) {
	cur, ok := b.currentDir()
	if !ok {
		return 0, "", false
	}
	kids := b.children()
	if b.cursor < 0 || b.cursor >= len(kids) {
		return 0, "", false
	}
	idx := kids[b.cursor]
	return idx, joinPath(b.breadcrumbOf(cur), b.tr.Node(idx).Name), true
}

func (b browser) rowAt(x, y int) (int, int, bool) {
	const top = 1
	cols := b.columns()
	if len(cols) == 0 || y < top {
		return 0, 0, false
	}
	panel := 0
	if b.width >= 76 {
		panel = panelWidth + 2
	}
	avail := b.width - panel
	colW := avail / len(cols)
	if colW < 12 {
		colW = 12
	}
	ci := x / colW
	if ci >= len(cols) {
		ci = len(cols) - 1
	}
	if ci >= len(cols) {
		return 0, 0, false
	}
	col := cols[ci]
	availRows := b.height - 8
	if availRows < 1 {
		availRows = 1
	}
	start := 0
	if col.selected >= availRows {
		start = col.selected - availRows + 1
	}
	ri := start + (y - top)
	if ri < 0 || ri >= len(col.rows) {
		return 0, 0, false
	}
	return ci, ri, true
}

func (b browser) breadcrumbOf(idx int32) string {
	if b.tr == nil {
		return b.root
	}
	var parts []string
	for depth := 0; idx >= 0 && depth <= b.tr.Len(); depth++ {
		parts = append(parts, b.tr.Node(idx).Name)
		if idx == b.tr.Root {
			break
		}
		idx = b.tr.Node(idx).Parent
	}
	for l, r := 0, len(parts)-1; l < r; l, r = l+1, r-1 {
		parts[l], parts[r] = parts[r], parts[l]
	}
	return strings.Join(parts, "/")
}

func (b browser) breadcrumb() string {
	cur, ok := b.currentDir()
	if !ok {
		return b.root
	}
	return b.breadcrumbOf(cur)
}

func joinPath(base, name string) string {
	if base == "/" {
		return "/" + name
	}
	return base + "/" + name
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

func (b browser) sizeOf(n *tree.Node) string {
	if b.apparent {
		return format.Bytes(n.Apparent)
	}
	return format.Bytes(n.Size)
}

func formatDate(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

func (b browser) rootSize() int64 {
	if b.tr == nil || b.tr.Root < 0 {
		return 0
	}
	n := b.tr.Node(b.tr.Root)
	if b.apparent {
		return n.Apparent
	}
	return n.Size
}

func (b browser) render() string {
	if b.width < 40 || b.height < 8 {
		return tooSmall
	}

	var sb strings.Builder
	sb.WriteString(padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("  "+clip(b.root, b.width-20)+"  "), b.width))
	sb.WriteByte('\n')
	sb.WriteString(divider(b.width))
	sb.WriteByte('\n')

	if b.loading {
		sb.WriteString(fmt.Sprintf("  %s Scanning %s", b.spin.View(), clip(b.root, b.width-24)))
		sb.WriteString("\n\n")
		line := "  counting…"
		if b.progress.Entries > 0 {
			line = fmt.Sprintf("  %s items · %s", format.Thousands(b.progress.Entries), format.Bytes(b.progress.Bytes))
		}
		sb.WriteString(styleDim.Render(line))
		sb.WriteByte('\n')
		if b.progress.Current != "" {
			sb.WriteString(styleDim.Render("  " + clip(b.progress.Current, b.width-4)))
			sb.WriteByte('\n')
		}
		return sb.String()
	}
	if b.err != nil {
		sb.WriteString(styleDim.Render("  error: " + b.err.Error()))
		sb.WriteByte('\n')
		return sb.String()
	}

	sb.WriteString(b.body(b.height - 6))
	sb.WriteByte('\n')
	sb.WriteString(divider(b.width))
	sb.WriteByte('\n')
	stats := fmt.Sprintf("%s · %s items · %s dirs · %s errors",
		format.Bytes(b.rootSize()), format.Thousands(b.sizes.Entries), format.Thousands(b.sizes.Dirs), format.Thousands(b.sizes.Errors))
	if b.sizes.Stalled {
		stats += " · STALLED (partial)"
	}
	sb.WriteString(padBetween(styleDim.Render("  "+stats), styleDim.Render("sort: "+b.sortLabel()+" "), b.width))
	sb.WriteByte('\n')
	sb.WriteString(styleDim.Render("  " + clip(b.breadcrumb(), b.width-4)))
	sb.WriteByte('\n')
	sb.WriteString(styleDim.Render("  ↑↓ Move · Enter Open · ← Up · S/N/I/M Sort · Space Mark · / Filter · D Delete · Q Quit"))
	return sb.String()
}

func (b browser) body(height int) string {
	cols := b.columns()
	if len(cols) == 0 {
		return ""
	}
	showPanel := b.width >= 76
	panel := 0
	if showPanel {
		panel = panelWidth + 2
	}
	avail := b.width - panel
	colW := avail / len(cols)
	if colW < 12 {
		colW = 12
	}

	rendered := make([]string, 0, len(cols)+2)
	for i, c := range cols {
		if i > 0 {
			rendered = append(rendered, styleDivider.Render("│"))
		}
		rendered = append(rendered, b.renderColumn(c, colW, height))
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if !showPanel {
		return board
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, board, "  ", b.renderBiggest(panelWidth, height))
}

func (b browser) renderColumn(c millerCol, w, h int) string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(clip(" "+baseName(b.tr.Node(c.dir).Name)+"/", w)))
	sb.WriteByte('\n')

	avail := h - 1
	if avail < 1 {
		avail = 1
	}
	start := 0
	if c.selected >= avail {
		start = c.selected - avail + 1
	}
	end := start + avail
	if end > len(c.rows) {
		end = len(c.rows)
	}
	for i := start; i < end; i++ {
		n := b.tr.Node(c.rows[i])
		name := n.Name
		if n.IsDir() {
			name += "/"
		}
		if _, marked := b.marks[joinPath(b.breadcrumbOf(c.dir), n.Name)]; marked {
			name = "● " + name
		}
		line := padBetween(" "+clip(name, w-9), b.sizeOf(n)+" ", w)
		if i == c.selected {
			line = styleSelected.Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (b browser) renderBiggest(w, h int) string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(" Biggest files"))
	sb.WriteByte('\n')
	for i, idx := range b.top {
		if i >= h-1 {
			break
		}
		n := b.tr.Node(idx)
		sb.WriteString(padBetween(" "+clip(n.Name, w-10), b.sizeOf(n)+" ", w))
		sb.WriteByte('\n')
	}
	return sb.String()
}
