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
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m, categoryMsg{index: 0, res: scope.CategoryResult{
		Category: cats[0],
		Matches: []scope.Match{
			{Path: "/a/keep/node_modules", Allocated: 2 << 30},
			{Path: "/b/drop/node_modules", Allocated: 1 << 30},
		},
	}})
	m = openFirstCategory(t, m)
	if m.view != viewCategory {
		t.Fatalf("view = %d, want viewCategory", m.view)
	}

	m = feed(t, m, key('/'))
	if !m.filtering {
		t.Fatal("filter mode should be active")
	}
	for _, r := range "keep" {
		m = feed(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
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

	m = feed(t, m, key(tea.KeyEscape))
	if m.filtering || m.filter != "" {
		t.Errorf("esc should clear the filter, got filtering=%v filter=%q", m.filtering, m.filter)
	}
	if got := len(m.cat.visible()); got != 2 {
		t.Errorf("visible = %d, want 2 after clearing", got)
	}
}
