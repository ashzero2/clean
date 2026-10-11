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

type dashboard struct {
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

func newDashboard(cats []catalog.Category, roots []string, workers int, root string, apparent bool) dashboard {
	d := dashboard{
		cats:     cats,
		results:  make([]scope.CategoryResult, len(cats)),
		state:    make([]rowState, len(cats)),
		roots:    roots,
		workers:  workers,
		apparent: apparent,
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
	scope.MeasureAllStream(context.Background(), d.cats, d.roots, scan.Options{}, d.workers, func(i int, r scope.CategoryResult) {
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

// order yields category indices by descending size, with pending rows last.
func (d dashboard) order() []int {
	idx := make([]int, len(d.cats))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return d.valueOf(idx[a]) > d.valueOf(idx[b]) })
	return idx
}

func (d *dashboard) move(delta int) {
	ord := d.order()
	if len(ord) == 0 {
		return
	}
	pos := 0
	for i, idx := range ord {
		if int32(idx) == d.cursor {
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
	d.cursor = int32(ord[pos])
}

func (d dashboard) selected() (catalog.Category, scope.CategoryResult, bool) {
	i := int(d.cursor)
	if i < 0 || i >= len(d.cats) || d.state[i] == rowPending {
		return catalog.Category{}, scope.CategoryResult{}, false
	}
	return d.cats[i], d.results[i], true
}

func (d dashboard) render() string {
	if d.width < 30 || d.height < 8 {
		return tooSmall
	}

	var b strings.Builder
	b.WriteString(padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("macOS · Developer Cleanup "), d.width))
	b.WriteByte('\n')
	b.WriteString(divider(d.width))
	b.WriteString("\n\n")

	ratio := 0.0
	if d.vol.Total > 0 {
		ratio = float64(d.vol.Used) / float64(d.vol.Total)
	}
	b.WriteString(padBetween(" Disk usage", fmt.Sprintf("%s / %s ", format.Bytes(d.vol.Used), format.Bytes(d.vol.Total)), d.width))
	b.WriteByte('\n')
	b.WriteString(" " + renderBar(ratio, d.width-2) + " ")
	b.WriteByte('\n')
	b.WriteString(padBetween(styleDim.Render(" Potentially reclaimable"), styleTotal.Render(format.Bytes(d.reclaimable)+" "), d.width))
	b.WriteString("\n\n")
	b.WriteString(divider(d.width))
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render(" Categorized storage"))
	b.WriteByte('\n')

	ord := d.order()
	maxSize := int64(1)
	for i := range d.cats {
		if v := d.valueOf(i); v > maxSize {
			maxSize = v
		}
	}
	barWidth := d.width - 4
	if barWidth < 8 {
		barWidth = 8
	}
	available := (d.height - 12) / 2
	if available < 1 {
		available = 1
	}

	for shown, i := range ord {
		if shown >= available {
			b.WriteString(styleDim.Render(fmt.Sprintf("  … %d more", len(ord)-shown)))
			b.WriteByte('\n')
			break
		}
		selected := int32(i) == d.cursor
		label := "  " + d.cats[i].Label
		if selected {
			label = styleSelected.Render("▸ " + d.cats[i].Label)
		}
		size := "—"
		if d.state[i] == rowDone {
			size = format.Bytes(d.valueOf(i))
		}
		b.WriteString(padBetween(label, styleSize.Render(size+" "), d.width))
		b.WriteByte('\n')

		r := 0.0
		if v := d.valueOf(i); v > 0 {
			r = float64(v) / float64(maxSize)
		}
		b.WriteString(" " + renderBar(r, barWidth))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(divider(d.width))
	b.WriteByte('\n')
	b.WriteString(styleDim.Render("  ↑↓ Navigate · Enter Inspect · R Rescan · Q Quit"))
	return b.String()
}
