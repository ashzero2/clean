package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
)

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func newCats() []catalog.Category {
	return []catalog.Category{
		{ID: "npm", Label: "npm cache", Tier: catalog.Safe, Kind: catalog.KindPath},
		{ID: "docs", Label: "Documents", Tier: catalog.UserData, Kind: catalog.KindUserFolder},
	}
}

func dashModel(t *testing.T, cats []catalog.Category, opts Options) Model {
	t.Helper()
	m := New(cats, opts)
	m.width, m.height = 100, 30
	m.dash.resize(100, 30)
	return m
}

func feed(t *testing.T, m Model, msgs ...any) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

// openFirstCategory expands the first group and opens its largest category.
func openFirstCategory(t *testing.T, m Model) Model {
	t.Helper()
	m = feed(t, m, key(tea.KeyEnter), key('j'), key(tea.KeyEnter))
	return m
}

func TestDashboardShowsGroupsAndReclaimable(t *testing.T) {
	cats := newCats()
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m,
		categoryMsg{index: 0, res: scope.CategoryResult{Category: cats[0], Allocated: 5 << 30}},
		categoryMsg{index: 1, res: scope.CategoryResult{Category: cats[1], Allocated: 1 << 30}},
	)

	out := m.render()
	t.Log("\n" + out)
	for _, want := range []string{"sweep", "Potentially reclaimable", "Safe caches", "5.0 GiB", "User folders"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
	// Collapsed groups must not leak their categories.
	if strings.Contains(out, "npm cache") {
		t.Errorf("category should be hidden while collapsed\n%s", out)
	}
	if m.dash.reclaimable != 5<<30 {
		t.Errorf("reclaimable = %d, want %d (UserData must not count)", m.dash.reclaimable, int64(5)<<30)
	}
}

func TestDashboardExpandGroup(t *testing.T) {
	cats := newCats()
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m, categoryMsg{index: 0, res: scope.CategoryResult{Category: cats[0], Allocated: 5 << 30}})

	m = feed(t, m, key(tea.KeyEnter)) // expand the first (Safe) group
	if out := m.render(); !strings.Contains(out, "npm cache") {
		t.Errorf("expected npm cache after expanding\n%s", out)
	}
}

func TestCategoryDrillDownAndBack(t *testing.T) {
	cats := []catalog.Category{{ID: "nm", Label: "node_modules", Tier: catalog.Rebuildable, Kind: catalog.KindDiscover}}
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m, categoryMsg{index: 0, res: scope.CategoryResult{
		Category: cats[0],
		Matches:  []scope.Match{{Path: "/tmp/a/node_modules", Allocated: 3 << 30}},
	}})

	m = openFirstCategory(t, m)
	if m.view != viewCategory {
		t.Fatalf("view = %d, want viewCategory", m.view)
	}
	if out := m.render(); !strings.Contains(out, "node_modules") {
		t.Errorf("category view missing the match path\n%s", out)
	}

	m = feed(t, m, key(tea.KeyEscape))
	if m.view != viewDashboard {
		t.Errorf("view = %d, want viewDashboard", m.view)
	}
}

func TestMoveClampsToBounds(t *testing.T) {
	cats := newCats()
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m.dash.move(-1)
	if m.dash.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.dash.cursor)
	}
	m.dash.move(50)
	if got := m.dash.cursor; got < 0 || got >= len(m.dash.rows()) {
		t.Errorf("cursor = %d out of range [0,%d)", got, len(m.dash.rows()))
	}
}

func TestDashboardScanHomeOpensBrowser(t *testing.T) {
	m := dashModel(t, newCats(), Options{Root: "/", Workers: 1})
	m = feed(t, m, key('b'))
	if m.view != viewBrowser {
		t.Fatalf("view = %d, want viewBrowser", m.view)
	}
	if m.tree.root == "" {
		t.Error("browser root should be set to the home directory")
	}
	if out := m.render(); !strings.Contains(out, "Scanning") {
		t.Errorf("expected a loading state right after starting a scan\n%s", out)
	}
}
