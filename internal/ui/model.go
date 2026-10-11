// Package ui renders the interactive dashboard. It is the only package that
// depends on Bubble Tea / Lip Gloss; everything it displays comes from catalog,
// scope and volume.
package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/scope"
	"github.com/rahul-17280/sweep/internal/volume"
)

type rowState uint8

const (
	rowPending rowState = iota
	rowDone
)

type categoryMsg struct {
	index int
	res   scope.CategoryResult
}

type scanDoneMsg struct{}

type Model struct {
	cats     []catalog.Category
	results  []scope.CategoryResult
	state    []rowState
	roots    []string
	workers  int
	apparent bool

	ch   chan categoryMsg
	done bool

	cursor int32
	width  int
	height int

	vol         volume.Usage
	reclaimable int64
}

func New(cats []catalog.Category, roots []string, workers int, root string, apparent bool) Model {
	m := Model{
		cats:     cats,
		results:  make([]scope.CategoryResult, len(cats)),
		state:    make([]rowState, len(cats)),
		roots:    roots,
		workers:  workers,
		apparent: apparent,
		ch:       make(chan categoryMsg, len(cats)+1),
	}
	m.vol, _ = volume.Stat(root)
	return m
}

func (m Model) Init() tea.Cmd {
	go m.measure()
	return waitForResult(m.ch)
}

func (m Model) measure() {
	scope.MeasureAllStream(context.Background(), m.cats, m.roots, scan.Options{}, m.workers, func(i int, r scope.CategoryResult) {
		m.ch <- categoryMsg{index: i, res: r}
	})
	close(m.ch)
}

func waitForResult(ch <-chan categoryMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return scanDoneMsg{}
		}
		return msg
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "r":
			return m.rescan()
		}

	case categoryMsg:
		m.results[msg.index] = msg.res
		m.state[msg.index] = rowDone
		if msg.res.Category.Tier.Reclaimable() {
			m.reclaimable += m.valueOf(msg.index)
		}
		return m, waitForResult(m.ch)

	case scanDoneMsg:
		m.done = true
	}
	return m, nil
}

func (m Model) rescan() (tea.Model, tea.Cmd) {
	m.results = make([]scope.CategoryResult, len(m.cats))
	m.state = make([]rowState, len(m.cats))
	m.reclaimable = 0
	m.done = false
	m.ch = make(chan categoryMsg, len(m.cats)+1)
	go m.measure()
	return m, waitForResult(m.ch)
}

func (m Model) valueOf(i int) int64 {
	if m.state[i] == rowPending {
		return -1
	}
	if m.apparent {
		return m.results[i].Apparent
	}
	return m.results[i].Allocated
}

// order yields category indices by descending size, with pending rows last.
func (m Model) order() []int {
	idx := make([]int, len(m.cats))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return m.valueOf(idx[a]) > m.valueOf(idx[b])
	})
	return idx
}

func (m *Model) move(delta int) {
	ord := m.order()
	if len(ord) == 0 {
		return
	}
	pos := 0
	for i, idx := range ord {
		if int32(idx) == m.cursor {
			pos = i
			break
		}
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(ord) {
		pos = len(ord) - 1
	}
	m.cursor = int32(ord[pos])
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width < 30 || m.height < 8 {
		return "terminal too small — press q to quit"
	}

	var b strings.Builder
	b.WriteString(padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("macOS · Developer Cleanup "), m.width))
	b.WriteByte('\n')
	b.WriteString(divider(m.width))
	b.WriteString("\n\n")

	ratio := 0.0
	if m.vol.Total > 0 {
		ratio = float64(m.vol.Used) / float64(m.vol.Total)
	}
	b.WriteString(padBetween(" Disk usage", fmt.Sprintf("%s / %s ", format.Bytes(m.vol.Used), format.Bytes(m.vol.Total)), m.width))
	b.WriteByte('\n')
	b.WriteString(" " + renderBar(ratio, m.width-2) + " ")
	b.WriteByte('\n')
	b.WriteString(padBetween(styleDim.Render(" Potentially reclaimable"), styleTotal.Render(format.Bytes(m.reclaimable)+" "), m.width))
	b.WriteString("\n\n")
	b.WriteString(divider(m.width))
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render(" Categorized storage"))
	b.WriteByte('\n')

	ord := m.order()
	maxSize := int64(1)
	for i := range m.cats {
		if v := m.valueOf(i); v > maxSize {
			maxSize = v
		}
	}
	barWidth := m.width - 4
	if barWidth < 8 {
		barWidth = 8
	}
	available := (m.height - 12) / 2
	if available < 1 {
		available = 1
	}

	for shown, i := range ord {
		if shown >= available {
			b.WriteString(styleDim.Render(fmt.Sprintf("  … %d more", len(ord)-shown)))
			b.WriteByte('\n')
			break
		}
		selected := int32(i) == m.cursor
		label := "  " + m.cats[i].Label
		if selected {
			label = styleSelected.Render("▸ " + m.cats[i].Label)
		}
		size := "—"
		if m.state[i] == rowDone {
			size = format.Bytes(m.valueOf(i))
		}
		b.WriteString(padBetween(label, styleSize.Render(size+" "), m.width))
		b.WriteByte('\n')

		r := 0.0
		if v := m.valueOf(i); v > 0 {
			r = float64(v) / float64(maxSize)
		}
		b.WriteString(" " + renderBar(r, barWidth))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(divider(m.width))
	b.WriteByte('\n')
	b.WriteString(styleDim.Render("  ↑↓ Navigate · R Rescan · Q Quit"))
	return b.String()
}
