// Package theme loads UI color/font themes from YAML files in a
// configurable directory. Loader re-reads the filesystem on every
// Load() call so operators can drop a new YAML in and see it appear
// without restarting the server. If the directory is missing or empty,
// Load returns an empty slice and the frontend falls back to the CSS
// :root defaults baked into the web bundle.
package theme

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Nicks holds the HSL parameters applied to hash-derived nick colors.
type Nicks struct {
	Saturation float64 `yaml:"saturation" json:"saturation"`
	Lightness  float64 `yaml:"lightness" json:"lightness"`
	HueOffset  float64 `yaml:"hue_offset" json:"hue_offset"`
}

// Fonts lists CSS font-family stacks.
type Fonts struct {
	Sans string `yaml:"sans" json:"sans"`
	Mono string `yaml:"mono" json:"mono"`
}

// Theme is one theme document.
type Theme struct {
	ID     string            `yaml:"-" json:"id"`
	Name   string            `yaml:"name" json:"name"`
	Colors map[string]string `yaml:"colors" json:"colors"`
	Fonts  Fonts             `yaml:"fonts" json:"fonts"`
	Nicks  Nicks             `yaml:"nicks" json:"nicks"`
}

// Loader reads themes from a directory. Dir may be empty, missing, or
// absent entirely — all resolve to an empty theme list.
type Loader struct {
	Dir string
}

// Load returns themes sorted by name (case-insensitive). A single
// malformed file is logged and skipped; other files still load.
func (l *Loader) Load() ([]Theme, error) {
	if l.Dir == "" {
		return nil, nil
	}
	themes, err := loadDir(l.Dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(themes, func(i, j int) bool {
		return strings.ToLower(themes[i].Name) < strings.ToLower(themes[j].Name)
	})
	return themes, nil
}

func loadDir(dir string) ([]Theme, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read themes dir %s: %w", dir, err)
	}
	var out []Theme
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			slog.Warn("theme: read failed", "file", name, "err", err)
			continue
		}
		t, err := parseTheme(data, name)
		if err != nil {
			slog.Warn("theme: parse failed", "file", name, "err", err)
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func parseTheme(data []byte, filename string) (Theme, error) {
	var t Theme
	if err := yaml.Unmarshal(data, &t); err != nil {
		return Theme{}, fmt.Errorf("parse %s: %w", filename, err)
	}
	id := strings.TrimSuffix(filename, filepath.Ext(filename))
	t.ID = slug(id)
	if t.Name == "" {
		t.Name = t.ID
	}
	if t.Colors == nil {
		t.Colors = map[string]string{}
	}
	return t, nil
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	return b.String()
}
