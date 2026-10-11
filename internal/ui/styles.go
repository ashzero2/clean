package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	colAccent = lipgloss.Color("#e8a06a")
	colSlate  = lipgloss.Color("#3b4a5a")
	colGreen  = lipgloss.Color("#8fbf7f")
	colDim    = lipgloss.Color("#6b7280")

	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleSubtitle = lipgloss.NewStyle().Foreground(colDim)
	styleDim      = lipgloss.NewStyle().Foreground(colDim)
	styleSize     = lipgloss.NewStyle().Bold(true)
	styleTotal    = lipgloss.NewStyle().Bold(true).Foreground(colGreen)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleMark     = lipgloss.NewStyle().Foreground(colGreen)
	styleBarFull  = lipgloss.NewStyle().Foreground(colAccent)
	styleBarEmpty = lipgloss.NewStyle().Foreground(colSlate)
	styleDivider  = lipgloss.NewStyle().Foreground(colSlate)
)

func divider(width int) string {
	if width < 1 {
		return ""
	}
	return styleDivider.Render(strings.Repeat("─", width))
}

func renderBar(ratio float64, width int) string {
	if width < 1 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if ratio > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return styleBarFull.Render(strings.Repeat("█", filled)) + styleBarEmpty.Render(strings.Repeat("░", width-filled))
}

// padBetween places left and right at the ends of a width-wide line.
func padBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

const tooSmall = "terminal too small — press q to quit"

// clip keeps the last n runes of s (the informative end of a path), rune-safe.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return "…" + string(r[len(r)-(n-1):])
}
