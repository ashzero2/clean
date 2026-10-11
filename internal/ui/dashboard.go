package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

const (
	sidebarWidth = 24
	donutWidth   = 26
	donutHeight  = 11
)

var tierOrder = []catalog.Tier{catalog.Safe, catalog.Rebuildable, catalog.Review, catalog.UserData}

func tierLabel(t catalog.Tier) string {
	switch t {
	case catalog.Safe:
		return "Safe caches"
	case catalog.Rebuildable:
		return "Rebuildable"
	case catalog.Review:
		return "Review"
	case catalog.UserData:
		return "User folders"
	}
	return "Other"
}

type rowKind uint8

const (
	rowGroup rowKind = iota
	rowCategory
)

type dashRow struct {
	kind rowKind
	tier catalog.Tier
	cat  int
}

type dashboard struct {
	cats     []catalog.Category
	results  []scope.CategoryResult
	state    []rowState
	roots    []string
	workers  int
	apparent bool
	exclude  []string

	ch   chan categoryMsg
	done bool

	expanded map[catalog.Tier]bool
	cursor   int
	width    int
	height   int

	vol         volume.Usage
	reclaimable int64
}

func newDashboard(cats []catalog.Category, roots []string, workers int, root string, apparent bool) dashboard {
	d := dashboard{
		cats:     cats,
		results:  make([]scope.CategoryResult, len(cats)),
		state:    make([]rowState, len(cats)),
		roots:    roots,
		workers:  workers,
		apparent: apparent,
		expanded: map[catalog.Tier]bool{},
		ch:       make(chan categoryMsg, len(cats)+1),
	}
	d.vol, _ = volume.Stat(root)
	return d
}

func (d dashboard) Init() tea.Cmd {
	go d.measure()
	return waitForResult(d.ch)
}

func (d dashboard) measure() {
	scope.MeasureAllStream(context.Background(), d.cats, d.roots, scan.Options{Exclude: d.exclude}, d.workers, func(i int, r scope.CategoryResult) {
		d.ch <- categoryMsg{index: i, res: r}
	})
	close(d.ch)
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

func (d *dashboard) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case categoryMsg:
		d.results[msg.index] = msg.res
		d.state[msg.index] = rowDone
		if msg.res.Category.Tier.Reclaimable() {
			d.reclaimable += d.valueOf(msg.index)
		}
		return waitForResult(d.ch)
	case scanDoneMsg:
		d.done = true
	}
	return nil
}

func (d *dashboard) rescan() tea.Cmd {
	d.results = make([]scope.CategoryResult, len(d.cats))
	d.state = make([]rowState, len(d.cats))
	d.reclaimable = 0
	d.done = false
	d.ch = make(chan categoryMsg, len(d.cats)+1)
	go d.measure()
	return waitForResult(d.ch)
}

func (d *dashboard) resize(w, h int) { d.width, d.height = w, h }

func (d dashboard) valueOf(i int) int64 {
	if d.state[i] == rowPending {
		return -1
	}
	if d.apparent {
		return d.results[i].Apparent
	}
	return d.results[i].Allocated
}

func (d dashboard) catIndices(tier catalog.Tier) []int {
	var idx []int
	for i := range d.cats {
		if d.cats[i].Tier == tier {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return d.valueOf(idx[a]) > d.valueOf(idx[b]) })
	return idx
}

func (d dashboard) groupTotal(tier catalog.Tier) int64 {
	var total int64
	for _, i := range d.catIndices(tier) {
		if v := d.valueOf(i); v > 0 {
			total += v
		}
	}
	return total
}

// rows flattens the group headers plus the categories of any expanded group.
func (d dashboard) rows() []dashRow {
	var rows []dashRow
	for _, tier := range tierOrder {
		idx := d.catIndices(tier)
		if len(idx) == 0 {
			continue
		}
		rows = append(rows, dashRow{kind: rowGroup, tier: tier})
		if d.expanded[tier] {
			for _, i := range idx {
				rows = append(rows, dashRow{kind: rowCategory, tier: tier, cat: i})
			}
		}
	}
	return rows
}

func (d *dashboard) move(delta int) {
	rows := d.rows()
	if len(rows) == 0 {
		return
	}
	d.cursor += delta
	if d.cursor < 0 {
		d.cursor = 0
	}
	if d.cursor >= len(rows) {
		d.cursor = len(rows) - 1
	}
}

func (d dashboard) currentRow() (dashRow, bool) {
	rows := d.rows()
	if d.cursor < 0 || d.cursor >= len(rows) {
		return dashRow{}, false
	}
	return rows[d.cursor], true
}

func (d *dashboard) toggle(tier catalog.Tier) { d.expanded[tier] = !d.expanded[tier] }

