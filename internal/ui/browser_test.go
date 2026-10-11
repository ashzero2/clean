package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rahul-17280/sweep/internal/scan"
)

func scanBrowser(t *testing.T, dir string) browser {
	t.Helper()
	res, err := scan.ScanTree(context.Background(), dir, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := newBrowser(dir, 80, 24, false)
	b.apply(treeMsg{path: dir, tree: res.Tree, sizes: res.Sizes})
	return b
}

func childIndex(t *testing.T, b browser, name string) int {
	t.Helper()
	for i, k := range b.children() {
		if b.tr.Node(k).Name == name {
			return i
		}
	}
	t.Fatalf("child %q not found", name)
	return -1
}

func TestBrowserNavigate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.bin"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	b := scanBrowser(t, dir)
	if got := len(b.children()); got != 2 {
		t.Fatalf("children = %d, want 2", got)
	}
	// Size sort is the default, so a.bin (5000) precedes sub/.
	if b.tr.Node(b.children()[0]).Name != "a.bin" {
		t.Errorf("first child = %q, want a.bin", b.tr.Node(b.children()[0]).Name)
	}

	b.down() // descending into a file is a no-op
	if len(b.stack) != 1 {
		t.Errorf("descending into a file changed depth to %d", len(b.stack))
	}

	b.cursor = childIndex(t, b, "sub")
	b.down()
	if len(b.stack) != 2 {
		t.Fatalf("depth = %d, want 2 after descending into sub", len(b.stack))
	}
	if !strings.HasSuffix(b.breadcrumb(), "/sub") {
		t.Errorf("breadcrumb = %q, want suffix /sub", b.breadcrumb())
	}
	if !b.up() || len(b.stack) != 1 {
		t.Error("up() should return to depth 1")
	}
	if b.up() {
		t.Error("up() at root should report false")
	}
}

func TestBrowserColumns(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "x.bin"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	b := scanBrowser(t, dir)
	if got := len(b.columns()); got != 1 {
		t.Fatalf("columns = %d, want 1 at the root", got)
	}
	b.cursor = childIndex(t, b, "sub")
	b.down()
	if got := len(b.columns()); got != 2 {
		t.Fatalf("columns = %d, want 2 after descending", got)
	}
	b.width, b.height = 110, 20
	t.Log("\n" + b.render())
}

func TestBrowserTopFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.bin"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, 50000), 0o644); err != nil {
		t.Fatal(err)
	}

	b := scanBrowser(t, dir)
	if len(b.top) == 0 {
		t.Fatal("expected the biggest-files panel to be populated")
	}
	if got := b.tr.Node(b.top[0]).Name; got != "big.bin" {
		t.Errorf("largest file = %q, want big.bin", got)
	}
}

func TestBrowserSortByMtime(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.bin")
	newer := filepath.Join(dir, "new.bin")
	for _, p := range []string{old, newer} {
		if err := os.WriteFile(p, make([]byte, 1000), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	b := scanBrowser(t, dir)
	b.setSort(sortMtime) // first press: newest first
	if got := b.tr.Node(b.children()[0]).Name; got != "new.bin" {
		t.Errorf("mtime desc first = %q, want new.bin", got)
	}
	b.setSort(sortMtime) // toggle to ascending
	if got := b.tr.Node(b.children()[0]).Name; got != "old.bin" {
		t.Errorf("mtime asc first = %q, want old.bin", got)
	}
}

func TestNewInBrowserStartsThere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewInBrowser(nil, dir, Options{Root: "/"})
	if m.view != viewBrowser {
		t.Errorf("view = %d, want viewBrowser", m.view)
	}
	if m.tree.root != dir {
		t.Errorf("root = %q, want %q", m.tree.root, dir)
	}
}
