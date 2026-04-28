package db

import (
	"context"
	"database/sql"
)

// UpsertNetwork inserts the network if missing, or updates its config fields
// from the provided values if it already exists (keyed by case-insensitive
// name). sort_order, autoconnect, and created_at are never modified.
func UpsertNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
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
	res, err := d.ExecContext(ctx,
		`INSERT INTO networks(name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, (SELECT COALESCE(MAX(sort_order)+1,0) FROM networks), ?)
		 ON CONFLICT(name_ci) DO UPDATE SET
		   name=excluded.name, host=excluded.host, port=excluded.port,
		   tls=excluded.tls, nick=excluded.nick, realname=excluded.realname,
		   sasl_user=excluded.sasl_user, sasl_pass=excluded.sasl_pass`,
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	if err != nil {
		return Network{}, err
	}
	id, err := res.LastInsertId()
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
