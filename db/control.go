package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// ErrNetworkNotFound indicates the requested network row does not exist.
var ErrNetworkNotFound = errors.New("db: network not found")

// ErrInvalidNetworkReorder indicates the provided reorder request is not a
// valid full permutation of the existing network IDs.
var ErrInvalidNetworkReorder = errors.New("db: invalid network reorder")

// GetNetwork returns a network by ID.
func GetNetwork(ctx context.Context, d *sql.DB, id uuid.UUID) (Network, error) {
	var n Network
	var tls, disabled int
	var saslUser, saslPass sql.NullString
	err := d.QueryRowContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,''), sasl_user, sasl_pass, sort_order, disabled
		 FROM networks WHERE id = ?`, id[:],
	).Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname, &saslUser, &saslPass, &n.SortOrder, &disabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Network{}, ErrNetworkNotFound
		}
		return Network{}, err
	}
	n.TLS = tls == 1
	n.SASLUser = saslUser.String
	n.SASLPass = saslPass.String
	n.Disabled = disabled == 1
	return n, nil
}

// CreateNetwork inserts a network row and returns the stored record.
func CreateNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
	if err := ValidateNetworkName(n.Name); err != nil {
		return Network{}, err
	}
	nameCI := NormalizeNetworkName(n.Name)
	tls := 0
	if n.TLS {
		tls = 1
	}
	var saslUser, saslPass any
	if n.SASLUser != "" {
		saslUser = n.SASLUser
		saslPass = n.SASLPass
	}
	id := newID()
	_, err := d.ExecContext(ctx,
		`INSERT INTO networks(id, name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, (SELECT COALESCE(MAX(sort_order) + 1, 0) FROM networks), ?)`,
		id[:], n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	if err != nil {
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
	validateErr := ValidateNetworkName(n.Name)
	if validateErr != nil {
		return Network{}, validateErr
	}
	nameCI := NormalizeNetworkName(n.Name)
	tls := 0
	if n.TLS {
		tls = 1
	}
	var saslUser, saslPass any
	if n.SASLUser != "" {
		saslUser = n.SASLUser
		saslPass = n.SASLPass
	}
	res, err := d.ExecContext(ctx,
		`UPDATE networks
		 SET name = ?, name_ci = ?, host = ?, port = ?, tls = ?, nick = ?, realname = ?, sasl_user = ?, sasl_pass = ?
		 WHERE id = ?`,
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, id[:])
	if err != nil {
		return Network{}, err
	}
	affected, err := res.RowsAffected()
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
	res, err := d.ExecContext(ctx, `DELETE FROM networks WHERE id = ?`, id[:])
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
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

	rows, err := tx.QueryContext(ctx, `SELECT id FROM networks`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	existing := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		existing[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
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
		if _, err := tx.ExecContext(ctx, `UPDATE networks SET sort_order = ? WHERE id = ?`, order, id[:]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
