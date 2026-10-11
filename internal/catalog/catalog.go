package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

type Tier uint8

const (
	Safe Tier = iota
	Rebuildable
	Review
	UserData
)

func (t Tier) String() string {
	switch t {
	case Safe:
		return "safe"
	case Rebuildable:
		return "rebuildable"
	case Review:
		return "review"
	case UserData:
		return "userdata"
	}
	return "unknown"
}

// Reclaimable reports whether deleting this tier is routine (counted toward the
// reclaimable total).
func (t Tier) Reclaimable() bool { return t == Safe || t == Rebuildable }

func (t *Tier) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	switch s {
	case "safe":
		*t = Safe
	case "rebuildable":
		*t = Rebuildable
	case "review":
		*t = Review
	case "userdata":
		*t = UserData
	default:
		return fmt.Errorf("unknown tier %q", s)
	}
	return nil
}

type Kind uint8

const (
	KindPath Kind = iota
	KindDiscover
	KindUserFolder
)

func (k Kind) String() string {
	switch k {
	case KindPath:
		return "path"
	case KindDiscover:
		return "discover"
	case KindUserFolder:
		return "userfolder"
	}
	return "unknown"
}

func (k *Kind) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	switch s {
	case "path":
		*k = KindPath
	case "discover":
		*k = KindDiscover
	case "userfolder":
		*k = KindUserFolder
	default:
		return fmt.Errorf("unknown kind %q", s)
	}
	return nil
}

type Category struct {
	ID        string   `yaml:"id" json:"id"`
	Label     string   `yaml:"label" json:"label"`
	Icon      string   `yaml:"icon,omitempty" json:"icon,omitempty"`
	Tier      Tier     `yaml:"tier" json:"tier"`
	Kind      Kind     `yaml:"kind" json:"kind"`
	Paths     []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Roots     []string `yaml:"roots,omitempty" json:"roots,omitempty"`
	Match     []string `yaml:"match,omitempty" json:"match,omitempty"`
	CleanHint string   `yaml:"clean_hint,omitempty" json:"clean_hint,omitempty"`
	Doc       string   `yaml:"doc,omitempty" json:"doc,omitempty"`
	Disabled  bool     `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

// Load returns the built-in catalog merged with the overrides at path. A
// missing file is not an error.
func Load(path string) ([]Category, error) {
	cats := Defaults()
	if path == "" {
		return cats, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cats, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Categories []Category `yaml:"categories"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return Merge(cats, file.Categories), nil
}

// Merge applies overrides onto base by ID: an existing ID is replaced, a new ID
// is appended, and an override with Disabled drops the entry.
func Merge(base, overrides []Category) []Category {
	index := make(map[string]int, len(base))
	out := make([]Category, 0, len(base)+len(overrides))
	for _, c := range base {
		index[c.ID] = len(out)
		out = append(out, c)
	}
	for _, o := range overrides {
		if i, ok := index[o.ID]; ok {
			out[i] = o
		} else {
			index[o.ID] = len(out)
			out = append(out, o)
		}
	}
	kept := out[:0]
	for _, c := range out {
		if !c.Disabled {
			kept = append(kept, c)
		}
	}
	return kept
}
