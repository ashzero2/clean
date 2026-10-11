package ui

import (
	"strings"
	"testing"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
)

func TestRenderShowsSizesAndReclaimable(t *testing.T) {
	cats := []catalog.Category{
		{ID: "npm", Label: "npm cache", Tier: catalog.Safe, Kind: catalog.KindPath},
		{ID: "docs", Label: "Documents", Tier: catalog.UserData, Kind: catalog.KindUserFolder},
	}
	m := New(cats, nil, 1, "/", false)
	m.width, m.height = 80, 24

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
	if m.reclaimable != 5<<30 {
		t.Errorf("reclaimable = %d, want %d (UserData must not count)", m.reclaimable, int64(5)<<30)
	}
}

func TestMoveClampsToBounds(t *testing.T) {
	cats := []catalog.Category{
		{ID: "a", Label: "A", Tier: catalog.Safe},
		{ID: "b", Label: "B", Tier: catalog.Safe},
	}
	m := New(cats, nil, 1, "/", false)
	m.move(-1)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	m.move(10)
	if got := int(m.cursor); got < 0 || got >= len(cats) {
		t.Errorf("cursor = %d out of range", got)
	}
}
