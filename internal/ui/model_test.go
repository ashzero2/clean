package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
)

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestRenderShowsSizesAndReclaimable(t *testing.T) {
	cats := []catalog.Category{
		{ID: "npm", Label: "npm cache", Tier: catalog.Safe, Kind: catalog.KindPath},
		{ID: "docs", Label: "Documents", Tier: catalog.UserData, Kind: catalog.KindUserFolder},
	}
	m := New(cats, nil, 1, "/", false)
	m.width, m.height = 80, 24
	m.dash.resize(80, 24)

	next, _ := m.Update(categoryMsg{index: 0, res: scope.CategoryResult{Category: cats[0], Allocated: 5 << 30}})
	m = next.(Model)
	next, _ = m.Update(categoryMsg{index: 1, res: scope.CategoryResult{Category: cats[1], Allocated: 1 << 30}})
	m = next.(Model)

	out := m.render()
	t.Log("\n" + out)
	for _, want := range []string{"sweep", "npm cache", "5.0 GiB", "Documents", "Potentially reclaimable"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
	if m.dash.reclaimable != 5<<30 {
		t.Errorf("reclaimable = %d, want %d (UserData must not count)", m.dash.reclaimable, int64(5)<<30)
	}
}

func TestCategoryDrillDownAndBack(t *testing.T) {
	cats := []catalog.Category{{ID: "nm", Label: "node_modules", Tier: catalog.Rebuildable, Kind: catalog.KindDiscover}}
	m := New(cats, nil, 1, "/", false)
	m.width, m.height = 80, 24
	m.dash.resize(80, 24)

	next, _ := m.Update(categoryMsg{index: 0, res: scope.CategoryResult{
		Category: cats[0],
		Matches:  []scope.Match{{Path: "/tmp/a/node_modules", Allocated: 3 << 30}},
	}})
	m = next.(Model)

	next, _ = m.Update(key(tea.KeyEnter))
	m = next.(Model)
	if m.view != viewCategory {
		t.Fatalf("view = %d, want viewCategory", m.view)
	}
	if out := m.render(); !strings.Contains(out, "node_modules") {
		t.Errorf("category view missing the match path\n%s", out)
	}

	next, _ = m.Update(key(tea.KeyEscape))
	m = next.(Model)
	if m.view != viewDashboard {
		t.Errorf("view = %d, want viewDashboard", m.view)
	}
}

func TestMoveClampsToBounds(t *testing.T) {
	cats := []catalog.Category{
		{ID: "a", Label: "A", Tier: catalog.Safe},
		{ID: "b", Label: "B", Tier: catalog.Safe},
	}
	m := New(cats, nil, 1, "/", false)
	m.dash.move(-1)
	if m.dash.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.dash.cursor)
	}
	m.dash.move(10)
	if got := int(m.dash.cursor); got < 0 || got >= len(cats) {
		t.Errorf("cursor = %d out of range", got)
	}
}
