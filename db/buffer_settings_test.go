package db

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestBufferSettingsDefaultsAndPatch(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	ctx := t.Context()
	netID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO networks(id, name, name_ci, host, port, tls, nick, sort_order, created_at)
		VALUES (?, 'Libera', 'libera', 'irc.example', 6697, 1, 'tester', 0, ?)`, netID[:], Now())
	if err != nil {
		t.Fatal(err)
	}
	channelID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, '#go', ?, ?)`, channelID[:], netID[:], BufferChannel, Now())
	if err != nil {
		t.Fatal(err)
	}

	settings, err := GetBufferSettings(ctx, d, channelID)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.ShowEmbeds || !settings.ShowPresenceEvents || settings.CollapsePresenceEvents || settings.Pinned {
		t.Fatalf("defaults = %+v", settings)
	}

	showEmbeds := false
	pinned := true
	settings, err = UpdateBufferSettings(ctx, d, channelID, BufferSettingsPatch{ShowEmbeds: &showEmbeds, Pinned: &pinned})
	if err != nil {
		t.Fatal(err)
	}
	if settings.ShowEmbeds || !settings.ShowPresenceEvents || !settings.Pinned {
		t.Fatalf("patched = %+v", settings)
	}
}

func TestPinOrderAppendsAndReorders(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	ctx := t.Context()
	netID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO networks(id, name, name_ci, host, port, tls, nick, sort_order, created_at)
		VALUES (?, 'Libera', 'libera', 'irc.example', 6697, 1, 'tester', 0, ?)`, netID[:], Now())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 3)
	for i, name := range []string{"#a", "#b", "#c"} {
		ids[i] = uuid.Must(uuid.NewV7())
		_, err = d.ExecContext(ctx, `INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?)`, ids[i][:], netID[:], name, BufferChannel, Now())
		if err != nil {
			t.Fatal(err)
		}
	}

	pinned := true
	for i, id := range ids {
		s, uerr := UpdateBufferSettings(ctx, d, id, BufferSettingsPatch{Pinned: &pinned})
		if uerr != nil {
			t.Fatal(uerr)
		}
		if s.PinOrder != int64(i) {
			t.Fatalf("pin %d: PinOrder = %d, want %d", i, s.PinOrder, i)
		}
	}

	// Re-pinning an already pinned buffer keeps its position.
	s, err := UpdateBufferSettings(ctx, d, ids[0], BufferSettingsPatch{Pinned: &pinned})
	if err != nil {
		t.Fatal(err)
	}
	if s.PinOrder != 0 {
		t.Fatalf("re-pin PinOrder = %d, want 0", s.PinOrder)
	}

	entries, err := ReorderPinnedBuffers(ctx, d, []uuid.UUID{ids[2], ids[0], ids[1]})
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]int64{}
	for _, e := range entries {
		got[e.ID] = e.SortOrder
	}
	if got[ids[2]] != 0 || got[ids[0]] != 1 || got[ids[1]] != 2 {
		t.Fatalf("reordered = %v", got)
	}

	// Unpin resets pin_order; next pin appends after remaining max.
	unpinned := false
	if _, err = UpdateBufferSettings(ctx, d, ids[1], BufferSettingsPatch{Pinned: &unpinned}); err != nil {
		t.Fatal(err)
	}
	s, err = UpdateBufferSettings(ctx, d, ids[1], BufferSettingsPatch{Pinned: &pinned})
	if err != nil {
		t.Fatal(err)
	}
	if s.PinOrder != 2 {
		t.Fatalf("re-pin after unpin PinOrder = %d, want 2", s.PinOrder)
	}

	// Unpinned or unknown ids are rejected.
	if _, err = ReorderPinnedBuffers(ctx, d, []uuid.UUID{uuid.Must(uuid.NewV7())}); !errors.Is(err, ErrInvalidPinnedReorder) {
		t.Fatalf("err = %v, want ErrInvalidPinnedReorder", err)
	}
	if _, err = ReorderPinnedBuffers(ctx, d, nil); !errors.Is(err, ErrInvalidPinnedReorder) {
		t.Fatalf("err = %v, want ErrInvalidPinnedReorder", err)
	}
}

func TestBufferSettingsRejectsStatusBuffers(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	ctx := t.Context()
	netID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO networks(id, name, name_ci, host, port, tls, nick, sort_order, created_at)
		VALUES (?, 'Libera', 'libera', 'irc.example', 6697, 1, 'tester', 0, ?)`, netID[:], Now())
	if err != nil {
		t.Fatal(err)
	}
	statusID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, '*status*', ?, ?)`, statusID[:], netID[:], BufferStatus, Now())
	if err != nil {
		t.Fatal(err)
	}
	pinned := true
	_, err = UpdateBufferSettings(ctx, d, statusID, BufferSettingsPatch{Pinned: &pinned})
	if !errors.Is(err, ErrBufferSettingsUnsupported) {
		t.Fatalf("err = %v, want ErrBufferSettingsUnsupported", err)
	}
}
