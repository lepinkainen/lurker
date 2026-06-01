package db

import (
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
