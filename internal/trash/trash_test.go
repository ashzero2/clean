//go:build darwin

package trash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrashMovesIntoTempHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	src := filepath.Join(t.TempDir(), "thing")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New().Trash([]string{src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still present after trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash", "thing")); err != nil {
		t.Errorf("not moved into trash: %v", err)
	}
}

func TestTrashCollisionSuffixed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first := filepath.Join(t.TempDir(), "dup")
	src := filepath.Join(t.TempDir(), "dup")
	if err := os.WriteFile(first, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := New()
	if err := tr.Trash([]string{first}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Trash([]string{src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash", "dup 2")); err != nil {
		t.Errorf("collision not suffixed: %v", err)
	}
}
