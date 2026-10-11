package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/tree"
)

type treeMsg struct {
	path string
	tree *tree.Tree
	err  error
}

type browser struct {
	root     string
	apparent bool
	tr       *tree.Tree
	stack    []int32
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
	b := browser{root: root, apparent: apparent, loading: true, width: w, height: h}
	b.table = table.New(
		table.WithColumns(b.columns()),
		table.WithFocused(true),
		table.WithHeight(b.tableHeight()),
	)
	b.table.SetWidth(w)
	return b
}

func (b browser) scan() tea.Cmd {
	root := b.root
	return func() tea.Msg {
		res, err := scan.ScanTree(context.Background(), root, scan.Options{})
		if err != nil {
			return treeMsg{path: root, err: err}
		}
		return treeMsg{path: root, tree: res.Tree}
	}
}

func (b *browser) apply(msg treeMsg) {
	b.loading = false
	b.err = msg.err
	b.tr = msg.tree
	if b.tr != nil && b.tr.Root >= 0 {
		b.stack = []int32{b.tr.Root}
	}
	b.rebuild()
}

func (b *browser) resize(w, h int) {
	b.width, b.height = w, h
	b.table.SetColumns(b.columns())
	b.table.SetWidth(w)
	b.table.SetHeight(b.tableHeight())
}

func (b browser) tableHeight() int {
	h := b.height - 7
	if h < 3 {
		h = 3
	}
	return h
}

func (b browser) columns() []table.Column {
	nameWidth := b.width - 4 - 12 - 8
	if nameWidth < 12 {
		nameWidth = 12
	}
	return []table.Column{
		{Title: "Name", Width: nameWidth},
		{Title: "Size", Width: 12},
		{Title: "Items", Width: 8},
	}
}

func (b browser) sizeOf(n *tree.Node) string {
	if b.apparent {
		return format.Bytes(n.Apparent)
	}
	return format.Bytes(n.Size)
}

func (b *browser) rebuild() {
	if b.tr == nil || len(b.stack) == 0 {
		b.table.SetRows(nil)
		return
	}
	kids := b.tr.Children(b.stack[len(b.stack)-1])
	rows := make([]table.Row, 0, len(kids))
	for _, k := range kids {
		n := b.tr.Node(k)
		name := n.Name
		if n.IsDir() {
			name += "/"
		}
		rows = append(rows, table.Row{name, b.sizeOf(n), format.Thousands(int64(n.Items))})
	}
	b.table.SetRows(rows)
	b.table.SetCursor(0)
}

func (b *browser) down() {
	if b.tr == nil || len(b.stack) == 0 {
		return
	}
	kids := b.tr.Children(b.stack[len(b.stack)-1])
	cur := b.table.Cursor()
	if cur < 0 || cur >= len(kids) {
		return
	}
	if !b.tr.Node(kids[cur]).IsDir() {
		return
	}
	b.stack = append(b.stack, kids[cur])
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

func (b *browser) tableUpdate(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	b.table, cmd = b.table.Update(msg)
	return cmd
}

func (b browser) breadcrumb() string {
	if b.tr == nil || len(b.stack) == 0 {
		return b.root
	}
	i := b.stack[len(b.stack)-1]
	var parts []string
	for depth := 0; i >= 0 && depth <= b.tr.Len(); depth++ {
		parts = append(parts, b.tr.Node(i).Name)
		if i == b.tr.Root {
			break
		}
		i = b.tr.Node(i).Parent
	}
	for l, r := 0, len(parts)-1; l < r; l, r = l+1, r-1 {
		parts[l], parts[r] = parts[r], parts[l]
	}
	return strings.Join(parts, "/")
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
		sb.WriteString(styleDim.Render("  scanning " + clip(b.root, b.width-12) + " …"))
		sb.WriteByte('\n')
		return sb.String()
	}
	if b.err != nil {
		sb.WriteString(styleDim.Render("  error: " + b.err.Error()))
		sb.WriteByte('\n')
		return sb.String()
	}

	sb.WriteString(styleDim.Render("  " + clip(b.breadcrumb(), b.width-4)))
	sb.WriteString("\n\n")
	sb.WriteString(b.table.View())
	sb.WriteByte('\n')
	sb.WriteString(divider(b.width))
	sb.WriteByte('\n')
	sb.WriteString(styleDim.Render("  ↑↓ Move · Enter Open · Esc Back · Q Quit"))
	return sb.String()
}
