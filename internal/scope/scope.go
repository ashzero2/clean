package scope

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rahul-17280/sweep/internal/catalog"
	"github.com/rahul-17280/sweep/internal/scan"
)

type Match struct {
	Path      string
	Allocated int64
	Items     int64
}

type CategoryResult struct {
	Category  catalog.Category
	Allocated int64
	Apparent  int64
	Items     int64
	Matches   []Match
	Err       error
}

// MeasureCategory resolves and measures one category. Discover categories scan
// the given project roots; path and user-folder categories measure their own
// paths. Missing paths are skipped rather than treated as errors.
func MeasureCategory(ctx context.Context, c catalog.Category, projectRoots []string, opts scan.Options) CategoryResult {
	res := CategoryResult{Category: c}
	if c.Kind == catalog.KindDiscover {
		roots := c.Roots
		if len(roots) == 0 {
			roots = projectRoots
		}
		match := matcher(c.Match)
		for _, root := range roots {
			root = Expand(root)
			if !dirExists(root) {
				continue
			}
			found, err := scan.Discover(ctx, root, match, opts)
			if err != nil {
				res.Err = err
				return res
			}
			for _, dir := range found {
				res.add(ctx, dir, opts)
			}
		}
		return res
	}
	for _, p := range c.Paths {
		p = Expand(p)
		if !exists(p) {
			continue
		}
		res.add(ctx, p, opts)
	}
	return res
}

func (r *CategoryResult) add(ctx context.Context, path string, opts scan.Options) {
	s, err := scan.Measure(ctx, path, opts)
	if err != nil {
		return
	}
	r.Allocated += s.Allocated
	r.Apparent += s.Apparent
	r.Items += s.Entries
	r.Matches = append(r.Matches, Match{Path: path, Allocated: s.Allocated, Items: s.Entries})
}

// MeasureAllStream measures categories with a bounded worker pool, calling emit
// as each finishes. emit may be called from several goroutines.
func MeasureAllStream(ctx context.Context, cats []catalog.Category, projectRoots []string, opts scan.Options, workers int, emit func(int, CategoryResult)) {
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range cats {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			emit(i, MeasureCategory(ctx, cats[i], projectRoots, opts))
		}(i)
	}
	wg.Wait()
}

// MeasureAll measures categories with a bounded worker pool. Each goroutine
// writes to a distinct index, so no lock is needed.
func MeasureAll(ctx context.Context, cats []catalog.Category, projectRoots []string, opts scan.Options, workers int) []CategoryResult {
	results := make([]CategoryResult, len(cats))
	MeasureAllStream(ctx, cats, projectRoots, opts, workers, func(i int, r CategoryResult) {
		results[i] = r
	})
	return results
}

// Expand resolves a leading ~ and ${VAR} references in a path.
func Expand(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func matcher(patterns []string) func(string) bool {
	return func(name string) bool {
		for _, p := range patterns {
			if ok, _ := filepath.Match(p, name); ok {
				return true
			}
		}
		return false
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
