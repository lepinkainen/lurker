package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// ErrInvalidBufferReorder indicates the provided buffer reorder request is not
// a valid set of channel buffer IDs for the network.
var ErrInvalidBufferReorder = errors.New("db: invalid buffer reorder")

// BufferSortEntry is one buffer's persisted sort position.
type BufferSortEntry struct {
	ID        uuid.UUID
	SortOrder int64
}

// LookupBufferRegistry resolves a global buffer registry row by ID.
func LookupBufferRegistry(ctx context.Context, d *sql.DB, bufferID uuid.UUID) (networkID uuid.UUID, name, kind string, err error) {
	row, err := controldb.New(d).LookupBufferRegistry(ctx, bufferID[:])
	if err != nil {
		return uuid.Nil, "", "", err
	}
	nid, err := parseUUID(row.NetworkID)
	if err != nil {
		return uuid.Nil, "", "", err
	}
	return nid, row.Name, row.Kind, nil
}

// ReorderNetworkBuffers assigns sort_order 0..n-1 to the given channel buffer
// IDs of a network. Partial sets are allowed: unlisted channels keep their
// current sort_order. Returns the resulting (id, sort_order) for every channel
// buffer of the network.
func ReorderNetworkBuffers(ctx context.Context, d *sql.DB, networkID uuid.UUID, ids []uuid.UUID) ([]BufferSortEntry, error) {
	if len(ids) == 0 {
		return nil, ErrInvalidBufferReorder
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
	rows, err := q.ListChannelBuffersForNetwork(ctx, networkID[:])
	if err != nil {
		return nil, err
	}
	channels := make(map[uuid.UUID]struct{}, len(rows))
	for _, r := range rows {
		id, perr := parseUUID(r.ID)
		if perr != nil {
			return nil, perr
		}
		channels[id] = struct{}{}
	}

	if aerr := assignDenseOrder(ids, channels, ErrInvalidBufferReorder, func(order int, id uuid.UUID) error {
		return q.SetBufferSortOrder(ctx, controldb.SetBufferSortOrderParams{
			SortOrder: int64(order),
			ID:        id[:],
		})
	}); aerr != nil {
		return nil, aerr
	}

	rows, err = q.ListChannelBuffersForNetwork(ctx, networkID[:])
	if err != nil {
		return nil, err
	}
	out := make([]BufferSortEntry, 0, len(rows))
	for _, r := range rows {
		id, perr := parseUUID(r.ID)
		if perr != nil {
			return nil, perr
		}
		out = append(out, BufferSortEntry{ID: id, SortOrder: r.SortOrder})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// assignDenseOrder validates that ids are all members of valid (with no
// duplicates) and, if so, assigns them dense positions 0..len(ids)-1 via
// setOrder in the given order. Returns sentinel on the first invalid or
// duplicate id, or the first error from setOrder. Shared by
// ReorderNetworkBuffers and ReorderPinnedBuffers.
func assignDenseOrder(ids []uuid.UUID, valid map[uuid.UUID]struct{}, sentinel error, setOrder func(order int, id uuid.UUID) error) error {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for order, id := range ids {
		if _, ok := valid[id]; !ok {
			return sentinel
		}
		if _, dup := seen[id]; dup {
			return sentinel
		}
		seen[id] = struct{}{}
		if err := setOrder(order, id); err != nil {
			return err
		}
	}
	return nil
}
