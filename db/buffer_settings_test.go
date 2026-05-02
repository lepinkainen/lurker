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
	_, err = d.ExecContext(ctx, `INSERT INTO networks(id, name, name_ci, host, port, tls, nick, autoconnect, sort_order, created_at)
		VALUES (?, 'Libera', 'libera', 'irc.example', 6697, 1, 'tester', 1, 0, ?)`, netID[:], Now())
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

func TestBufferSettingsRejectsStatusBuffers(t *testing.T) {
	d, err := OpenControl(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	ctx := t.Context()
	netID := uuid.Must(uuid.NewV7())
	_, err = d.ExecContext(ctx, `INSERT INTO networks(id, name, name_ci, host, port, tls, nick, autoconnect, sort_order, created_at)
		VALUES (?, 'Libera', 'libera', 'irc.example', 6697, 1, 'tester', 1, 0, ?)`, netID[:], Now())
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
