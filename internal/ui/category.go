package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/format"
	"github.com/rahul-17280/sweep/internal/scope"
)

type category struct {
	cat      catalog.Category
	res      scope.CategoryResult
	order    []int
	marks    map[string]mark
	filter   string
	cursor   int
	apparent bool
	width    int
	height   int
}

func newCategory(c catalog.Category, res scope.CategoryResult, apparent bool) category {
	m := category{cat: c, res: res, apparent: apparent}
	m.reindex()
	return m
}

func (c *category) reindex() {
	c.order = make([]int, len(c.res.Matches))
	for i := range c.order {
		c.order[i] = i
	}
	sort.SliceStable(c.order, func(a, b int) bool {
		return c.res.Matches[c.order[a]].Allocated > c.res.Matches[c.order[b]].Allocated
	})
	c.clamp()
}

func (c *category) clamp() {
	if c.cursor >= len(c.visible()) {
		c.cursor = len(c.visible()) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

// visible lists the result indices passing the current filter.
func (c category) visible() []int {
	if c.filter == "" {
		return c.order
	}
	out := make([]int, 0, len(c.order))
	for _, i := range c.order {
		if strings.Contains(c.res.Matches[i].Path, c.filter) {
			out = append(out, i)
		}
	}
	return out
}

func (c *category) resize(w, h int) { c.width, c.height = w, h }

func (c *category) move(delta int) {
	if len(c.visible()) == 0 {
		return
	}
	c.cursor += delta
	c.clamp()
}

func (c category) current() (scope.Match, bool) {
	v := c.visible()
	if len(v) == 0 || c.cursor >= len(v) {
		return scope.Match{}, false
	}
	return c.res.Matches[v[c.cursor]], true
}

func (c category) selectedPath() (string, bool) {
	m, ok := c.current()
	return m.Path, ok
}

// removePaths drops the given paths from the result and recomputes its totals.
func (c *category) removePaths(set map[string]bool) {
	kept := c.res.Matches[:0]
	var alloc, apparent, items int64
	for _, m := range c.res.Matches {
		if set[m.Path] {
			continue
		}
		kept = append(kept, m)
		alloc += m.Allocated
		apparent += m.Apparent
		items += m.Items
	}
	c.res.Matches = kept
	c.res.Allocated = alloc
	c.res.Apparent = apparent
	c.res.Items = items
	c.reindex()
}

func (c category) sizeOf(m scope.Match) string {
	if c.apparent {
		return format.Bytes(m.Apparent)
	}
	return format.Bytes(m.Allocated)
}

func (c category) render() string {
	if c.width < 30 || c.height < 6 {
		return tooSmall
	}

	var b strings.Builder
	b.WriteString(padBetween(styleTitle.Render(" sweep"), styleSubtitle.Render("  "+c.cat.Label+"  "), c.width))
	b.WriteByte('\n')
	b.WriteString(divider(c.width))
	b.WriteString("\n\n")

	vis := c.visible()
	if len(vis) == 0 {
		b.WriteString(styleDim.Render("  nothing to show"))
		b.WriteByte('\n')
	}

	available := c.height - 8
	if available < 1 {
		available = 1
	}
	start := 0
	if c.cursor >= available {
		start = c.cursor - available + 1
	}
	end := start + available
	if end > len(vis) {
		end = len(vis)
	}

	for pos := start; pos < end; pos++ {
		m := c.res.Matches[vis[pos]]
		prefix := "  "
		if _, ok := c.marks[m.Path]; ok {
			prefix = styleMark.Render("●") + " "
		}
		if pos == c.cursor {
			prefix = styleSelected.Render("▸") + " "
		}
		left := prefix + clip(m.Path, c.width-16)
		b.WriteString(padBetween(left, styleSize.Render(c.sizeOf(m)+" "), c.width))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(divider(c.width))
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s", styleDim.Render(fmt.Sprintf(
		"  %d shown · %d marked · Space Mark · / Filter · D Delete · Esc Back · Q Quit",
		len(vis), len(c.marks))))
	return b.String()
}
