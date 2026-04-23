package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	l := &Loader{Dir: dir}
	themes, err := l.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(themes) != 0 {
		t.Errorf("empty dir should yield 0 themes, got %d", len(themes))
	}
}

func TestLoadMissingDir(t *testing.T) {
	l := &Loader{Dir: "/nonexistent/path/definitely/not/here"}
	themes, err := l.Load()
	if err != nil {
		t.Fatalf("missing dir should not error, got: %v", err)
	}
	if len(themes) != 0 {
		t.Errorf("missing dir should yield 0 themes, got %d", len(themes))
	}
}

func TestLoadEmptyDirField(t *testing.T) {
	l := &Loader{Dir: ""}
	themes, err := l.Load()
	if err != nil {
		t.Fatalf("empty Dir should not error, got: %v", err)
	}
	if len(themes) != 0 {
		t.Errorf("empty Dir should yield 0 themes, got %d", len(themes))
	}
}

func TestLoadUserDir(t *testing.T) {
	dir := t.TempDir()
	valid := `name: Custom
colors:
  bg-0: "#000000"
  fg-0: "#ffffff"
nicks:
  saturation: 50
  lightness: 60
`
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("not: valid: yaml:"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{Dir: dir}
	themes, err := l.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(themes) != 1 {
		t.Fatalf("want 1 theme (broken skipped), got %d", len(themes))
	}
	got := themes[0]
	if got.ID != "custom" {
		t.Errorf("id: want custom got %q", got.ID)
	}
	if got.Name != "Custom" {
		t.Errorf("name: want Custom got %q", got.Name)
	}
	if got.Colors["bg-0"] != "#000000" {
		t.Errorf("color bg-0: want #000000 got %q", got.Colors["bg-0"])
	}
	if got.Nicks.Saturation != 50 || got.Nicks.Lightness != 60 {
		t.Errorf("nicks: got %+v", got.Nicks)
	}
}

func TestLoadSort(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct{ name, body string }{
		{"zebra.yaml", "name: Zebra\n"},
		{"apple.yaml", "name: Apple\n"},
		{"mango.yaml", "name: mango\n"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	themes, err := (&Loader{Dir: dir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{themes[0].Name, themes[1].Name, themes[2].Name}
	want := []string{"Apple", "mango", "Zebra"}
	for i := range names {
		if names[i] != want[i] {
			t.Errorf("sort[%d]: got %q want %q", i, names[i], want[i])
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Tokyo Night":     "tokyo-night",
		"solarized_light": "solarized-light",
		"Foo 123!":        "foo-123",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
