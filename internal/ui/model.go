// Package ui renders the interactive dashboard. It is the only package that
// depends on Bubble Tea / Lip Gloss; everything it displays comes from catalog,
// scope, tree, trash and volume.
package ui

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
	"github.com/rahul-17280/sweep/internal/trash"
)

type viewKind uint8

const (
	viewDashboard viewKind = iota
	viewCategory
	viewBrowser
)

// mark records a path chosen for deletion: its size and, in the browser view,
// the tree node index to unlink.
type mark struct {
	size int64
	node int32
}

type Options struct {
	Roots       []string
	Workers     int
	Root        string
	Apparent    bool
	Exclude     []string
	Trasher     trash.Trasher
	AllowDelete bool
	Permanent   bool
}

type Model struct {
	width  int
	height int
	view   viewKind

	dash dashboard
	cat  category
	tree browser

	opts      Options
	marks     map[string]mark
	pending   []string
	confirm   bool
	status    string
	filter    string
	filtering bool
	prompt    string
	prompting bool
}

func New(cats []catalog.Category, opts Options) Model {
	if opts.Trasher == nil {
		opts.Trasher = trash.New()
	}
	m := Model{opts: opts, marks: map[string]mark{}}
	m.dash = newDashboard(cats, opts.Roots, opts.Workers, opts.Root, opts.Apparent)
	m.dash.exclude = opts.Exclude
	return m
}

// NewInBrowser starts directly in the tree browser at path.
func NewInBrowser(cats []catalog.Category, path string, opts Options) Model {
	m := New(cats, opts)
	m.view = viewBrowser
	m.tree = newBrowser(scope.Expand(path), 100, 30, opts.Apparent)
	m.tree.exclude = opts.Exclude
	return m
}

func (m Model) Init() tea.Cmd {
	if m.view == viewBrowser && m.tree.root != "" {
		return tea.Batch(m.dash.Init(), m.tree.scan())
	}
	return m.dash.Init()
}

func (m *Model) openBrowser(path string) tea.Cmd {
	m.tree = newBrowser(scope.Expand(path), m.width, m.height, m.dash.apparent)
	m.tree.exclude = m.opts.Exclude
	m.filter = ""
	m.view = viewBrowser
	return m.tree.scan()
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/"
}

// sync copies the shared per-view inputs (marks, filter) onto the sub-views.
func (m *Model) sync() {
	m.cat.marks = m.marks
	m.tree.marks = m.marks
	m.cat.filter = m.filter
	m.tree.filter = m.filter
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.sync()

	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.dash.resize(ws.Width, ws.Height)
		m.cat.resize(ws.Width, ws.Height)
		m.tree.resize(ws.Width, ws.Height)
		return m, nil
	}

	switch msg := msg.(type) {
	case categoryMsg, scanDoneMsg:
		return m, m.dash.update(msg)
	case treeMsg:
		m.tree.apply(msg)
		return m, nil
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	}

	key, isKey := msg.(tea.KeyPressMsg)
	if isKey && key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.filtering {
		if isKey {
			m.applyFilterKey(key)
			m.sync()
		}
		return m, nil
	}
	if m.prompting {
		if isKey {
			return m, m.applyPromptKey(key)
		}
		return m, nil
	}
	if m.confirm {
		return m.updateConfirm(msg)
	}

	switch m.view {
	case viewCategory:
		return m.updateCategory(msg)
	case viewBrowser:
		return m.updateBrowser(msg)
	default:
		return m.updateDashboard(msg)
	}
}

func (m *Model) applyFilterKey(key tea.KeyPressMsg) {
	switch key.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
	case "enter":
		m.filtering = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	default:
		if key.Text != "" {
			m.filter += key.Text
		}
	}
}

func (m *Model) applyPromptKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.prompting = false
		m.prompt = ""
	case "enter":
		m.prompting = false
		path := m.prompt
		m.prompt = ""
		if path == "" {
			path = homeDir()
		}
		return m.openBrowser(path)
	case "backspace":
		if r := []rune(m.prompt); len(r) > 0 {
			m.prompt = string(r[:len(r)-1])
		}
	default:
		if key.Text != "" {
			m.prompt += key.Text
		}
	}
	return nil
}

func (m Model) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.dash.move(-1)
	case "down", "j":
		m.dash.move(1)
	case "r":
		return m, m.dash.rescan()
	case "b":
		return m, m.openBrowser(homeDir())
	case "o":
		m.prompting = true
		m.prompt = ""
	case "enter":
		r, ok := m.dash.currentRow()
		if !ok {
			break
		}
		if r.kind == rowGroup {
			m.dash.toggle(r.tier)
			break
		}
		return m.openCategory(r.cat)
	}
	return m, nil
}

