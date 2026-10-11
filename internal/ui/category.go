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
	cursor   int
	apparent bool
	width    int
	height   int
}

func newCategory(c catalog.Category, res scope.CategoryResult, apparent bool) category {
	m := category{cat: c, res: res, apparent: apparent}
	m.order = make([]int, len(res.Matches))
	for i := range m.order {
		m.order[i] = i
	}
	sort.SliceStable(m.order, func(a, b int) bool {
		return res.Matches[m.order[a]].Allocated > res.Matches[m.order[b]].Allocated
	})
	return m
}

func (c *category) resize(w, h int) { c.width, c.height = w, h }

func (c *category) move(delta int) {
	if len(c.order) == 0 {
		return
	}
	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = 0
	}
	if c.cursor >= len(c.order) {
		c.cursor = len(c.order) - 1
	}
}

func (c category) selectedPath() (string, bool) {
	if len(c.order) == 0 {
		return "", false
	}
	return c.res.Matches[c.order[c.cursor]].Path, true
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

	if len(c.order) == 0 {
		b.WriteString(styleDim.Render("  nothing found for this category"))
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
	if end > len(c.order) {
		end = len(c.order)
	}

	for pos := start; pos < end; pos++ {
		m := c.res.Matches[c.order[pos]]
		path := clip(m.Path, c.width-16)
		if pos == c.cursor {
			path = styleSelected.Render("▸ " + clip(m.Path, c.width-18))
			fmt.Fprintf(&b, "%s %s\n", path, styleSize.Render(c.sizeOf(m)+" "))
			continue
		}
		b.WriteString(padBetween("  "+path, styleSize.Render(c.sizeOf(m)+" "), c.width))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(divider(c.width))
	b.WriteByte('\n')
	b.WriteString(styleDim.Render(fmt.Sprintf("  %d items · Enter Open · Esc Back · Q Quit", len(c.order))))
	return b.String()
}
