package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LogStore manages one per-network log database.
type LogStore struct {
	NetworkID int64
	DB        *sql.DB
}

// OpenLogStore opens the per-network log DB for a validated network name.
func OpenLogStore(networkID int64, path string) (*LogStore, error) {
	d, err := OpenLog(path)
	if err != nil {
		return nil, err
	}
	return &LogStore{NetworkID: networkID, DB: d}, nil
}

// Close closes the underlying SQLite handle.
func (s *LogStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// LogBufferRow mirrors local per-network buffers table.
type LogBufferRow = logBufferRow

// LogMessageRow mirrors local per-network messages table.
type LogMessageRow = logMessageRow

// LogMessageInput is an inbound IRC event prepared for per-network storage.
type LogMessageInput struct {
	BufferID  int64
	MsgID     string
	Timestamp time.Time
	Sender    string
	Account   string
	Kind      string
	Target    string
	Content   string
	Raw       string
}

// UpsertLogBuffer creates or looks up a per-network buffer row.
func UpsertLogBuffer(ctx context.Context, d *sql.DB, networkID int64, name, kind string) (id int64, created bool, buf LogBufferRow, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	now := Now()
	joined := 0
	if kind == BufferChannel {
		joined = 1
	}
	return upsertBufferRegistryOrLogRow(
		ctx,
		d,
		`SELECT id FROM buffers WHERE name = ?`,
		[]any{name},
		`INSERT INTO buffers(name, kind, joined, created_at) VALUES (?, ?, ?, ?)`,
		[]any{name, kind, joined, now},
		logBufferRow{Name: name, Kind: kind, Joined: joined == 1, CreatedAt: now},
		func(row *logBufferRow, id int64) { row.ID = id },
	)
}

// InsertLogMessage inserts an IRC event into the per-network log.
func InsertLogMessage(ctx context.Context, d *sql.DB, m LogMessageInput) (id int64, ts string, inserted bool, err error) {
	ts = FormatTime(m.Timestamp)
	insert := logMessageInsert{
		BufferID: m.BufferID,
		MsgID:    m.MsgID,
		TS:       ts,
		Sender:   m.Sender,
		Account:  m.Account,
		Kind:     m.Kind,
		Target:   m.Target,
		Content:  m.Content,
		Raw:      m.Raw,
	}
	return insertMessageRow(ctx, d,
		`INSERT OR IGNORE INTO messages
		   (buffer_id, msgid, ts, sender, account, kind, target, content, raw)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{insert.BufferID, nullableString(insert.MsgID), insert.TS, insert.Sender, nullableString(insert.Account), insert.Kind, nullableString(insert.Target), insert.Content, insert.Raw},
		ts,
	)
}

// ListLogBuffers returns every buffer stored in a per-network log DB.
func ListLogBuffers(ctx context.Context, d *sql.DB) ([]LogBufferRow, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, kind, COALESCE(topic,''), joined, COALESCE(last_seen_id,0), created_at
		 FROM buffers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	scanned, err := scanLogBufferRows(rows, func(b *logBufferRow) error {
		var joined int
		scanErr := rows.Scan(&b.ID, &b.Name, &b.Kind, &b.Topic, &joined, &b.LastSeenID, &b.CreatedAt)
		if scanErr != nil {
			return scanErr
		}
		b.Joined = joined == 1
		return nil
	})
	if err != nil {
		return nil, err
	}
	return scanned, nil
}

// RecentLogMessages returns recent messages for a buffer in ascending order.
func RecentLogMessages(ctx context.Context, d *sql.DB, bufferID int64, limit int) ([]LogMessageRow, error) {
	return logMessagesQuery(ctx, d,
		`SELECT id, buffer_id, COALESCE(msgid,''), ts, sender,
		        COALESCE(account,''), kind, COALESCE(target,''), content
		 FROM (
		   SELECT * FROM messages WHERE buffer_id = ? ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`,
		bufferID, limit)
}

// LogMessagesBefore returns messages before a given message ID.
func LogMessagesBefore(ctx context.Context, d *sql.DB, bufferID, before int64, limit int) ([]LogMessageRow, error) {
	return logMessagesQuery(ctx, d,
		`SELECT id, buffer_id, COALESCE(msgid,''), ts, sender,
		        COALESCE(account,''), kind, COALESCE(target,''), content
		 FROM (
		   SELECT * FROM messages WHERE buffer_id = ? AND id < ?
		   ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`,
		bufferID, before, limit)
}

// LookupLogBuffer returns the name and kind for a local log buffer.
func LookupLogBuffer(ctx context.Context, d *sql.DB, bufferID int64) (name, kind string, err error) {
	err = d.QueryRowContext(ctx,
		`SELECT name, kind FROM buffers WHERE id = ?`, bufferID,
	).Scan(&name, &kind)
	return
}

func logMessagesQuery(ctx context.Context, d *sql.DB, q string, args ...any) ([]LogMessageRow, error) {
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanLogMessageRows(rows, func(m *logMessageRow) error {
		return rows.Scan(&m.ID, &m.BufferID, &m.MsgID, &m.TS,
			&m.Sender, &m.Account, &m.Kind, &m.Target, &m.Content)
	})
}

// EnsureStatusBuffer ensures the synthetic status buffer exists for a network.
func EnsureStatusBuffer(ctx context.Context, store *MultiStore, networkID int64) (int64, error) {
	logStore, err := store.LogStore(networkID)
	if err != nil {
		return 0, err
	}
	registryID, _, _, err := store.UpsertBufferRegistry(ctx, networkID, "", BufferStatus)
	if err != nil {
		return 0, err
	}
	_, _, _, err = UpsertLogBuffer(ctx, logStore.DB, networkID, "", BufferStatus)
	if err != nil {
		return 0, err
	}
	return registryID, nil
}

// UpdateLogBufferJoined updates the joined state for a channel buffer.
func UpdateLogBufferJoined(ctx context.Context, d *sql.DB, name string, joined bool) error {
	joinedInt := 0
	if joined {
		joinedInt = 1
	}
	_, err := d.ExecContext(ctx, `UPDATE buffers SET joined = ? WHERE name = ?`, joinedInt, name)
	return err
}

// UpdateLogBufferTopic updates the topic for a buffer.
func UpdateLogBufferTopic(ctx context.Context, d *sql.DB, name, topic string) error {
	_, err := d.ExecContext(ctx, `UPDATE buffers SET topic = ? WHERE name = ?`, topic, name)
	return err
}

// UpdateLogBufferLastSeen updates the last seen message ID for a buffer.
func UpdateLogBufferLastSeen(ctx context.Context, d *sql.DB, name string, lastSeenID int64) error {
	_, err := d.ExecContext(ctx, `UPDATE buffers SET last_seen_id = ? WHERE name = ?`, lastSeenID, name)
	return err
}

// SearchLogMessages searches a per-network log DB using FTS.
func SearchLogMessages(ctx context.Context, d *sql.DB, query string, bufferID int64, limit int) ([]LogMessageRow, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT m.id, m.buffer_id, COALESCE(m.msgid,''), m.ts, m.sender,
		     COALESCE(m.account,''), m.kind, COALESCE(m.target,''), m.content
	      FROM messages_fts f
	      JOIN messages m ON m.id = f.rowid`
	args := []any{query}
	where := ` WHERE messages_fts MATCH ?`
	if bufferID > 0 {
		where += ` AND m.buffer_id = ?`
		args = append(args, bufferID)
	}
	q += where + ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, limit)
	return logMessagesQuery(ctx, d, q, args...)
}

func (s *LogStore) String() string {
	return fmt.Sprintf("LogStore(network_id=%d)", s.NetworkID)
}

// MessagePreviewLink is one (message_id, url) association in a per-network log.
type MessagePreviewLink struct {
	MessageID int64
	URL       string
	Position  int
}

// InsertMessagePreviewLinks upserts (message_id, url, position) rows. No-op
// on empty input.
func InsertMessagePreviewLinks(ctx context.Context, d *sql.DB, links []MessagePreviewLink) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO message_previews(message_id, url, position)
		 VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, l := range links {
		if _, err := stmt.ExecContext(ctx, l.MessageID, l.URL, l.Position); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
	}
	_ = stmt.Close()
	return tx.Commit()
}

// ListMessagePreviewLinks returns (message_id, url, position) rows for the
// given message IDs ordered by (message_id, position).
func ListMessagePreviewLinks(ctx context.Context, d *sql.DB, messageIDs []int64) ([]MessagePreviewLink, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT message_id, url, position FROM message_previews
		 WHERE message_id IN (%s) ORDER BY message_id, position`,
		strings.Join(placeholders, ","))
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MessagePreviewLink
	for rows.Next() {
		var l MessagePreviewLink
		if err := rows.Scan(&l.MessageID, &l.URL, &l.Position); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
