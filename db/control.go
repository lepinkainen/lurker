package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrNetworkNotFound indicates the requested network row does not exist.
var ErrNetworkNotFound = errors.New("db: network not found")

// GetNetwork returns a network by ID.
func GetNetwork(ctx context.Context, d *sql.DB, id int64) (Network, error) {
	var n Network
	var tls int
	var saslUser, saslPass sql.NullString
	err := d.QueryRowContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,''), sasl_user, sasl_pass
		 FROM networks WHERE id = ?`, id,
	).Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname, &saslUser, &saslPass)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Network{}, ErrNetworkNotFound
		}
		return Network{}, err
	}
	n.TLS = tls == 1
	n.SASLUser = saslUser.String
	n.SASLPass = saslPass.String
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
	res, err := d.ExecContext(ctx,
		`INSERT INTO networks(name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	if err != nil {
		return Network{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Network{}, err
	}
	return GetNetwork(ctx, d, id)
}

// UpdateNetwork updates a network row and returns the stored record.
func UpdateNetwork(ctx context.Context, d *sql.DB, id int64, n Network) (Network, error) {
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
		n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, id)
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
func DeleteNetwork(ctx context.Context, d *sql.DB, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM networks WHERE id = ?`, id)
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
