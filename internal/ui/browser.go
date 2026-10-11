package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

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
	sizes    scan.Sizes
	progress scan.Progress
	spin     spinner.Model
	loading  bool
	err      error
	width    int
	height   int
	table    table.Model
}

func newBrowser(root string, w, h int, apparent bool) browser {
	if w < 40 {
		w = 80
	}
	b := browser{root: root, apparent: apparent, loading: true, width: w, height: h, sortKey: sortSize, spin: spinner.New()}
	b.table = table.New(
		table.WithColumns(b.columns()),
		table.WithFocused(true),
		table.WithHeight(b.tableHeight()),
	)
	b.table.SetWidth(w)
	return b
}

func (b *browser) apply(msg treeMsg) {
	b.loading = false
	b.err = msg.err
	b.tr = msg.tree
	b.sizes = msg.sizes
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
	b.table.SetColumns(b.columns())
	b.table.SetWidth(w)
	b.table.SetHeight(b.tableHeight())
	b.rebuild()
}

func (b browser) tableHeight() int {
	h := b.height - 8
	if h < 3 {
		h = 3
	}
	return h
}

func (b browser) columns() []table.Column {
	nameWidth := b.width - 4 - 10 - 6 - 11
	if nameWidth < 12 {
		nameWidth = 12
	}
	return []table.Column{
		{Title: "Name", Width: nameWidth},
		{Title: "Size", Width: 10},
		{Title: "Items", Width: 6},
		{Title: "Modified", Width: 11},
	}
}

func (b browser) sizeOf(n *tree.Node) string {
	if b.apparent {
		return format.Bytes(n.Apparent)
	}
	return format.Bytes(n.Size)
}

func (b browser) currentDir() (int32, bool) {
	if b.tr == nil || len(b.stack) == 0 {
		return 0, false
	}
	return b.stack[len(b.stack)-1], true
}

// children lists the current directory's children, filtered then sorted.
func (b browser) children() []int32 {
	cur, ok := b.currentDir()
	if !ok {
		return nil
	}
	kids := b.tr.Children(cur)
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

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (b *browser) rebuild() {
	if _, ok := b.currentDir(); !ok {
		b.table.SetRows(nil)
		return
	}
	kids := b.children()
	rows := make([]table.Row, 0, len(kids))
	for _, k := range kids {
		n := b.tr.Node(k)
		name := n.Name
		if n.IsDir() {
			name += "/"
		}
		if _, marked := b.marks[joinPath(b.breadcrumbOf(b.stack[len(b.stack)-1]), n.Name)]; marked {
			name = "● " + name
		}
		rows = append(rows, table.Row{name, b.sizeOf(n), format.Thousands(int64(n.Items)), formatDate(n.Mtime)})
	}
	b.table.SetRows(rows)
	b.table.SetCursor(0)
}

func formatDate(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

func (b browser) currentChild() (int32, string, bool) {
	cur, ok := b.currentDir()
	if !ok {
		return 0, "", false
	}
	kids := b.children()
	pos := b.table.Cursor()
	if pos < 0 || pos >= len(kids) {
		return 0, "", false
	}
	idx := kids[pos]
	return idx, joinPath(b.breadcrumbOf(cur), b.tr.Node(idx).Name), true
}

// rowAt maps a screen y to an index into children() (the table starts below the
// header, divider, breadcrumb and stats lines, and its header row).
func (b browser) rowAt(y int) (int, bool) {
	const top = 5
	start := b.table.Cursor() - b.tableHeight()
	if start < 0 {
		start = 0
	}
	idx := start + (y - top)
	if y < top || idx < 0 || idx >= len(b.children()) {
		return 0, false
	}
	return idx, true
}

func (b *browser) down() {
	idx, _, ok := b.currentChild()
	if !ok || !b.tr.Node(idx).IsDir() {
		return
	}
	b.stack = append(b.stack, idx)
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

func (b *browser) tableUpdate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	b.table, cmd = b.table.Update(msg)
	return cmd
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

func (b browser) render() string {
	if b.width < 30 || b.height < 6 {
		return tooSmall
	}

	var sb strings.Builder
	sb.WriteString(padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("  browse  "), b.width))
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

	sb.WriteString(styleDim.Render("  " + clip(b.breadcrumb(), b.width-4)))
	sb.WriteByte('\n')
	stats := fmt.Sprintf("%s · %s items · %s dirs · %s errors",
		format.Bytes(b.rootSize()), format.Thousands(b.sizes.Entries), format.Thousands(b.sizes.Dirs), format.Thousands(b.sizes.Errors))
	sb.WriteString(padBetween(styleDim.Render("  "+stats), styleDim.Render("sort: "+b.sortLabel()+" "), b.width))
	sb.WriteByte('\n')
	sb.WriteString(b.table.View())
	sb.WriteByte('\n')
	sb.WriteString(divider(b.width))
	sb.WriteByte('\n')
	sb.WriteString(styleDim.Render("  ↑↓ Move · S/N/I/M Sort · Space Mark · / Filter · Enter Open · D Delete · Esc Back · Q Quit"))
	return sb.String()
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
