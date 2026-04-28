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
	_, err := d.ExecContext(ctx,
		`INSERT INTO networks(name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, (SELECT COALESCE(MAX(sort_order)+1,0) FROM networks), ?)
		 ON CONFLICT(name_ci) DO UPDATE SET
		   name=excluded.name, host=excluded.host, port=excluded.port,
		   tls=excluded.tls, nick=excluded.nick, realname=excluded.realname,
		   sasl_user=excluded.sasl_user, sasl_pass=excluded.sasl_pass,
		   disabled=0`,
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	if err != nil {
		return Network{}, err
	}

	// LastInsertId is unreliable for INSERT .. ON CONFLICT DO UPDATE because
	// SQLite leaves it at the connection's previous insert rowid, or zero on a
	// fresh connection. Resolve the stored row explicitly so callers always get
	// the real network ID.
	var id int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM networks WHERE name_ci = ?`, nameCI).Scan(&id); err != nil {
		return Network{}, err
	}
	return GetNetwork(ctx, d, id)
}

// ListNetworks returns every network row for API state responses.
func ListNetworks(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,''), sort_order, disabled
		 FROM networks ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Network
	for rows.Next() {
		var n Network
		var tls, disabled int
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname, &n.SortOrder, &disabled); err != nil {
			return nil, err
		}
		n.TLS = tls == 1
		n.Disabled = disabled == 1
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkNonYAMLNetworksDisabled sets disabled=1 on all networks whose name is
// not in the provided list. Used at startup to disable networks removed from
// config.yaml without deleting their log data.
func MarkNonYAMLNetworksDisabled(ctx context.Context, d *sql.DB, yamlNames []string) error {
	if len(yamlNames) == 0 {
		return nil
	}
	normalized := make([]any, len(yamlNames))
	for i, name := range yamlNames {
		normalized[i] = NormalizeNetworkName(name)
	}
	placeholders := make([]byte, 0, len(yamlNames)*2)
	for i := range yamlNames {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	_, err := d.ExecContext(ctx,
		`UPDATE networks SET disabled=1 WHERE disabled=0 AND name_ci NOT IN (`+string(placeholders)+`)`,
		normalized...)
	return err
}

// SetNetworkDisabled sets the disabled flag on a single network.
func SetNetworkDisabled(ctx context.Context, d *sql.DB, id int64, disabled bool) error {
	v := 0
	if disabled {
		v = 1
	}
	res, err := d.ExecContext(ctx, `UPDATE networks SET disabled=? WHERE id=?`, v, id)
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

// ListNetworksWithSASL returns every non-disabled network row including SASL
// credentials, ordered by sort_order. Used for config export only.
func ListNetworksWithSASL(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,''), sasl_user, sasl_pass, sort_order
		 FROM networks WHERE disabled=0 ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Network
	for rows.Next() {
		var n Network
		var tls int
		var saslUser, saslPass sql.NullString
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname, &saslUser, &saslPass, &n.SortOrder); err != nil {
			return nil, err
		}
		n.TLS = tls == 1
		n.SASLUser = saslUser.String
		n.SASLPass = saslPass.String
		out = append(out, n)
	}
	return out, rows.Err()
}
