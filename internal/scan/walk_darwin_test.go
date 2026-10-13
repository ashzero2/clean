//go:build darwin

package scan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// bulkFixture builds a tree exercising the cases that can break the bulk
// enumerator: files, nested dirs, a hard-linked pair, a symlink and a sparse file.
func bulkFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), 1000)
	writeFile(t, filepath.Join(dir, "sub", "b.bin"), 5000)
	writeFile(t, filepath.Join(dir, "sub", "deep", "c.bin"), 20000)
	if err := os.Link(filepath.Join(dir, "a.txt"), filepath.Join(dir, "a.hard")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "sparse.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(1<<20, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return dir
}

// TestBulkMatchesFastwalk is the both-enumerators-agree check: the
// getattrlistbulk walker and fastwalk must report identical totals.
func TestBulkMatchesFastwalk(t *testing.T) {
	dir := bulkFixture(t)

	bulk, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := runWalk(context.Background(), dir, Options{}, newMeasurer(), driver{events: fastwalkWalk})
	if err != nil {
		t.Fatal(err)
	}
	if bulk.Allocated != fast.Allocated || bulk.Apparent != fast.Apparent ||
		bulk.Entries != fast.Entries || bulk.Dirs != fast.Dirs {
		t.Errorf("bulk = %+v\nfastwalk = %+v", bulk, fast)
	}
}

// TestBulkMatchesDuResourceFork guards the ATTR_FILE_ALLOCSIZE trap: an ordinary
// file reports only its data fork through stat(2)'s st_size, but st_blocks and
// ALLOCSIZE cover all forks. A resource fork is where a wrong attribute choice
// silently diverges from du.
func TestBulkMatchesDuResourceFork(t *testing.T) {
	duPath, err := exec.LookPath("du")
	if err != nil {
		t.Skip("du not available")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "rsrc.bin")
	writeFile(t, f, 4)
	if err := unix.Setxattr(f, "com.apple.ResourceFork", make([]byte, 8192), 0); err != nil {
		t.Skipf("cannot set resource fork: %v", err)
	}

	got, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(duPath, "-sk", dir).Output()
	if err != nil {
		t.Fatalf("du: %v", err)
	}
	var kib int64
	if _, err := fmt.Sscanf(strings.Fields(string(out))[0], "%d", &kib); err != nil {
		t.Fatalf("parse du %q: %v", out, err)
	}
	if want := kib * 1024; got.Allocated != want {
		t.Errorf("allocated = %d, du = %d (resource fork not accounted like du)", got.Allocated, want)
	}
}

// TestBulkRefillsBuffer drives more entries through one directory than a single
// 256 KiB attribute buffer holds, forcing getattrlistbulk to be called again.
func TestBulkRefillsBuffer(t *testing.T) {
	if testing.Short() {
		t.Skip("creates many files")
	}
	dir := t.TempDir()
	const n = 5000
	for i := 0; i < n; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%05d", i)), 10)
	}

	s, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries != n+1 { // the files plus the root
		t.Errorf("Entries = %d, want %d", s.Entries, n+1)
	}
	if s.Dirs != 1 {
		t.Errorf("Dirs = %d, want 1", s.Dirs)
	}
	if s.Errors != 0 {
		t.Errorf("Errors = %d, want 0", s.Errors)
	}
}
