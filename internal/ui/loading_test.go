package ui

import (
	"strings"
	"testing"

	"github.com/rahul-17280/sweep/internal/scan"
)

func TestBrowserShowsLoadingAndProgress(t *testing.T) {
	m := NewInBrowser(nil, "/Users/example", Options{Root: "/"})
	m.width, m.height = 100, 30
	m.tree.resize(100, 30)

	if out := m.render(); !strings.Contains(out, "Scanning") {
		t.Fatalf("loading state missing:\n%s", out)
	} else {
		t.Log("\n" + out)
	}

	m = feed(t, m, progressMsg(scan.Progress{Entries: 12345, Bytes: 1 << 30}))
	if out := m.render(); !strings.Contains(out, "12,345") {
		t.Errorf("progress counts not rendered:\n%s", out)
	}

	// The tree message clears the loading state.
	m = feed(t, m, treeMsg{path: "/Users/example"})
	if out := m.render(); strings.Contains(out, "Scanning") {
		t.Errorf("loading state should be cleared:\n%s", out)
	}
}
