package ui

import (
	"fmt"
	"sort"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rahul-17280/sweep/internal/format"
)

var styleConfirmBox = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colAccent).
	Padding(1, 3)

func (m *Model) toggleMark(path string, size int64, node int32) {
	if m.marks == nil {
		m.marks = map[string]mark{}
	}
	if _, ok := m.marks[path]; ok {
		delete(m.marks, path)
		return
	}
	m.marks[path] = mark{size: size, node: node}
}

func (m *Model) initiateDelete() {
	m.status = ""
	if !m.opts.AllowDelete {
		m.status = "deleting is disabled (--no-delete)"
		return
	}
	if len(m.marks) == 0 {
		m.status = "nothing marked — press Space to mark items"
		return
	}
	m.pending = make([]string, 0, len(m.marks))
	for p := range m.marks {
		m.pending = append(m.pending, p)
	}
	sort.Strings(m.pending)
	m.confirm = true
}

func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "y", "enter":
		m.performDelete()
	case "n", "esc", "q":
		m.confirm = false
		m.pending = nil
	}
	return m, nil
}

func (m *Model) performDelete() {
	paths := append([]string(nil), m.pending...)
	m.confirm = false
	m.pending = nil

	set := make(map[string]bool, len(paths))
	var freed int64
	var nodes []int32
	for _, p := range paths {
		set[p] = true
		mk := m.marks[p]
		freed += mk.size
		if mk.node >= 0 {
			nodes = append(nodes, mk.node)
		}
	}

	var err error
	if m.opts.Permanent {
		err = m.opts.Trasher.Remove(paths)
	} else {
		err = m.opts.Trasher.Trash(paths)
	}
	if err != nil {
		m.status = "delete failed: " + err.Error()
		return
	}

	switch m.view {
	case viewCategory:
		m.cat.removePaths(set)
	case viewBrowser:
		m.tree.removeNodes(nodes)
	}
	m.dash.forget(m.cat.cat.ID, freed)
	m.marks = map[string]mark{}
	m.status = "reclaimed " + format.Bytes(freed)
}

func (m Model) renderConfirm() string {
	verb := "Move to Trash"
	if m.opts.Permanent {
		verb = "Permanently delete"
	}
	var total int64
	for _, p := range m.pending {
		total += m.marks[p].size
	}
	body := fmt.Sprintf("%s %d item(s)?\n\nreclaim %s\n\n[y] confirm    [n] cancel",
		verb, len(m.pending), format.Bytes(total))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, styleConfirmBox.Render(body))
}
