package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestFirstPinnedChannelOrdersByPinOrderThenName(t *testing.T) {
	net := uuid.New()
	networks := []networkDTO{{ID: net, SortOrder: 0}}
	// "zeta" has the lower PinOrder so it should win despite sorting after
	// "alpha" alphabetically; PinOrder takes priority over name.
	zeta := bufferDTO{ID: uuid.New(), NetworkID: net, Kind: "channel", Pinned: true, Name: "zeta", PinOrder: 1}
	alpha := bufferDTO{ID: uuid.New(), NetworkID: net, Kind: "channel", Pinned: true, Name: "alpha", PinOrder: 2}
	buffers := []bufferDTO{alpha, zeta}

	got := pickStartupBuffer(networks, buffers, uuid.Nil)
	if got != zeta.ID {
		t.Fatalf("pickStartupBuffer() = %v, want zeta (%v) by lower PinOrder", got, zeta.ID)
	}
}

func TestFirstPinnedChannelFallsBackToNameWhenPinOrderTies(t *testing.T) {
	net := uuid.New()
	networks := []networkDTO{{ID: net, SortOrder: 0}}
	alpha := bufferDTO{ID: uuid.New(), NetworkID: net, Kind: "channel", Pinned: true, Name: "alpha", PinOrder: 0}
	beta := bufferDTO{ID: uuid.New(), NetworkID: net, Kind: "channel", Pinned: true, Name: "beta", PinOrder: 0}
	buffers := []bufferDTO{beta, alpha}

	got := pickStartupBuffer(networks, buffers, uuid.Nil)
	if got != alpha.ID {
		t.Fatalf("pickStartupBuffer() = %v, want alpha (%v) by name tiebreak", got, alpha.ID)
	}
}

func TestStateDirOverrideRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { stateDir = "" })
	stateDir = dir

	path, err := stateFilePath()
	if err != nil {
		t.Fatalf("stateFilePath() error = %v", err)
	}
	if want := filepath.Join(dir, "tui-state.json"); path != want {
		t.Fatalf("stateFilePath() = %q, want %q", path, want)
	}

	id := uuid.New()
	if err := savePersistedBufferID(id); err != nil {
		t.Fatalf("savePersistedBufferID() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written to override dir: %v", err)
	}
	if got := loadPersistedBufferID(); got != id {
		t.Fatalf("loadPersistedBufferID() = %v, want %v", got, id)
	}
}

func TestStateFilePathDefaultsToUserConfigDir(t *testing.T) {
	t.Cleanup(func() { stateDir = "" })
	stateDir = ""

	path, err := stateFilePath()
	if err != nil {
		t.Skipf("os.UserConfigDir() unavailable: %v", err)
	}
	if filepath.Base(path) != "tui-state.json" || filepath.Base(filepath.Dir(path)) != "lurker" {
		t.Fatalf("stateFilePath() = %q, want <user config dir>/lurker/tui-state.json", path)
	}
}
