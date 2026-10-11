package ui

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

var donutPalette = []color.Color{
	lipgloss.Color("#e8a06a"), lipgloss.Color("#6aa9e8"), lipgloss.Color("#8fbf7f"), lipgloss.Color("#b48ead"),
	lipgloss.Color("#e8d06a"), lipgloss.Color("#6ae8d0"), lipgloss.Color("#e86a9c"), lipgloss.Color("#e87a6a"),
}

// renderDonut draws a ring of coloured cells sized by the given slices. It is a
// coarse terminal approximation of a sunburst: resolution is one cell, so keep
// the widget small (roughly 26x11).
func renderDonut(sizes []int64, w, h int) string {
	if w < 8 || h < 5 || len(sizes) == 0 {
		return ""
	}
	var total int64
	for _, s := range sizes {
		total += s
	}
	if total <= 0 {
		return ""
	}

	cx := float64(w-1) / 2
	cy := float64(h-1) / 2
	rx := float64(w) / 2
	ry := float64(h) / 2

	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			dx := (float64(x) - cx) / rx
			dy := (float64(y) - cy) / ry
			r := math.Hypot(dx, dy)
			if r > 1.0 || r < 0.62 {
				b.WriteByte(' ')
				continue
			}
			ang := math.Atan2(dy, dx)
			if ang < 0 {
				ang += 2 * math.Pi
			}
			idx := sliceAt(sizes, total, ang/(2*math.Pi))
			b.WriteString(lipgloss.NewStyle().Foreground(donutPalette[idx%len(donutPalette)]).Render("█"))
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

func sliceAt(sizes []int64, total int64, frac float64) int {
	acc := 0.0
	for i, s := range sizes {
		acc += float64(s) / float64(total)
		if frac <= acc {
			return i
		}
	}
	return len(sizes) - 1
}
