package main

import (
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
