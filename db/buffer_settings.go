package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrBufferNotFound indicates the requested global buffer does not exist.
var ErrBufferNotFound = errors.New("db: buffer not found")

// ErrBufferSettingsUnsupported indicates settings cannot be changed for this buffer kind.
var ErrBufferSettingsUnsupported = errors.New("db: buffer settings unsupported for buffer kind")

// BufferSettings are per-buffer display preferences stored in the control DB.
type BufferSettings struct {
	BufferID               int64
	ShowEmbeds             bool
	ShowPresenceEvents     bool
	CollapsePresenceEvents bool
	Pinned                 bool
	UpdatedAt              string
}

// BufferSettingsPatch is a partial update. Nil fields mean unchanged.
type BufferSettingsPatch struct {
	ShowEmbeds             *bool
	ShowPresenceEvents     *bool
	CollapsePresenceEvents *bool
	Pinned                 *bool
}

func defaultBufferSettings(bufferID int64) BufferSettings {
	return BufferSettings{BufferID: bufferID, ShowEmbeds: true, ShowPresenceEvents: true}
}

// ListBufferSettings returns persisted settings keyed by buffer id. Missing rows use defaults elsewhere.
func ListBufferSettings(ctx context.Context, d *sql.DB) (map[int64]BufferSettings, error) {
	rows, err := d.QueryContext(ctx, `SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at FROM buffer_settings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]BufferSettings{}
	for rows.Next() {
		var s BufferSettings
		var showEmbeds, showPresence, collapsePresence, pinned int
		if err := rows.Scan(&s.BufferID, &showEmbeds, &showPresence, &collapsePresence, &pinned, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ShowEmbeds = showEmbeds != 0
		s.ShowPresenceEvents = showPresence != 0
		s.CollapsePresenceEvents = collapsePresence != 0
		s.Pinned = pinned != 0
		out[s.BufferID] = s
	}
	return out, rows.Err()
}

// GetBufferSettings returns settings with defaults when no row exists.
func GetBufferSettings(ctx context.Context, d *sql.DB, bufferID int64) (BufferSettings, error) {
	var s BufferSettings
	var showEmbeds, showPresence, collapsePresence, pinned int
	err := d.QueryRowContext(ctx, `SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at FROM buffer_settings WHERE buffer_id = ?`, bufferID).
		Scan(&s.BufferID, &showEmbeds, &showPresence, &collapsePresence, &pinned, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var exists int
			if scanErr := d.QueryRowContext(ctx, `SELECT COUNT(1) FROM buffer_registry WHERE id = ?`, bufferID).Scan(&exists); scanErr != nil {
				return BufferSettings{}, scanErr
			}
			if exists == 0 {
				return BufferSettings{}, ErrBufferNotFound
			}
			return defaultBufferSettings(bufferID), nil
		}
		return BufferSettings{}, err
	}
	s.ShowEmbeds = showEmbeds != 0
	s.ShowPresenceEvents = showPresence != 0
	s.CollapsePresenceEvents = collapsePresence != 0
	s.Pinned = pinned != 0
	return s, nil
}

// UpdateBufferSettings applies a partial settings patch for a channel buffer.
func UpdateBufferSettings(ctx context.Context, d *sql.DB, bufferID int64, patch BufferSettingsPatch) (BufferSettings, error) {
	var kind string
	if err := d.QueryRowContext(ctx, `SELECT kind FROM buffer_registry WHERE id = ?`, bufferID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BufferSettings{}, ErrBufferNotFound
		}
		return BufferSettings{}, err
	}
	if kind != BufferChannel {
		return BufferSettings{}, ErrBufferSettingsUnsupported
	}

	current, err := GetBufferSettings(ctx, d, bufferID)
	if err != nil {
		return BufferSettings{}, err
	}
	if patch.ShowEmbeds != nil {
		current.ShowEmbeds = *patch.ShowEmbeds
	}
	if patch.ShowPresenceEvents != nil {
		current.ShowPresenceEvents = *patch.ShowPresenceEvents
	}
	if patch.CollapsePresenceEvents != nil {
		current.CollapsePresenceEvents = *patch.CollapsePresenceEvents
	}
	if patch.Pinned != nil {
		current.Pinned = *patch.Pinned
	}
	current.UpdatedAt = Now()
	_, err = d.ExecContext(ctx, `INSERT INTO buffer_settings(buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(buffer_id) DO UPDATE SET
		show_embeds=excluded.show_embeds,
		show_presence_events=excluded.show_presence_events,
		collapse_presence_events=excluded.collapse_presence_events,
		pinned=excluded.pinned,
		updated_at=excluded.updated_at`,
		bufferID, boolInt(current.ShowEmbeds), boolInt(current.ShowPresenceEvents), boolInt(current.CollapsePresenceEvents), boolInt(current.Pinned), current.UpdatedAt)
	if err != nil {
		return BufferSettings{}, err
	}
	return current, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
