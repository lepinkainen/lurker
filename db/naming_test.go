package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestValidateNetworkName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{name: "IRCNet", valid: true},
		{name: "oftc-test", valid: true},
		{name: "under_score", valid: true},
		{name: "name with space", valid: false},
		{name: "dots.not.allowed", valid: false},
		{name: "slash/not-allowed", valid: false},
		{name: "ümlaut", valid: false},
		{name: "", valid: false},
	}

	for _, tc := range tests {
		err := ValidateNetworkName(tc.name)
		if tc.valid && err != nil {
			t.Fatalf("ValidateNetworkName(%q) unexpected error: %v", tc.name, err)
		}
		if !tc.valid && !errors.Is(err, ErrInvalidNetworkName) {
			t.Fatalf("ValidateNetworkName(%q) error = %v, want ErrInvalidNetworkName", tc.name, err)
		}
	}
}

func TestLogDBFilename(t *testing.T) {
	filename, err := LogDBFilename("IRCNet")
	if err != nil {
		t.Fatal(err)
	}
	if filename != "ircnet.db" {
		t.Fatalf("filename = %q, want %q", filename, "ircnet.db")
	}
}

func TestLogDBPath(t *testing.T) {
	path, err := LogDBPath("data", "Libera")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("data", "libera.db")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestUpsertNetworkRejectsInvalidName(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, err = UpsertNetwork(t.Context(), d, Network{
		Name: "bad/name",
		Host: "irc.example.net",
		Port: 6697,
		TLS:  true,
		Nick: "tester",
	})
	if !errors.Is(err, ErrInvalidNetworkName) {
		t.Fatalf("err = %v, want ErrInvalidNetworkName", err)
	}
}

func TestUpsertNetworkCaseInsensitiveDuplicateReturnsSameRow(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	first, err := UpsertNetwork(t.Context(), d, Network{
		Name: "Libera",
		Host: "irc.libera.chat",
		Port: 6697,
		TLS:  true,
		Nick: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := UpsertNetwork(t.Context(), d, Network{
		Name: "LIBERA",
		Host: "ignored.example.net",
		Port: 6667,
		TLS:  false,
		Nick: "other",
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("ids differ: first=%d second=%d", first.ID, second.ID)
	}
}
