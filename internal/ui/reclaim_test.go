package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scope"
)

type fakeTrasher struct{ trashed []string }

func (f *fakeTrasher) Trash(paths []string) error {
	f.trashed = append(f.trashed, paths...)
	return nil
}

func (f *fakeTrasher) Remove(paths []string) error {
	f.trashed = append(f.trashed, paths...)
	return nil
}

func reclaimModel(t *testing.T, ft *fakeTrasher) Model {
	t.Helper()
	cats := []catalog.Category{{ID: "npm", Label: "npm cache", Tier: catalog.Safe, Kind: catalog.KindPath}}
	m := New(cats, Options{Root: "/", Workers: 1, AllowDelete: true, Trasher: ft})
	m.width, m.height = 80, 24
	m.dash.resize(80, 24)

	next, _ := m.Update(categoryMsg{index: 0, res: scope.CategoryResult{
		Category:  cats[0],
		Allocated: 2 << 30,
		Matches:   []scope.Match{{Path: "/tmp/cache", Allocated: 2 << 30}},
	}})
	m = next.(Model)
	next, _ = m.Update(key(tea.KeyEnter)) // open the category
	return next.(Model)
}

func TestReclaimMarkConfirmTrash(t *testing.T) {
	ft := &fakeTrasher{}
	m := reclaimModel(t, ft)

	next, _ := m.Update(key(' '))
	m = next.(Model)
	if len(m.marks) != 1 {
		t.Fatalf("marks = %d, want 1", len(m.marks))
	}

	next, _ = m.Update(key('d'))
	m = next.(Model)
	if !m.confirm {
		t.Fatal("expected the confirm modal")
	}
	t.Log("\n" + m.render())

	next, _ = m.Update(key('y'))
	m = next.(Model)

	if len(ft.trashed) != 1 || ft.trashed[0] != "/tmp/cache" {
		t.Fatalf("trashed = %v, want [/tmp/cache]", ft.trashed)
	}
	if len(m.cat.res.Matches) != 0 {
		t.Errorf("matches = %d, want 0 after deletion", len(m.cat.res.Matches))
	}
	if m.dash.reclaimable != 0 {
		t.Errorf("reclaimable = %d, want 0", m.dash.reclaimable)
	}
}

func TestReclaimCancelKeepsMarks(t *testing.T) {
	ft := &fakeTrasher{}
	m := reclaimModel(t, ft)

	next, _ := m.Update(key(' '))
	m = next.(Model)
	next, _ = m.Update(key('d'))
	m = next.(Model)
	next, _ = m.Update(key('n'))
	m = next.(Model)

	if m.confirm {
		t.Error("confirm should be dismissed")
	}
	if len(ft.trashed) != 0 {
		t.Errorf("cancel must not delete, trashed = %v", ft.trashed)
	}
	if len(m.cat.res.Matches) != 1 {
		t.Error("cancel must keep the match")
	}
}

func TestDeleteDisabled(t *testing.T) {
	ft := &fakeTrasher{}
	cats := []catalog.Category{{ID: "npm", Label: "npm cache", Tier: catalog.Safe}}
	m := New(cats, Options{Root: "/", Workers: 1, AllowDelete: false, Trasher: ft})
	m.width, m.height = 80, 24

	next, _ := m.Update(categoryMsg{index: 0, res: scope.CategoryResult{
		Category: cats[0], Allocated: 1 << 30,
		Matches: []scope.Match{{Path: "/tmp/cache", Allocated: 1 << 30}},
	}})
	m = next.(Model)
	next, _ = m.Update(key(tea.KeyEnter))
	m = next.(Model)
	next, _ = m.Update(key(' '))
	m = next.(Model)
	next, _ = m.Update(key('d'))
	m = next.(Model)

	if m.confirm {
		t.Error("confirm must not open when deletion is disabled")
	}
	if len(ft.trashed) != 0 {
		t.Errorf("disabled deletion must not trash, got %v", ft.trashed)
	}
}
