package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// UnreadCandidate is a minimal projection of a message used to compute
// unread/mention counts without loading full message rows.
type UnreadCandidate struct {
	Kind    string
	Sender  string
	Content string
}

// UnreadCandidates returns the kind/sender/content of every message in a
// per-network log buffer with id > lastSeenID, capped at limit. limit <= 0
// means no cap. Used by /api/state to compute unread + mention counts
// server-side from the full stored history.
func UnreadCandidates(ctx context.Context, d *sql.DB, bufferID uuid.UUID, lastSeenID uuid.UUID, limit int) ([]UnreadCandidate, error) {
	q := `SELECT kind, sender, content FROM messages WHERE buffer_id = ?`
	args := []any{bufferID[:]}
	if lastSeenID != uuid.Nil {
		q += ` AND id > ?`
		args = append(args, lastSeenID[:])
	}
	q += ` ORDER BY id ASC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []UnreadCandidate
	for rows.Next() {
		var c UnreadCandidate
		if err := rows.Scan(&c.Kind, &c.Sender, &c.Content); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