// forget subtracts reclaimed bytes from a category after a deletion.
func (d *dashboard) forget(id string, freed int64) {
	for i := range d.cats {
		if d.cats[i].ID != id {
			continue
		}
		if d.results[i].Allocated -= freed; d.results[i].Allocated < 0 {
			d.results[i].Allocated = 0
		}
		if d.cats[i].Tier.Reclaimable() {
			if d.reclaimable -= freed; d.reclaimable < 0 {
				d.reclaimable = 0
			}
		}
		return
	}
}

func (d dashboard) donutSizes() []int64 {
	vals := make([]int64, 0, len(d.cats))
	for i := range d.cats {
		if v := d.valueOf(i); v > 0 {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(a, b int) bool { return vals[a] > vals[b] })
	const top = 8
	if len(vals) <= top {
		return vals
	}
	out := append([]int64(nil), vals[:top]...)
	var other int64
	for _, v := range vals[top:] {
		other += v
	}
	if other > 0 {
		out = append(out, other)
	}
	return out
}

func (d dashboard) render() string {
	if d.width < 40 || d.height < 10 {
		return tooSmall
	}

	header := padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("macOS · Developer Cleanup "), d.width)
	div := divider(d.width)

	bodyHeight := d.height - 4
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	showSidebar := d.width >= 84
	mainWidth := d.width
	if showSidebar {
		mainWidth -= sidebarWidth + 2
	}
	showDonut := mainWidth >= 70

	var cols []string
	if showSidebar {
		cols = append(cols, lipgloss.NewStyle().Width(sidebarWidth).Height(bodyHeight).Render(d.sidebar()))
		cols = append(cols, "  ")
	}
	cols = append(cols, d.main(bodyHeight, mainWidth, showDonut))
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	footer := styleDim.Render("  ↑↓ Navigate · Enter Expand · R Rescan · Q Quit")
	return header + "\n" + div + "\n" + body + "\n" + div + "\n" + footer
}

func (d dashboard) main(height, width int, showDonut bool) string {
	var b strings.Builder

	ratio := 0.0
	if d.vol.Total > 0 {
		ratio = float64(d.vol.Used) / float64(d.vol.Total)
	}
	groupsWidth := width
	if showDonut {
		groupsWidth = width - donutWidth - 2
	}
	if groupsWidth < 30 {
		groupsWidth = width
		showDonut = false
	}

	b.WriteString(padBetween(" Disk usage", fmt.Sprintf("%s / %s ", format.Bytes(d.vol.Used), format.Bytes(d.vol.Total)), groupsWidth))
	b.WriteByte('\n')
	b.WriteString(" " + renderBar(ratio, groupsWidth-2) + " ")
	b.WriteByte('\n')
	b.WriteString(padBetween(styleDim.Render(" Potentially reclaimable"), styleTotal.Render(format.Bytes(d.reclaimable)+" "), groupsWidth))
	b.WriteString("\n\n")

	rows := d.rows()
	available := height - 6
	if available < 1 {
		available = 1
	}
	start := 0
	if d.cursor >= available {
		start = d.cursor - available + 1
	}
	end := start + available
	if end > len(rows) {
		end = len(rows)
	}

	for pos := start; pos < end; pos++ {
		r := rows[pos]
		var line string
		if r.kind == rowGroup {
			marker := "▸"
			if d.expanded[r.tier] {
				marker = "▾"
			}
			left := fmt.Sprintf(" %s %s", marker, tierLabel(r.tier))
			right := fmt.Sprintf("%s  (%d) ", format.Bytes(d.groupTotal(r.tier)), len(d.catIndices(r.tier)))
			line = padBetween(left, right, groupsWidth)
		} else {
			left := "     " + d.cats[r.cat].Label
			line = padBetween(left, format.Bytes(d.valueOf(r.cat))+" ", groupsWidth)
		}
		if pos == d.cursor {
			line = styleSelected.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	groups := b.String()

	if !showDonut {
		return groups
	}
	donut := lipgloss.NewStyle().PaddingLeft(2).Render(renderDonut(d.donutSizes(), donutWidth, donutHeight))
	return lipgloss.JoinHorizontal(lipgloss.Top, groups, donut)
}

func (d dashboard) sidebar() string {
	var b strings.Builder
	b.WriteString(styleDim.Render("Volumes"))
	b.WriteByte('\n')
	b.WriteString(" Macintosh HD\n")
	b.WriteString(fmt.Sprintf(" %s / %s\n", format.Bytes(d.vol.Used), format.Bytes(d.vol.Total)))
	ratio := 0.0
	if d.vol.Total > 0 {
		ratio = float64(d.vol.Used) / float64(d.vol.Total)
	}
	b.WriteString(" " + renderBar(ratio, sidebarWidth-3))
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render("Folders"))
	b.WriteByte('\n')
	for i := range d.cats {
		if d.cats[i].Kind != catalog.KindUserFolder {
			continue
		}
		b.WriteString(padBetween(" "+d.cats[i].Label, format.Bytes(d.valueOf(i))+" ", sidebarWidth))
		b.WriteByte('\n')
	}
	return b.String()
}
