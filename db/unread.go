package db

import (
	"context"
	"database/sql"
	"fmt"
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

// BatchUnreadCandidates returns unread candidates for multiple buffers in one
// query. cutoffs maps buffer ID to last-seen message ID (uuid.Nil = no cutoff).
// limit must be > 0 and caps per-buffer row count via a window function.
func BatchUnreadCandidates(ctx context.Context, d *sql.DB, cutoffs map[uuid.UUID]uuid.UUID, limit int) (map[uuid.UUID][]UnreadCandidate, error) {
	if len(cutoffs) == 0 {
		return nil, nil
	}
	vals := make([]string, 0, len(cutoffs))
	args := make([]any, 0, len(cutoffs)*2+1)
	for bufID, cutID := range cutoffs {
		vals = append(vals, "(?, ?)")
		b := bufID
		args = append(args, b[:])
		if cutID == uuid.Nil {
			args = append(args, nil)
		} else {
			c := cutID
			args = append(args, c[:])
		}
	}
	args = append(args, int64(limit))
	q := fmt.Sprintf(`WITH cutoffs(buf_id, cut_id) AS (VALUES %s),
ranked AS (
  SELECT m.buffer_id, m.kind, m.sender, m.content,
         ROW_NUMBER() OVER (PARTITION BY m.buffer_id ORDER BY m.id ASC) AS rn
  FROM messages m
  JOIN cutoffs c ON m.buffer_id = c.buf_id
  WHERE c.cut_id IS NULL OR m.id > c.cut_id
)
SELECT buffer_id, kind, sender, content FROM ranked WHERE rn <= ?`,
		strings.Join(vals, ","))
	rows, err := d.QueryContext(ctx, q, args...)
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
