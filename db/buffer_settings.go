package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// ErrBufferNotFound indicates the requested global buffer does not exist.
var ErrBufferNotFound = errors.New("db: buffer not found")

// ErrBufferSettingsUnsupported indicates settings cannot be changed for this buffer kind.
var ErrBufferSettingsUnsupported = errors.New("db: buffer settings unsupported for buffer kind")

// BufferSettings are per-buffer display preferences stored in the control DB.
type BufferSettings struct {
	BufferID               uuid.UUID
	ShowEmbeds             bool
	ShowPresenceEvents     bool
	CollapsePresenceEvents bool
	Pinned                 bool
	Archived               bool
	UpdatedAt              string
}

// BufferSettingsPatch is a partial update. Nil fields mean unchanged.
type BufferSettingsPatch struct {
	ShowEmbeds             *bool
	ShowPresenceEvents     *bool
	CollapsePresenceEvents *bool
	Pinned                 *bool
	Archived               *bool
}

func defaultBufferSettings(bufferID uuid.UUID) BufferSettings {
	return BufferSettings{BufferID: bufferID, ShowEmbeds: true, ShowPresenceEvents: true}
}

func bufferSettingsFromRow(r controldb.BufferSetting) BufferSettings {
	return BufferSettings{
		BufferID:               scanUUID(r.BufferID),
		ShowEmbeds:             r.ShowEmbeds != 0,
		ShowPresenceEvents:     r.ShowPresenceEvents != 0,
		CollapsePresenceEvents: r.CollapsePresenceEvents != 0,
		Pinned:                 r.Pinned != 0,
		Archived:               r.Archived != 0,
		UpdatedAt:              r.UpdatedAt,
	}
}

// ListBufferSettings returns persisted settings keyed by buffer id. Missing rows use defaults elsewhere.
func ListBufferSettings(ctx context.Context, d *sql.DB) (map[uuid.UUID]BufferSettings, error) {
	rows, err := controldb.New(d).ListBufferSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]BufferSettings{}
	for _, r := range rows {
		s := bufferSettingsFromRow(r)
		out[s.BufferID] = s
	}
	return out, nil
}

// GetBufferSettings returns settings with defaults when no row exists.
func GetBufferSettings(ctx context.Context, d *sql.DB, bufferID uuid.UUID) (BufferSettings, error) {
	q := controldb.New(d)
	r, err := q.GetBufferSettings(ctx, bufferID[:])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			exists, exErr := q.BufferRegistryExists(ctx, bufferID[:])
			if exErr != nil {
				return BufferSettings{}, exErr
			}
			if exists == 0 {
				return BufferSettings{}, ErrBufferNotFound
			}
			return defaultBufferSettings(bufferID), nil
		}
		return BufferSettings{}, err
	}
	return bufferSettingsFromRow(r), nil
}

// UpdateBufferSettings applies a partial settings patch for a channel or
// query buffer. Status buffers have no user-tunable settings.
func UpdateBufferSettings(ctx context.Context, d *sql.DB, bufferID uuid.UUID, patch BufferSettingsPatch) (BufferSettings, error) {
	q := controldb.New(d)
	kind, err := q.GetBufferRegistryKind(ctx, bufferID[:])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BufferSettings{}, ErrBufferNotFound
		}
		return BufferSettings{}, err
	}
	if kind != BufferChannel && kind != BufferQuery {
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
	if patch.Archived != nil {
		current.Archived = *patch.Archived
	}
	current.UpdatedAt = Now()
	if err := q.UpsertBufferSettings(ctx, controldb.UpsertBufferSettingsParams{
		BufferID:               bufferID[:],
		ShowEmbeds:             boolInt(current.ShowEmbeds),
		ShowPresenceEvents:     boolInt(current.ShowPresenceEvents),
		CollapsePresenceEvents: boolInt(current.CollapsePresenceEvents),
		Pinned:                 boolInt(current.Pinned),
		Archived:               boolInt(current.Archived),
		UpdatedAt:              current.UpdatedAt,
	}); err != nil {
		return BufferSettings{}, err
	}
	return current, nil
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
