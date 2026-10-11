package scope

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scan"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureDiscover(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "proj", "app", "node_modules", "pkg.bin"), 20000)
	write(t, filepath.Join(base, "proj", "other", "src.bin"), 100)

	c := catalog.Category{ID: "nm", Kind: catalog.KindDiscover, Match: []string{"node_modules"}}
	res := MeasureCategory(context.Background(), c, []string{base}, scan.Options{})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(res.Matches))
	}
	if res.Allocated < 20000 {
		t.Errorf("allocated = %d, want >= 20000", res.Allocated)
	}
	if res.Allocated > 20000+4096+200 {
		t.Errorf("allocated = %d suggests non-matching files were counted", res.Allocated)
	}
}

func TestMeasurePathSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cache", "x.bin"), 5000)

	c := catalog.Category{ID: "c", Kind: catalog.KindPath, Paths: []string{
		filepath.Join(dir, "cache"),
		filepath.Join(dir, "does-not-exist"),
	}}
	res := MeasureCategory(context.Background(), c, nil, scan.Options{})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("matches = %d, want 1 (missing path skipped)", len(res.Matches))
	}
	if res.Allocated < 5000 {
		t.Errorf("allocated = %d, want >= 5000", res.Allocated)
	}
}

func TestMeasureAllConcurrent(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < 3; i++ {
		write(t, filepath.Join(base, fmt.Sprintf("p%d", i), "node_modules", "f.bin"), 4096)
	}

	cats := []catalog.Category{
		{ID: "a", Kind: catalog.KindDiscover, Match: []string{"node_modules"}},
		{ID: "b", Kind: catalog.KindDiscover, Match: []string{"node_modules"}},
	}
	results := MeasureAll(context.Background(), cats, []string{base}, scan.Options{}, 2)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, r := range results {
		if len(r.Matches) != 3 {
			t.Errorf("category %s matches = %d, want 3", r.Category.ID, len(r.Matches))
		}
	}
}

func TestExpand(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := Expand("~/Library/Caches"); got != filepath.Join(home, "Library", "Caches") {
		t.Errorf("Expand(~/) = %q", got)
	}
	if got := Expand("/absolute/path"); got != "/absolute/path" {
		t.Errorf("Expand(absolute) = %q", got)
	}
}
