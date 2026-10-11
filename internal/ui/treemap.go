package ui

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rahul-17280/sweep/internal/format"
)

var tilePalette = []color.Color{
	lipgloss.Color("#e8a06a"), lipgloss.Color("#6aa9e8"), lipgloss.Color("#8fbf7f"), lipgloss.Color("#b48ead"),
	lipgloss.Color("#e8d06a"), lipgloss.Color("#6ae8d0"), lipgloss.Color("#e86a9c"), lipgloss.Color("#e87a6a"),
}

type tmItem struct {
	label string
	size  int64
}

type tmRect struct {
	idx        int
	x, y, w, h int
}

// renderTreemap lays items out with the squarified algorithm and draws them as
// coloured blocks, labelling tiles that are large enough. Area is proportional
// to size, so it reads as a real composition rather than a decorative ring.
func renderTreemap(items []tmItem, w, h int) string {
	if w < 12 || h < 4 || len(items) == 0 {
		return ""
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].size > items[b].size })

	var total int64
	for _, it := range items {
		total += it.size
	}
	if total <= 0 {
		return ""
	}

	areas := make([]float64, len(items))
	for i, it := range items {
		areas[i] = float64(it.size) * float64(w*h) / float64(total)
	}
	rects := squarify(areas, 0, 0, w, h)

	tile := make([][]int, h)
	label := make([][]rune, h)
	for y := range tile {
		tile[y] = make([]int, w)
		label[y] = make([]rune, w)
		for x := range tile[y] {
			tile[y][x] = -1
		}
	}
	for _, r := range rects {
		for y := max(0, r.y); y < r.y+r.h && y < h; y++ {
			for x := max(0, r.x); x < r.x+r.w && x < w; x++ {
				tile[y][x] = r.idx
			}
		}
	}
	for _, r := range rects {
		if r.h < 1 || r.w < 6 || r.x < 0 || r.y < 0 || r.y >= h {
			continue
		}
		text := fmt.Sprintf("%s %s", clip(items[r.idx].label, r.w-2), format.Bytes(items[r.idx].size))
		if r.w < len([]rune(text))+1 {
			continue
		}
		for k, ch := range []rune(text) {
			x := r.x + 1 + k
			if x >= r.x+r.w || x >= w {
				break
			}
			label[r.y][x] = ch
		}
	}

	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := tile[y][x]
			if t < 0 {
				b.WriteByte(' ')
				continue
			}
			style := lipgloss.NewStyle().Foreground(tilePalette[t%len(tilePalette)])
			if label[y][x] != 0 {
				b.WriteString(style.Bold(true).Render(string(label[y][x])))
				continue
			}
			b.WriteString(style.Render("█"))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// squarify lays areas (already scaled to sum to about w*h) into the rectangle,
// greedily growing rows along the shorter side to keep tiles close to square.
func squarify(areas []float64, x, y, w, h int) []tmRect {
	rects := make([]tmRect, len(areas))
	cx, cy, cw, ch := x, y, w, h

	for i := 0; i < len(areas); {
		if cw <= 0 || ch <= 0 {
			break
		}
		short := cw
		if ch < cw {
			short = ch
		}

		rowStart := i
		rowSum := 0.0
		best := math.Inf(1)
		for i < len(areas) {
			thick := (rowSum + areas[i]) / float64(short)
			worst := 0.0
			for j := rowStart; j <= i; j++ {
				side := areas[j] / thick
				aspect := side / thick
				if aspect < 1 {
					aspect = 1 / aspect
				}
				if aspect > worst {
					worst = aspect
				}
			}
			if i == rowStart || worst <= best {
				best = worst
				rowSum += areas[i]
				i++
			} else {
				break
			}
		}

		if cw <= ch {
			thick := rowSum / float64(cw)
			ox := float64(cx)
			for j := rowStart; j < i; j++ {
				ow := areas[j] / thick
				rects[j] = tmRect{j, int(ox), cy, int(ow + 0.5), int(thick + 0.5)}
				ox += ow
			}
			cy += int(thick + 0.5)
			ch -= int(thick + 0.5)
		} else {
			thick := rowSum / float64(ch)
			oy := float64(cy)
			for j := rowStart; j < i; j++ {
				oh := areas[j] / thick
				rects[j] = tmRect{j, cx, int(oy), int(thick + 0.5), int(oh + 0.5)}
				oy += oh
			}
			cx += int(thick + 0.5)
			cw -= int(thick + 0.5)
		}
	}
	return rects
}
