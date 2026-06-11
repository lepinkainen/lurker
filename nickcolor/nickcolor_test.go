package nickcolor

import (
	"regexp"
	"testing"
)

// Pinned against the web client's historical djb2 nickHue (format.ts) so the
// server-side move doesn't shift anyone's colors. Values generated with the
// original JS implementation.
func TestIndexMatchesLegacyWebHash(t *testing.T) {
	tests := []struct {
		nick string
		want int
	}{
		{"shrike", 27},
		{"Shrike", 27}, // case-insensitive
		{"alice", 19},
		{"BOB", 8},
		{"xkr47", 21},
		{"Yön", 2},
		{"日本語", 20},
		{"[away]nick", 44},
		{"a", 22},
		{"", 5},
		{"verylongnicknamewithlotsofchars123", 11},
	}
	for _, tt := range tests {
		if got := Index(tt.nick); got != tt.want {
			t.Errorf("Index(%q) = %d, want %d", tt.nick, got, tt.want)
		}
	}
}

func TestIndexInRange(t *testing.T) {
	for _, nick := range []string{"", "a", "zz", "Ω", "💜nick", "0123456789"} {
		idx := Index(nick)
		if idx < 0 || idx >= Count {
			t.Errorf("Index(%q) = %d, out of [0,%d)", nick, idx, Count)
		}
	}
}

func TestHue(t *testing.T) {
	if got := Hue(0); got != 0 {
		t.Errorf("Hue(0) = %v, want 0", got)
	}
	if got := Hue(1); got != 7.5 {
		t.Errorf("Hue(1) = %v, want 7.5", got)
	}
	if got := Hue(47); got != 352.5 {
		t.Errorf("Hue(47) = %v, want 352.5", got)
	}
}

func TestHexShapeAndDistinctness(t *testing.T) {
	re := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	seen := map[string]int{}
	for i := range Count {
		h := Hex(i)
		if !re.MatchString(h) {
			t.Fatalf("Hex(%d) = %q, not #rrggbb", i, h)
		}
		if prev, dup := seen[h]; dup {
			t.Errorf("Hex(%d) = Hex(%d) = %q, palette collision", i, prev, h)
		}
		seen[h] = i
	}
}

func TestHexKnownValue(t *testing.T) {
	// oklch(0.72 0.12 0deg) is a desaturated rose; sanity-check one
	// conversion against the CSS Color 4 reference algorithm.
	if got := Hex(0); got != "#e183a1" {
		t.Errorf("Hex(0) = %q, want #e183a1 (update if palette L/C change)", got)
	}
}
