package db

import (
	"context"
	"database/sql"
	"time"
)

// Now returns the current time formatted for storage (UTC, ISO-8601 with ms).
func Now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// FormatTime formats t for storage.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return Now()
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Network is the minimal network row used at startup.
type Network struct {
	ID       int64
	Name     string
	Host     string
	Port     int
	TLS      bool
	Nick     string
	Realname string
	SASLUser string
	SASLPass string
}

// UpsertNetwork inserts the network if missing (keyed by name) and returns
// the stored row. Existing rows are not modified — mutation goes through a
// future API, not the seed.
func UpsertNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
	if err := ValidateNetworkName(n.Name); err != nil {
		return Network{}, err
	}
	nameCI := NormalizeNetworkName(n.Name)

	var id int64
	hasNameCI, err := tableHasColumn(ctx, d, "networks", "name_ci")
	if err != nil {
		return Network{}, err
	}
	lookupQuery := `SELECT id FROM networks WHERE lower(name) = ?`
	if hasNameCI {
		lookupQuery = `SELECT id FROM networks WHERE name_ci = ?`
	}
	err = d.QueryRowContext(ctx, lookupQuery, nameCI).Scan(&id)
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
	var res sql.Result
	if hasNameCI {
		res, err = d.ExecContext(ctx,
			`INSERT INTO networks(name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			n.Name, nameCI, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	} else {
		res, err = d.ExecContext(ctx,
			`INSERT INTO networks(name, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			n.Name, n.Host, n.Port, tls, n.Nick, n.Realname, saslUser, saslPass, Now())
	}
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

// BufferKind enumerates the rows allowed in buffers.kind.
const (
	BufferChannel = "channel"
	BufferQuery   = "query"
	BufferStatus  = "status"
)

// Buffer is the DTO returned for newly-created buffers so the API can
// push a buffer_created event without a second query.
type Buffer = bufferRow

// ListNetworks returns every network row for /api/state.
func tableHasColumn(ctx context.Context, d *sql.DB, table, column string) (bool, error) {
	rows, err := d.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ListNetworks(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, host, port, tls, nick, COALESCE(realname,'')
		 FROM networks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Network
	for rows.Next() {
		var n Network
		var tls int
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &tls, &n.Nick, &n.Realname); err != nil {
			return nil, err
		}
		n.TLS = tls == 1
		out = append(out, n)
	}
	return out, rows.Err()
}

// StoredMessage mirrors the messages table for API responses.
type StoredMessage = messageRow
