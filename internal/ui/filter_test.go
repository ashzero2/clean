package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
)

func TestCategoryFilter(t *testing.T) {
	cats := []catalog.Category{{ID: "nm", Label: "node_modules", Tier: catalog.Rebuildable, Kind: catalog.KindDiscover}}
	m := New(cats, Options{Root: "/", Workers: 1})
	m.width, m.height = 80, 24
	m.dash.resize(80, 24)

	next, _ := m.Update(categoryMsg{index: 0, res: scope.CategoryResult{
		Category: cats[0],
		Matches: []scope.Match{
			{Path: "/a/keep/node_modules", Allocated: 2 << 30},
			{Path: "/b/drop/node_modules", Allocated: 1 << 30},
		},
	}})
	m = next.(Model)
	next, _ = m.Update(key(tea.KeyEnter))
	m = next.(Model)

	next, _ = m.Update(key('/'))
	m = next.(Model)
	if !m.filtering {
		t.Fatal("filter mode should be active")
	}
	for _, r := range "keep" {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
	}

	if got := len(m.cat.visible()); got != 1 {
		t.Fatalf("visible = %d, want 1", got)
	}
	out := m.render()
	if !strings.Contains(out, "keep/node_modules") {
		t.Errorf("matching row missing:\n%s", out)
	}
	if strings.Contains(out, "drop") {
		t.Errorf("non-matching row not hidden:\n%s", out)
	}

	// Esc clears the filter and leaves filter mode.
	next, _ = m.Update(key(tea.KeyEscape))
	m = next.(Model)
	if m.filtering || m.filter != "" {
		t.Errorf("esc should clear the filter, got filtering=%v filter=%q", m.filtering, m.filter)
	}
	if got := len(m.cat.visible()); got != 2 {
		t.Errorf("visible = %d, want 2 after clearing", got)
	}
}
