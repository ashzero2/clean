// Package ui renders the interactive dashboard. It is the only package that
// depends on Bubble Tea / Lip Gloss; everything it displays comes from catalog,
// scope, tree and volume.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
)

type viewKind uint8

const (
	viewDashboard viewKind = iota
	viewCategory
	viewBrowser
)

type Model struct {
	width  int
	height int
	view   viewKind

	dash dashboard
	cat  category
	tree browser
}

func New(cats []catalog.Category, roots []string, workers int, root string, apparent bool) Model {
	return Model{dash: newDashboard(cats, roots, workers, root, apparent)}
}

func (m Model) Init() tea.Cmd {
	return m.dash.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
		m.dash.resize(ws.Width, ws.Height)
		m.cat.resize(ws.Width, ws.Height)
		m.tree.resize(ws.Width, ws.Height)
		return m, nil
	}

	// Category scan messages arrive regardless of the active view.
	switch msg := msg.(type) {
	case categoryMsg, scanDoneMsg:
		return m, m.dash.update(msg)
	case treeMsg:
		m.tree.apply(msg)
		return m, nil
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
	case "enter", "l", "right":
		if path, ok := m.cat.selectedPath(); ok {
			m.tree = newBrowser(path, m.width, m.height, m.dash.apparent)
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
			m.view = viewCategory
		}
		return m, nil
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
	switch m.view {
	case viewCategory:
		return m.cat.render()
	case viewBrowser:
		return m.tree.render()
	default:
		return m.dash.render()
	}
}
