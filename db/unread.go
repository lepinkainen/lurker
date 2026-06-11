package db

import (
	"context"
	"database/sql"
	"strings"

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

// BatchUnreadCandidates returns unread candidates for multiple buffers in a
// single round-trip. Each buffer's subquery uses ORDER BY id ASC LIMIT so the
// index stops early rather than ranking full unread history. cutoffs maps
// buffer ID to last-seen message ID (uuid.Nil = no cutoff). limit must be > 0.
func BatchUnreadCandidates(ctx context.Context, d *sql.DB, cutoffs map[uuid.UUID]uuid.UUID, limit int) (map[uuid.UUID][]UnreadCandidate, error) {
	if len(cutoffs) == 0 {
		return map[uuid.UUID][]UnreadCandidate{}, nil
	}
	parts := make([]string, 0, len(cutoffs))
	args := make([]any, 0, len(cutoffs)*3)
	for bufID, cutID := range cutoffs {
		b := bufID
		if cutID == uuid.Nil {
			parts = append(parts, `SELECT buffer_id, kind, sender, content
			  FROM (SELECT buffer_id, kind, sender, content FROM messages
			        WHERE buffer_id = ? ORDER BY id ASC LIMIT ?)`)
			args = append(args, b[:], int64(limit))
		} else {
			c := cutID
			parts = append(parts, `SELECT buffer_id, kind, sender, content
			  FROM (SELECT buffer_id, kind, sender, content FROM messages
			        WHERE buffer_id = ? AND id > ? ORDER BY id ASC LIMIT ?)`)
			args = append(args, b[:], c[:], int64(limit))
		}
	}
	rows, err := d.QueryContext(ctx, strings.Join(parts, "\nUNION ALL\n"), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[uuid.UUID][]UnreadCandidate, len(cutoffs))
	for rows.Next() {
		var bufB []byte
		var c UnreadCandidate
		if err := rows.Scan(&bufB, &c.Kind, &c.Sender, &c.Content); err != nil {
			return nil, err
		}
		bufID := scanUUID(bufB)
		out[bufID] = append(out[bufID], c)
	}
	return out, rows.Err()
}
