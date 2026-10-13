package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rahul-17280/sweep/internal/tree"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureCounts(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "top.txt"), 1000)
	writeFile(t, filepath.Join(dir, "sub", "deep", "x.bin"), 5000)

	s, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries != 5 { // dir, sub, deep, top.txt, x.bin
		t.Errorf("Entries = %d, want 5", s.Entries)
	}
	if s.Dirs != 3 { // dir, sub, deep
		t.Errorf("Dirs = %d, want 3", s.Dirs)
	}
	if s.Errors != 0 {
		t.Errorf("Errors = %d, want 0", s.Errors)
	}
	if s.Allocated < 6000 {
		t.Errorf("Allocated = %d, want >= 6000", s.Allocated)
	}
}

func TestMeasureHardlinkDedup(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.bin")
	writeFile(t, f, 1<<20)
	if err := os.Link(f, filepath.Join(dir, "b.bin")); err != nil {
		t.Fatalf("os.Link: %v", err)
	}

	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	_, fileAlloc, nlink := statSizes(fi)
	if nlink != 2 {
		t.Fatalf("nlink = %d, want 2 (test requires a real hard link)", nlink)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, dirAlloc, _ := statSizes(di)

	s, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := fileAlloc + dirAlloc
	if s.Allocated != want {
		t.Errorf("Allocated = %d, want %d (hard-linked blocks counted once)", s.Allocated, want)
	}
	if s.Entries != 3 { // dir + two names for one inode
		t.Errorf("Entries = %d, want 3", s.Entries)
	}
}

func TestScanTreeStructure(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "top.txt"), 1000)
	writeFile(t, filepath.Join(dir, "sub", "deep", "x.bin"), 5000)

	res, err := ScanTree(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Tree
	if bad := tr.CheckInvariant(); bad != -1 {
		t.Fatalf("parent/child index invariant violated at node %d", bad)
	}
	root := tr.Node(tr.Root)
	if !root.IsDir() {
		t.Fatal("root is not a directory")
	}
	if root.Name != dir {
		t.Errorf("root name = %q, want %q", root.Name, dir)
	}

	kids := tr.Children(tr.Root)
	if len(kids) != 2 {
		t.Fatalf("root has %d children, want 2", len(kids))
	}
	sub := findChild(tr, tr.Root, "sub")
	if sub == tr.Root {
		t.Fatal("child 'sub' not found")
	}
	if findChild(tr, sub, "deep") == sub {
		t.Fatal("grandchild 'deep' not found")
	}
	// Root's rolled-up size must cover both files at minimum.
	if root.Size < 6000 {
		t.Errorf("root.Size = %d, want >= 6000", root.Size)
	}
}

func findChild(tr *tree.Tree, parent int32, name string) int32 {
	for _, c := range tr.Children(parent) {
		if tr.Node(c).Name == name {
			return c
		}
	}
	return parent
}

func TestScanCancelReturnsPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	// Point at a real, non-trivial tree so the walk takes measurable time.
	root := os.Getenv("HOME")
	if root == "" {
		t.Skip("no HOME")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	s, err := Measure(ctx, root, Options{})
	if err == nil {
		t.Fatal("expected a context error")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("err = %v, want context cancellation", err)
	}
	// Partial counters are still returned for inspection.
	_ = s
}

// TestMeasureMatchesDu cross-checks our allocated total against du, which uses
// the same st_blocks accounting and also counts hard links once.
func TestMeasureMatchesDu(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	duPath, err := exec.LookPath("du")
	if err != nil {
		t.Skip("du not available")
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a", "one.bin"), 300_000)
	writeFile(t, filepath.Join(dir, "a", "b", "two.bin"), 700_000)
	writeFile(t, filepath.Join(dir, "three.bin"), 1_500_000)
	if err := os.Link(filepath.Join(dir, "three.bin"), filepath.Join(dir, "three.link")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Measure(ctx, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(duPath, "-sk", dir).Output()
	if err != nil {
		t.Fatalf("du: %v", err)
	}
	var kib int64
	if _, err := fmt.Sscanf(strings.Fields(string(out))[0], "%d", &kib); err != nil {
		t.Fatalf("parse du output %q: %v", out, err)
	}
	duBytes := kib * 1024

	diff := got.Allocated - duBytes
	if diff < 0 {
		diff = -diff
	}
	tol := duBytes/20 + 8192 // 5% plus a small floor
	if diff > tol {
		t.Errorf("allocated = %d, du = %d (diff %d > tol %d)", got.Allocated, duBytes, diff, tol)
	}
}

// TestStallWatchdogAborts blocks the walk (simulating a directory wedged in
// open(2)) and asserts the scan gives up with partial results instead of
// hanging for the kernel.
func TestStallWatchdogAborts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bin"), 1000)

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	walk := func(root string, opts Options, fn fs.WalkDirFunc) error {
		close(started)
		<-release // never returns until the test releases it
		return nil
	}

	s, err := runWalk(context.Background(), dir, Options{StallTimeout: 150 * time.Millisecond}, newMeasurer(), driver{events: walk})
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if !s.Stalled {
		t.Errorf("Stalled = false, want true")
	}
	select {
	case <-started:
	default:
		t.Fatal("walk was never started")
	}
}

// TestStallWatchdogDisabled keeps the walk blocked but sets a negative timeout,
// so the scan must wait for the walk rather than abort it.
func TestStallWatchdogDisabled(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bin"), 1000)

	done := make(chan struct{})
	walk := func(root string, opts Options, fn fs.WalkDirFunc) error {
		<-done
		return nil
	}

	result := make(chan error, 1)
	go func() {
		_, err := runWalk(context.Background(), dir, Options{StallTimeout: -1}, newMeasurer(), driver{events: walk})
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("scan returned %v with the watchdog disabled", err)
	case <-time.After(300 * time.Millisecond):
		// Still waiting on the walk, as it should be.
	}
	close(done)
	if err := <-result; err != nil {
		t.Fatalf("err = %v, want nil once the walk finishes", err)
	}
}

// TestErrorPathsRecorded checks an unreadable directory is counted and its path
// kept, while its readable siblings are still measured.
func TestErrorPathsRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 does not block root")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ok.bin"), 4000)
	lock := filepath.Join(dir, "locked")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lock, "hidden.bin"), 8000)
	if err := os.Chmod(lock, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lock, 0o755) })

	s, err := Measure(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors == 0 {
		t.Fatal("Errors = 0, want >= 1 for the unreadable directory")
	}
	if !slicesContains(s.ErrorPaths, lock) {
		t.Errorf("ErrorPaths = %v, want it to include %q", s.ErrorPaths, lock)
	}
	if s.Entries < 2 { // dir + ok.bin; the locked dir may or may not be listed
		t.Errorf("Entries = %d, want the readable sibling counted", s.Entries)
	}
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
