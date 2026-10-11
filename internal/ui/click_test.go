package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scan"
	"github.com/rahul-17280/sweep/internal/scope"
)

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestClickExpandsGroup(t *testing.T) {
	cats := newCats()
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m, categoryMsg{index: 0, res: scope.CategoryResult{Category: cats[0], Allocated: 5 << 30}})

	m = feed(t, m, click(5, rowsTop))
	if !m.dash.expanded[catalog.Safe] {
		t.Error("clicking the first group row should expand it")
	}
}

func TestClickTreemapOpensCategory(t *testing.T) {
	cats := newCats()
	m := dashModel(t, cats, Options{Root: "/", Workers: 1})
	m = feed(t, m, categoryMsg{index: 0, res: scope.CategoryResult{Category: cats[0], Allocated: 5 << 30}})

	hx, hy := -1, -1
	for y := 0; y < m.height && hx < 0; y++ {
		for x := 0; x < m.width; x++ {
			if m.dash.treemapHit(x, y) >= 0 {
				hx, hy = x, y
				break
			}
		}
	}
	if hx < 0 {
		t.Skip("treemap not visible at this size")
	}

	m = feed(t, m, click(hx, hy))
	if m.view != viewCategory {
		t.Errorf("clicking a treemap tile should open the category, view = %d", m.view)
	}
}

func TestClickSelectsBrowserRow(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"a.bin": 5000, "b.bin": 1000} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := NewInBrowser(nil, dir, Options{Root: "/"})
	m.width, m.height = 100, 30
	m.tree.resize(100, 30)

	res, err := scan.ScanTree(context.Background(), dir, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.tree.apply(treeMsg{path: dir, tree: res.Tree, sizes: res.Sizes})

	// Rows start at y=5; the second row is b.bin (size sort, desc).
	m = feed(t, m, click(3, 6))
	if got := m.tree.table.Cursor(); got != 1 {
		t.Errorf("cursor = %d, want 1", got)
	}
}
