package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rahul-17280/sweep/internal/scan"
)

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

	res, err := scan.ScanTree(context.Background(), dir, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}

	b := newBrowser(dir, 80, 24, false)
	b.apply(treeMsg{path: dir, tree: res.Tree})

	rows := b.table.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (%v)", len(rows), rows)
	}
	// Children sort by size desc, so a.bin (5000) precedes sub/.
	if rows[0][0] != "a.bin" {
		t.Errorf("first row = %q, want a.bin", rows[0][0])
	}

	// Selecting a file and descending must be a no-op.
	b.down()
	if len(b.stack) != 1 {
		t.Errorf("descending into a file changed depth to %d", len(b.stack))
	}

	// Select the directory row and descend.
	idx := -1
	for i, r := range b.table.Rows() {
		if r[0] == "sub/" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("sub/ row not found")
	}
	b.table.SetCursor(idx)
	b.down()
	if len(b.stack) != 2 {
		t.Fatalf("depth = %d, want 2 after descending into sub", len(b.stack))
	}
	if !strings.HasSuffix(b.breadcrumb(), "/sub") {
		t.Errorf("breadcrumb = %q, want suffix /sub", b.breadcrumb())
	}
	if !b.up() || len(b.stack) != 1 {
		t.Errorf("up() should return to depth 1")
	}
	if b.up() {
		t.Errorf("up() at root should report false")
	}
}