func (m Model) updateCategory(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		m.view = viewDashboard
	case "up", "k":
		m.cat.move(-1)
	case "down", "j":
		m.cat.move(1)
	case "space":
		if match, ok := m.cat.current(); ok {
			m.toggleMark(match.Path, match.Allocated, -1)
		}
	case "d":
		m.initiateDelete()
	case "/":
		m.filtering = true
		m.filter = ""
	case "enter", "l", "right":
		if path, ok := m.cat.selectedPath(); ok {
			m.tree = newBrowser(path, m.width, m.height, m.dash.apparent)
			m.tree.exclude = m.opts.Exclude
			m.filter = ""
			m.view = viewBrowser
			return m, m.tree.scan()
		}
	}
	return m, nil
}

func (m Model) updateBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		if !m.tree.up() {
			m.filter = ""
			if m.cat.cat.ID != "" {
				m.view = viewCategory
			} else {
				m.view = viewDashboard
			}
		}
		return m, nil
	case "space":
		if idx, path, ok := m.tree.currentChild(); ok {
			m.toggleMark(path, m.tree.tr.Node(idx).Size, idx)
		}
	case "d":
		m.initiateDelete()
	case "s":
		m.tree.setSort(sortSize)
	case "n":
		m.tree.setSort(sortName)
	case "i":
		m.tree.setSort(sortItems)
	case "m":
		m.tree.setSort(sortMtime)
	case "/":
		m.filtering = true
		m.filter = ""
	case "enter", "l", "right":
		m.tree.down()
		return m, nil
	}
	return m, m.tree.tableUpdate(msg)
}

func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.confirm {
		return m, nil
	}
	p := msg.Mouse()
	switch m.view {
	case viewCategory:
		return m.catClick(p.Y)
	case viewBrowser:
		return m.browserClick(p.Y)
	default:
		return m.dashClick(p.X, p.Y)
	}
}

func (m Model) openCategory(cat int) (tea.Model, tea.Cmd) {
	m.cat = newCategory(m.dash.cats[cat], m.dash.results[cat], m.dash.apparent)
	m.cat.resize(m.width, m.height)
	m.filter = ""
	m.view = viewCategory
	return m, nil
}

func (m Model) dashClick(x, y int) (tea.Model, tea.Cmd) {
	if cat := m.dash.treemapHit(x, y); cat >= 0 {
		return m.openCategory(cat)
	}
	idx, ok := m.dash.rowAt(y)
	if !ok {
		return m, nil
	}
	m.dash.cursor = idx
	r := m.dash.rows()[idx]
	if r.kind == rowGroup {
		m.dash.toggle(r.tier)
		return m, nil
	}
	return m.openCategory(r.cat)
}

// catClick selects a row; clicking the selected row opens it.
func (m Model) catClick(y int) (tea.Model, tea.Cmd) {
	idx, ok := m.cat.rowAt(y)
	if !ok {
		return m, nil
	}
	already := idx == m.cat.cursor
	m.cat.cursor = idx
	if already {
		if path, ok := m.cat.selectedPath(); ok {
			return m, m.openBrowser(path)
		}
	}
	return m, nil
}

// browserClick selects a row; clicking the selected row descends into it.
func (m Model) browserClick(y int) (tea.Model, tea.Cmd) {
	idx, ok := m.tree.rowAt(y)
	if !ok {
		return m, nil
	}
	already := idx == m.tree.table.Cursor()
	m.tree.table.SetCursor(idx)
	if already {
		m.tree.down()
	}
	return m, nil
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) render() string {
	m.sync()

	if m.confirm {
		return m.renderConfirm()
	}

	var body string
	switch m.view {
	case viewCategory:
		body = m.cat.render()
	case viewBrowser:
		body = m.tree.render()
	default:
		body = m.dash.render()
	}
	if m.filtering || m.filter != "" {
		line := "filter: " + m.filter
		if m.filtering {
			line += "▏"
		}
		body += "\n" + styleDim.Render("  "+line)
	}
	if m.prompting || m.prompt != "" {
		line := "open folder: " + m.prompt
		if m.prompting {
			line += "▏"
		}
		body += "\n" + styleDim.Render("  "+line)
	}
	if m.status != "" {
		body += "\n" + styleDim.Render("  "+m.status)
	}
	return body
}
