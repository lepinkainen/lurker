package db

import (
	"context"
	"database/sql"
)

type bufferRow struct {
	ID         int64
	NetworkID  int64
	Name       string
	Kind       string
	Topic      string
	Joined     bool
	LastSeenID int64
	CreatedAt  string
}

type messageRow struct {
	ID        int64
	NetworkID int64
	BufferID  int64
	MsgID     string
	TS        string
	Sender    string
	Account   string
	Kind      string
	Target    string
	Content   string
}

type messageInsert struct {
	NetworkID int64
	BufferID  int64
	MsgID     string
	TS        string
	Sender    string
	Account   string
	Kind      string
	Target    string
	Content   string
	Raw       string
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

func scanBufferRows(rows *sql.Rows, scan func(*bufferRow) error) ([]bufferRow, error) {
	defer func() { _ = rows.Close() }()
	var out []bufferRow
	for rows.Next() {
		var row bufferRow
		if err := scan(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func scanMessageRows(rows *sql.Rows, scan func(*messageRow) error) ([]messageRow, error) {
	defer func() { _ = rows.Close() }()
	var out []messageRow
	for rows.Next() {
		var row messageRow
		if err := scan(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func upsertBufferRow(ctx context.Context, d *sql.DB, selectQuery string, selectArgs []any, insertQuery string, insertArgs []any, row bufferRow) (id int64, created bool, out bufferRow, err error) {
	err = d.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&id)
	if err == nil {
		return id, false, bufferRow{}, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, bufferRow{}, err
	}
	res, err := d.ExecContext(ctx, insertQuery, insertArgs...)
	if err != nil {
		if err2 := d.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&id); err2 == nil {
			return id, false, bufferRow{}, nil
		}
		return 0, false, bufferRow{}, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, false, bufferRow{}, err
	}
	row.ID = id
	return id, true, row, nil
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
