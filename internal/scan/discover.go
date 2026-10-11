package scan

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"

	"github.com/charlievieth/fastwalk"
)

var vcsDirs = map[string]bool{".git": true, ".hg": true, ".svn": true, ".bzr": true}

// Discover returns directories under root whose base name matches, without
// descending into a match. Matches are returned sorted for deterministic output.
func Discover(ctx context.Context, root string, match func(string) bool, opts Options) ([]string, error) {
	rootClean := filepath.Clean(root)

	ch := make(chan string, 1024)
	var wg sync.WaitGroup
	wg.Add(1)
	var matches []string
	go func() {
		defer wg.Done()
		for p := range ch {
			matches = append(matches, p)
		}
	}()

	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if walkErr != nil || !d.IsDir() || path == rootClean {
			return nil
		}
		name := d.Name()
		if vcsDirs[name] || excluded(opts.Exclude, name, path) {
			return fs.SkipDir
		}
		if match(name) {
			select {
			case ch <- path:
			case <-ctx.Done():
				return fs.SkipAll
			}
			return fs.SkipDir
		}
		return nil
	}

	cfg := fastwalk.Config{Follow: false}
	walkErr := fastwalk.Walk(&cfg, rootClean, walkFn)
	close(ch)
	wg.Wait()

	slices.Sort(matches)
	if cerr := ctx.Err(); cerr != nil {
		return matches, cerr
	}
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return matches, walkErr
	}
	return matches, nil
}
