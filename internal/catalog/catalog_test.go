package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMerge(t *testing.T) {
	base := []Category{{ID: "a", Label: "A", Tier: Safe}, {ID: "b", Label: "B", Tier: Review}}
	got := Merge(base, []Category{
		{ID: "b", Label: "B2", Tier: Safe},
		{ID: "c", Label: "C", Tier: Safe},
		{ID: "a", Disabled: true},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%v)", len(got), got)
	}
	if got[0].ID != "b" || got[0].Label != "B2" || got[0].Tier != Safe {
		t.Errorf("override not applied: %+v", got[0])
	}
	if got[1].ID != "c" {
		t.Errorf("new entry not appended: %+v", got[1])
	}
}

func TestLoadOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	content := "categories:\n" +
		"  - id: npm\n    label: npm cache\n    tier: review\n    kind: path\n    paths: [\"~/.npm\"]\n" +
		"  - id: custom\n    label: Custom\n    tier: safe\n    kind: path\n    paths: [\"/tmp/x\"]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cats, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var npm, custom *Category
	for i := range cats {
		switch cats[i].ID {
		case "npm":
			npm = &cats[i]
		case "custom":
			custom = &cats[i]
		}
	}
	if npm == nil || npm.Tier != Review {
		t.Errorf("npm override not applied: %+v", npm)
	}
	if custom == nil {
		t.Error("custom entry not appended")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cats, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) == 0 {
		t.Fatal("expected built-in defaults")
	}
}
