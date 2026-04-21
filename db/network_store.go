package db

import (
	"context"
	"database/sql"
)

// UpsertNetwork inserts the network if missing (keyed by case-insensitive
// name) and returns the stored row. Existing rows are not modified — mutation
// goes through the API.
func UpsertNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
	if err := ValidateNetworkName(n.Name); err != nil {
		return Network{}, err
	}
	nameCI := NormalizeNetworkName(n.Name)

	var id int64
	err := d.QueryRowContext(ctx, `SELECT id FROM networks WHERE name_ci = ?`, nameCI).Scan(&id)
	if err == nil {
		n.ID = id
		return n, nil
	}
	if err != sql.ErrNoRows {
		return Network{}, err
	}

	tls := 0
	if n.TLS {
		tls = 1
	}
	var saslUser, saslPass any
	if n.SASLUser != "" {
		saslUser = n.SASLUser
		saslPass = n.SASLPass
	}
	var nextSortOrder int
	queryErr := d.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order) + 1, 0) FROM networks`).Scan(&nextSortOrder)
	if queryErr != nil {
		return Network{}, queryErr
	}
	res, err := d.ExecContext(ctx,
		`INSERT INTO networks(name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, nextSortOrder, Now())
	if err != nil {
		return Network{}, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return Network{}, err
	}
	n.ID = id
	return n, nil
}

// ListNetworks returns every network row for API state responses.
func ListNetworks(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,''), sort_order
		 FROM networks ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Network
	for rows.Next() {
		var n Network
		var tls int
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname, &n.SortOrder); err != nil {
			return nil, err
		}
		n.TLS = tls == 1
		out = append(out, n)
	}
	return out, rows.Err()
}
