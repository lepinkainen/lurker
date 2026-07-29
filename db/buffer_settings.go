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

// ErrInvalidPinnedReorder indicates the provided pinned reorder request is not
// a valid set of pinned buffer IDs.
var ErrInvalidPinnedReorder = errors.New("db: invalid pinned reorder")

// BufferSettings are per-buffer display preferences stored in the control DB.
type BufferSettings struct {
	BufferID               uuid.UUID
	ShowEmbeds             bool
	ShowPresenceEvents     bool
	CollapsePresenceEvents bool
	Pinned                 bool
	Archived               bool
	// PinOrder is the buffer's position in the pinned section. Only
	// meaningful while Pinned; assigned max+1 on pin so new pins append.
	PinOrder  int64
	UpdatedAt string
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
		PinOrder:               r.PinOrder,
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
	return getBufferSettings(ctx, controldb.New(d), bufferID)
}

// getBufferSettings is the shared implementation behind GetBufferSettings,
// parameterized over controldb.Queries so it can run against either a
// *sql.DB (public API) or a *sql.Tx (transactional callers like
// UpdateBufferSettings).
func getBufferSettings(ctx context.Context, q *controldb.Queries, bufferID uuid.UUID) (BufferSettings, error) {
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
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return BufferSettings{}, err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
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

	current, err := getBufferSettings(ctx, q, bufferID)
	if err != nil {
		return BufferSettings{}, err
	}
	if err := applyBufferSettingsPatch(ctx, q, &current, patch); err != nil {
		return BufferSettings{}, err
	}
	current.UpdatedAt = Now()
	if err := q.UpsertBufferSettings(ctx, controldb.UpsertBufferSettingsParams{
		BufferID:               bufferID[:],
		ShowEmbeds:             boolInt(current.ShowEmbeds),
		ShowPresenceEvents:     boolInt(current.ShowPresenceEvents),
		CollapsePresenceEvents: boolInt(current.CollapsePresenceEvents),
		Pinned:                 boolInt(current.Pinned),
		Archived:               boolInt(current.Archived),
		PinOrder:               current.PinOrder,
		UpdatedAt:              current.UpdatedAt,
	}); err != nil {
		return BufferSettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return BufferSettings{}, err
	}
	return current, nil
}

// applyBufferSettingsPatch folds a partial patch into current. Pinning an
// unpinned buffer assigns pin_order = MAX(pinned)+1 so new pins append to the
// end of the Pinned section; unpinning resets it.
func applyBufferSettingsPatch(ctx context.Context, q *controldb.Queries, current *BufferSettings, patch BufferSettingsPatch) error {
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
		if *patch.Pinned && !current.Pinned {
			maxOrder, err := q.MaxPinOrder(ctx)
			if err != nil {
				return err
			}
			current.PinOrder = maxOrder + 1
		}
		if !*patch.Pinned {
			current.PinOrder = 0
		}
		current.Pinned = *patch.Pinned
	}
	if patch.Archived != nil {
		current.Archived = *patch.Archived
	}
	return nil
}

// ReorderPinnedBuffers assigns pin_order 0..n-1 to the given pinned buffer
// IDs. Partial sets are allowed: unlisted pinned buffers keep their current
// pin_order. Returns the resulting (id, pin_order) for every pinned buffer.
func ReorderPinnedBuffers(ctx context.Context, d *sql.DB, ids []uuid.UUID) ([]BufferSortEntry, error) {
	if len(ids) == 0 {
		return nil, ErrInvalidPinnedReorder
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
	rows, err := q.ListPinnedBuffers(ctx)
	if err != nil {
		return nil, err
	}
	pinned := make(map[uuid.UUID]struct{}, len(rows))
	for _, r := range rows {
		id, perr := parseUUID(r.BufferID)
		if perr != nil {
			return nil, perr
		}
		pinned[id] = struct{}{}
	}

	if aerr := assignDenseOrder(ids, pinned, ErrInvalidPinnedReorder, func(order int, id uuid.UUID) error {
		return q.SetBufferPinOrder(ctx, controldb.SetBufferPinOrderParams{
			PinOrder: int64(order),
			BufferID: id[:],
		})
	}); aerr != nil {
		return nil, aerr
	}

	rows, err = q.ListPinnedBuffers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BufferSortEntry, 0, len(rows))
	for _, r := range rows {
		id, perr := parseUUID(r.BufferID)
		if perr != nil {
			return nil, perr
		}
		out = append(out, BufferSortEntry{ID: id, SortOrder: r.PinOrder})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
