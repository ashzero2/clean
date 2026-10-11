// Package ui renders the interactive dashboard. It is the only package that
// depends on Bubble Tea / Lip Gloss; everything it displays comes from catalog,
// scope, tree, trash and volume.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
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

func (m Model) Init() tea.Cmd {
	return m.dash.Init()
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
	case "enter":
		if c, res, ok := m.dash.selected(); ok {
			m.cat = newCategory(c, res, m.dash.apparent)
			m.cat.resize(m.width, m.height)
			m.filter = ""
			m.view = viewCategory
		}
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
			m.view = viewCategory
		}
		return m, nil
	case "space":
		if idx, path, ok := m.tree.currentChild(); ok {
			m.toggleMark(path, m.tree.tr.Node(idx).Size, idx)
		}
	case "d":
		m.initiateDelete()
	case "/":
		m.filtering = true
		m.filter = ""
	case "enter", "l", "right":
		m.tree.down()
		return m, nil
	}
	return m, m.tree.tableUpdate(msg)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
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
	if m.status != "" {
		body += "\n" + styleDim.Render("  "+m.status)
	}
	return body
}
