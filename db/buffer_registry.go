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

// ReorderNetworkBuffers assigns sort_order to every channel buffer of a
// network: the given IDs form the ordered prefix, and channels omitted from
// the request follow in their previous (sort_order, name) relative order. The
// result is always dense and collision-free, so partial sets — a client
// reordering only the channels it renders — cannot leave duplicate sort_order
// values behind. Returns the resulting (id, sort_order) for every channel
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
	channels := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		id, perr := parseUUID(r.ID)
		if perr != nil {
			return nil, perr
		}
		channels = append(channels, id)
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

// assignDenseOrder validates that ids are all members of all (with no
// duplicates) and, if so, renumbers the whole set to a dense, collision-free
// 0..len(all)-1: ids form the ordered prefix, then every member of all that
// was omitted follows in its existing relative order. Normalizing the omitted
// tail is what keeps partial sets from leaving duplicate order values behind —
// e.g. a client that reorders only the channels it renders while an archived
// one holds a stale position. Returns sentinel on the first invalid or
// duplicate id, or the first error from setOrder. Shared by
// ReorderNetworkBuffers and ReorderPinnedBuffers.
func assignDenseOrder(ids, all []uuid.UUID, sentinel error, setOrder func(order int, id uuid.UUID) error) error {
	valid := make(map[uuid.UUID]struct{}, len(all))
	for _, id := range all {
		valid[id] = struct{}{}
	}
	submitted := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := valid[id]; !ok {
			return sentinel
		}
		if _, dup := submitted[id]; dup {
			return sentinel
		}
		submitted[id] = struct{}{}
	}

	order := 0
	for _, id := range ids {
		if err := setOrder(order, id); err != nil {
			return err
		}
		order++
	}
	for _, id := range all {
		if _, ok := submitted[id]; ok {
			continue
		}
		if err := setOrder(order, id); err != nil {
			return err
		}
		order++
	}
	return nil
}
