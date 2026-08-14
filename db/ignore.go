package db

import (
	"context"
	"database/sql"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// Ignore levels. "hide" drops matching messages before storage; "mute"
// stores and shows them but suppresses the unread indicator (mentions and
// highlights still count).
const (
	IgnoreLevelHide = "hide"
	IgnoreLevelMute = "mute"
)

// IgnoreEntry is a single configured ignore mask and its enforcement level.
type IgnoreEntry struct {
	Mask  string
	Level string
}

// CreateIgnore adds an ignore mask for a network at the given level.
// Re-adding an existing mask updates its level (upsert).
func CreateIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask, level string) error {
	id := newID()
	return controldb.New(d).CreateIgnore(ctx, controldb.CreateIgnoreParams{
		ID:        id[:],
		NetworkID: networkID[:],
		Mask:      mask,
		CreatedAt: Now(),
		Level:     level,
	})
}

// DeleteIgnore removes an ignore mask for a network, regardless of level.
func DeleteIgnore(ctx context.Context, d *sql.DB, networkID uuid.UUID, mask string) error {
	return controldb.New(d).DeleteIgnore(ctx, controldb.DeleteIgnoreParams{
		NetworkID: networkID[:],
		Mask:      mask,
	})
}

// ListIgnores returns all ignore entries (mask + level) for a network.
func ListIgnores(ctx context.Context, d *sql.DB, networkID uuid.UUID) ([]IgnoreEntry, error) {
	rows, err := controldb.New(d).ListIgnores(ctx, networkID[:])
	if err != nil {
		return nil, err
	}
	entries := make([]IgnoreEntry, len(rows))
	for i, r := range rows {
		entries[i] = IgnoreEntry{Mask: r.Mask, Level: r.Level}
	}
	return entries, nil
}

// IgnoreLevelFor returns the ignore level ("", hide, or mute) that applies
// to nick given a network's ignore entries. Matching is case-insensitive
// glob matching against the nick (mirrors the historical isIgnored logic).
// When a nick matches both a hide and a mute mask, hide wins.
func IgnoreLevelFor(entries []IgnoreEntry, nick string) string {
	nickLower := strings.ToLower(nick)
	level := ""
	for _, entry := range entries {
		matched, _ := path.Match(strings.ToLower(entry.Mask), nickLower)
		if !matched {
			continue
		}
		if entry.Level == IgnoreLevelHide {
			return IgnoreLevelHide
		}
		if entry.Level == IgnoreLevelMute {
			level = IgnoreLevelMute
		}
	}
	return level
}
