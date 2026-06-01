package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// ErrNetworkNotFound indicates the requested network row does not exist.
var ErrNetworkNotFound = errors.New("db: network not found")

// ErrInvalidNetworkReorder indicates the provided reorder request is not a
// valid full permutation of the existing network IDs.
var ErrInvalidNetworkReorder = errors.New("db: invalid network reorder")

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// GetNetwork returns a network by ID.
func GetNetwork(ctx context.Context, d *sql.DB, id uuid.UUID) (Network, error) {
	row, err := controldb.New(d).GetNetwork(ctx, id[:])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Network{}, ErrNetworkNotFound
		}
		return Network{}, err
	}
	rid, err := parseUUID(row.ID)
	if err != nil {
		return Network{}, err
	}
	return Network{
		ID:        rid,
		Name:      row.Name,
		Kind:      row.Kind,
		Host:      row.Host,
		Port:      int(row.Port),
		TLS:       row.Tls == 1,
		Nick:      row.Nick,
		Realname:  row.Realname,
		SASLUser:  row.SaslUser.String,
		SASLPass:  row.SaslPass.String,
		SortOrder: int(row.SortOrder),
		Disabled:  row.Disabled == 1,
	}, nil
}

// CreateNetwork inserts a network row and returns the stored record.
func CreateNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
	if err := ValidateNetworkName(n.Name); err != nil {
		return Network{}, err
	}
	kind := n.Kind
	if kind == "" {
		kind = NetworkKindIRC
	}
	id := newID()
	tls := int64(0)
	if n.TLS {
		tls = 1
	}
	if err := controldb.New(d).CreateNetwork(ctx, controldb.CreateNetworkParams{
		ID:        id[:],
		Name:      n.Name,
		NameCi:    NormalizeNetworkName(n.Name),
		Kind:      kind,
		Host:      n.Host,
		Port:      int64(n.Port),
		Tls:       tls,
		Nick:      n.Nick,
		Realname:  nullStr(n.Realname),
		SaslUser:  nullStr(n.SASLUser),
		SaslPass:  nullStr(n.SASLPass),
		CreatedAt: Now(),
	}); err != nil {
		return Network{}, err
	}
	return GetNetwork(ctx, d, id)
}

// UpdateNetwork updates a network row and returns the stored record.
func UpdateNetwork(ctx context.Context, d *sql.DB, id uuid.UUID, n Network) (Network, error) {
	current, err := GetNetwork(ctx, d, id)
	if err != nil {
		return Network{}, err
	}
	if n.Name == "" {
		n.Name = current.Name
	}
	if n.Host == "" {
		n.Host = current.Host
	}
	if n.Port == 0 {
		n.Port = current.Port
	}
	if n.Nick == "" {
		n.Nick = current.Nick
	}
	if n.Realname == "" {
		n.Realname = current.Realname
	}
	if n.SASLUser == "" && current.SASLUser != "" {
		n.SASLUser = current.SASLUser
		n.SASLPass = current.SASLPass
	}
	if vErr := ValidateNetworkName(n.Name); vErr != nil {
		return Network{}, vErr
	}
	tls := int64(0)
	if n.TLS {
		tls = 1
	}
	affected, err := controldb.New(d).UpdateNetwork(ctx, controldb.UpdateNetworkParams{
		Name:     n.Name,
		NameCi:   NormalizeNetworkName(n.Name),
		Host:     n.Host,
		Port:     int64(n.Port),
		Tls:      tls,
		Nick:     n.Nick,
		Realname: nullStr(n.Realname),
		SaslUser: nullStr(n.SASLUser),
		SaslPass: nullStr(n.SASLPass),
		ID:       id[:],
	})
	if err != nil {
		return Network{}, err
	}
	if affected == 0 {
		return Network{}, ErrNetworkNotFound
	}
	return GetNetwork(ctx, d, id)
}

// DeleteNetwork deletes a network row by ID.
func DeleteNetwork(ctx context.Context, d *sql.DB, id uuid.UUID) error {
	affected, err := controldb.New(d).DeleteNetwork(ctx, id[:])
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNetworkNotFound
	}
	return nil
}

// ReorderNetworks sets network ordering to match the given IDs.
func ReorderNetworks(ctx context.Context, d *sql.DB, ids []uuid.UUID) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := controldb.New(tx)
	existingBytes, err := q.ListNetworkIDs(ctx)
	if err != nil {
		return err
	}
	existing := map[uuid.UUID]struct{}{}
	for _, b := range existingBytes {
		id, perr := parseUUID(b)
		if perr != nil {
			return perr
		}
		existing[id] = struct{}{}
	}
	if len(existing) != len(ids) {
		return ErrInvalidNetworkReorder
	}

	seen := map[uuid.UUID]struct{}{}
	for order, id := range ids {
		if _, ok := existing[id]; !ok {
			return ErrInvalidNetworkReorder
		}
		if _, dup := seen[id]; dup {
			return ErrInvalidNetworkReorder
		}
		seen[id] = struct{}{}
		if err := q.SetNetworkSortOrder(ctx, controldb.SetNetworkSortOrderParams{
			SortOrder: int64(order),
			ID:        id[:],
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
