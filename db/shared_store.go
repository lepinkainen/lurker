package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

type logBufferRow struct {
	ID         uuid.UUID
	Name       string
	Kind       string
	Topic      string
	LastSeenID uuid.UUID
	CreatedAt  string
}

type logMessageRow struct {
	ID       uuid.UUID
	BufferID uuid.UUID
	MsgID    string
	TS       string
	Sender   string
	Userhost string
	Account  string
	Kind     string
	Target   string
	Content  string
}

type logMessageInsert struct {
	ID       uuid.UUID
	BufferID uuid.UUID
	MsgID    string
	TS       string
	Sender   string
	Userhost string
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

// insertMessageRow executes the provided INSERT (which must include a BLOB id
// column). On affected=0 (duplicate msgid), returns Nil and inserted=false.
// Otherwise returns the caller-provided id.
func insertMessageRow(ctx context.Context, d *sql.DB, query string, args []any, ts string, id uuid.UUID) (resolved uuid.UUID, storedTS string, inserted bool, err error) {
	res, err := d.ExecContext(ctx, query, args...)
	if err != nil {
		return uuid.Nil, ts, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return uuid.Nil, ts, false, err
	}
	if affected == 0 {
		return uuid.Nil, ts, false, nil
	}
	return id, ts, true, nil
}
