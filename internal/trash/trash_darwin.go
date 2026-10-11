//go:build darwin

package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func New() Trasher { return darwinTrasher{} }

type darwinTrasher struct{}

// Trash moves each path into ~/.Trash, suffixing collisions. A plain rename
// keeps this fast but does not register Finder's "Put Back" metadata.
func (darwinTrasher) Trash(paths []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, p := range paths {
		dst := uniqueDest(filepath.Join(dir, filepath.Base(p)))
		if err := os.Rename(p, dst); err != nil {
			return fmt.Errorf("trashing %s: %w", p, err)
		}
	}
	return nil
}

func (darwinTrasher) Remove(paths []string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	return nil
}

func uniqueDest(dst string) string {
	if !exists(dst) {
		return dst
	}
	ext := filepath.Ext(dst)
	base := strings.TrimSuffix(dst, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s %d%s", base, i, ext)
		if !exists(candidate) {
			return candidate
		}
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
