package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Roots    []string `yaml:"roots"`
	Exclude  []string `yaml:"exclude"`
	Workers  int      `yaml:"workers"`
	SizeMode string   `yaml:"size_mode"`
}

func Dir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".config", "sweep")
}

func CatalogPath() string {
	if d := Dir(); d != "" {
		return filepath.Join(d, "catalog.yaml")
	}
	return ""
}

// Load reads config.yaml, falling back to defaults for any unset field. A
// missing file is not an error.
func Load() (Config, error) {
	c := Config{Workers: 4, Roots: DefaultRoots()}
	dir := Dir()
	if dir == "" {
		return c, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if len(c.Roots) == 0 {
		c.Roots = DefaultRoots()
	}
	if c.Workers <= 0 {
		c.Workers = 4
	}
	return c, nil
}

func DefaultRoots() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(h, "Projects"),
		filepath.Join(h, "code"),
		filepath.Join(h, "src"),
		filepath.Join(h, "Developer"),
		filepath.Join(h, "Documents"),
	}
}
