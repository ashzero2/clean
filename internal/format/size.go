package format

import (
	"strconv"
	"strings"
)

const unit = 1024

func Bytes(n int64) string {
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	const prefixes = "KMGTPE"
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string(prefixes[exp]) + "iB"
}

func BytesShort(n int64) string {
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	const prefixes = "KMGTPE"
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + string(prefixes[exp])
}

func Thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func Bar(used, total int64, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	if total <= 0 {
		return strings.Repeat("░", width), 0
	}
	ratio := float64(used) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if used > 0 && filled == 0 {
		filled = 1
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled), filled
}

func Percent(part, whole int64) string {
	if whole <= 0 {
		return "0%"
	}
	p := int(float64(part)/float64(whole)*100 + 0.5)
	if p > 100 {
		p = 100
	}
	return strconv.Itoa(p) + "%"
}
