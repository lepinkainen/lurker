package db

import (
	"context"
	"database/sql"
)

type bufferRegistryRow struct {
	ID        int64
	NetworkID int64
	Name      string
	Kind      string
	CreatedAt string
}

type logBufferRow struct {
	ID         int64
	Name       string
	Kind       string
	Topic      string
	Joined     bool
	LastSeenID int64
	CreatedAt  string
}

type logMessageRow struct {
	ID       int64
	BufferID int64
	MsgID    string
	TS       string
	Sender   string
	Account  string
	Kind     string
	Target   string
	Content  string
}

type logMessageInsert struct {
	BufferID int64
	MsgID    string
	TS       string
	Sender   string
	Account  string
	Kind     string
	Target   string
	Content  string
	Raw      string
}

func normalizeBufferIdentity(name, kind string) (normalizedName, normalizedKind string) {
	if name == "" {
		return "*status*", BufferStatus
	}
	return name, kind
}

func nullableString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

func scanLogBufferRows(rows *sql.Rows, scan func(*logBufferRow) error) ([]logBufferRow, error) {
	defer func() { _ = rows.Close() }()
	var out []logBufferRow
	for rows.Next() {
		var row logBufferRow
		if err := scan(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func scanLogMessageRows(rows *sql.Rows, scan func(*logMessageRow) error) ([]logMessageRow, error) {
	defer func() { _ = rows.Close() }()
	var out []logMessageRow
	for rows.Next() {
		var row logMessageRow
		if err := scan(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func upsertBufferRegistryOrLogRow[T any](ctx context.Context, d *sql.DB, selectQuery string, selectArgs []any, insertQuery string, insertArgs []any, row T, setID func(*T, int64)) (id int64, created bool, out T, err error) {
	err = d.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&id)
	if err == nil {
		return id, false, out, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, out, err
	}
	res, err := d.ExecContext(ctx, insertQuery, insertArgs...)
	if err != nil {
		if err2 := d.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&id); err2 == nil {
			return id, false, out, nil
		}
		return 0, false, out, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, out, err
	}
	out = row
	setID(&out, id)
	return id, true, out, nil
}

func insertMessageRow(ctx context.Context, d *sql.DB, query string, args []any, ts string) (id int64, storedTS string, inserted bool, err error) {
	res, err := d.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, ts, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, ts, false, err
	}
	if affected == 0 {
		return 0, ts, false, nil
	}
	id, err = res.LastInsertId()
	return id, ts, true, err
}
