package main

import (
	"context"
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
)

// TestPinnedFixtureRefsExist guards against typos drifting between
// pinnedFixture and fixture: every pinned ref must name a real, non-archived
// seeded channel.
func TestPinnedFixtureRefsExist(t *testing.T) {
	channels := map[seedBufferRef]seedChannel{}
	for _, n := range fixture() {
		for _, c := range n.Channels {
			channels[seedBufferRef{Network: n.Name, Buffer: c.Name}] = c
		}
	}
	refs := pinnedFixture()
	if len(refs) < 2 {
		t.Fatalf("want at least 2 pinned fixtures, got %d", len(refs))
	}
	networks := map[string]struct{}{}
	for _, ref := range refs {
		c, ok := channels[ref]
		if !ok {
			t.Fatalf("pinned fixture %s/%s is not a seeded channel", ref.Network, ref.Buffer)
		}
		if c.Archived {
			t.Errorf("pinned fixture %s/%s is archived", ref.Network, ref.Buffer)
		}
		networks[ref.Network] = struct{}{}
	}
	if len(networks) < 2 {
		t.Errorf("want pinned fixtures spanning >1 network, got %d", len(networks))
	}
	// Ordering bugs are only visible if the intended order differs from the
	// name order clients fall back to when they ignore pin_order.
	alphabetical := true
	for i := 1; i < len(refs); i++ {
		if refs[i-1].Buffer > refs[i].Buffer {
			alphabetical = false
			break
		}
	}
	if alphabetical {
		t.Error("pinned fixture order is alphabetical by channel name; pick an order that exposes name-order fallbacks")
	}
}

// TestSeedAssignsPinOrder seeds a throwaway data dir and checks the pinned
// channels land with dense pin_order values matching the fixture order.
func TestSeedAssignsPinOrder(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatalf("open stores: %v", err)
	}
	defer func() { _ = stores.Close() }()

	ctx := context.Background()
	if err := seed(ctx, stores, fixture()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	settings, err := ircdb.ListBufferSettings(ctx, stores.Control)
	if err != nil {
		t.Fatalf("list buffer settings: %v", err)
	}

	gotOrder := map[string]int64{}
	for id, s := range settings {
		if !s.Pinned {
			continue
		}
		_, name, _, lerr := ircdb.LookupBufferRegistry(ctx, stores.Control, id)
		if lerr != nil {
			t.Fatalf("lookup buffer %s: %v", id, lerr)
		}
		gotOrder[name] = s.PinOrder
	}

	refs := pinnedFixture()
	if len(gotOrder) != len(refs) {
		t.Fatalf("pinned buffers = %v, want %d entries", gotOrder, len(refs))
	}
	for i, ref := range refs {
		order, ok := gotOrder[ref.Buffer]
		if !ok {
			t.Errorf("%s not pinned", ref.Buffer)
			continue
		}
		if order != int64(i) {
			t.Errorf("%s pin_order = %d, want %d", ref.Buffer, order, i)
		}
	}
}
