package ui

import "testing"

func TestClipNonPositiveWidth(t *testing.T) {
	for _, n := range []int{-5, -2, 0} {
		if got := clip("hello world", n); got != "" {
			t.Errorf("clip(n=%d) = %q, want empty", n, got)
		}
	}
	if got := clip("hello", 1); got != "…" {
		t.Errorf("clip(1) = %q, want …", got)
	}
	if got := clip("hi", 10); got != "hi" {
		t.Errorf("clip(10) = %q, want hi", got)
	}
}

// Regression: many items in a small box produced 1-wide tiles whose label
// computed a negative width and panicked.
func TestRenderTreemapNeverPanics(t *testing.T) {
	items := make([]tmItem, 20)
	for i := range items {
		items[i] = tmItem{label: "category-with-a-long-name", size: int64(i + 1)}
	}
	for w := 1; w <= 40; w++ {
		for h := 1; h <= 14; h++ {
			_ = renderTreemap(items, w, h)
		}
	}
}
