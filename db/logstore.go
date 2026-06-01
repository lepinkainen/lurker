package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/logdb"
)

// LogStore manages one per-network log database.
type LogStore struct {
	NetworkID uuid.UUID
	DB        *sql.DB
}

// OpenLogStore opens the per-network log DB for a validated network name.
func OpenLogStore(networkID uuid.UUID, path string) (*LogStore, error) {
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
	BufferID  uuid.UUID
	MsgID     string
	Timestamp time.Time
	Sender    string
	Userhost  string
	Account   string
	Kind      string
	Target    string
	Content   string
	Raw       string
}

// InsertLogMessage inserts an IRC event into the per-network log.
func InsertLogMessage(ctx context.Context, d *sql.DB, m LogMessageInput) (id uuid.UUID, ts string, inserted bool, err error) {
	ts = FormatTime(m.Timestamp)
	newId := newID()
	affected, err := logdb.New(d).InsertLogMessage(ctx, logdb.InsertLogMessageParams{
		ID:       newId[:],
		BufferID: m.BufferID[:],
		Msgid:    nullableString(m.MsgID),
		Ts:       ts,
		Sender:   m.Sender,
		Userhost: m.Userhost,
		Account:  nullableString(m.Account),
		Kind:     m.Kind,
		Target:   nullableString(m.Target),
		Content:  m.Content,
		Raw:      m.Raw,
	})
	if err != nil {
		return uuid.Nil, ts, false, err
	}
	if affected == 0 {
		return uuid.Nil, ts, false, nil
	}
	return newId, ts, true, nil
}

// ListLogBuffers returns every buffer stored in a per-network log DB.
func ListLogBuffers(ctx context.Context, d *sql.DB) ([]LogBufferRow, error) {
	rows, err := logdb.New(d).ListLogBuffers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]logBufferRow, 0, len(rows))
	for _, r := range rows {
		id, err := parseUUID(r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, logBufferRow{
			ID:         id,
			Name:       r.Name,
			Kind:       r.Kind,
			Topic:      r.Topic,
			LastSeenID: scanUUID(r.LastSeenID),
			CreatedAt:  r.CreatedAt,
		})
	}
	return out, nil
}

// RecentLogMessages returns recent messages for a buffer in ascending order.
func RecentLogMessages(ctx context.Context, d *sql.DB, bufferID uuid.UUID, limit int) ([]LogMessageRow, error) {
	rows, err := logdb.New(d).RecentLogMessages(ctx, logdb.RecentLogMessagesParams{
		BufferID: bufferID[:],
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return recentRowsToMessages(rows), nil
}

// LogMessagesBefore returns messages before a given message ID.
func LogMessagesBefore(ctx context.Context, d *sql.DB, bufferID, before uuid.UUID, limit int) ([]LogMessageRow, error) {
	rows, err := logdb.New(d).LogMessagesBefore(ctx, logdb.LogMessagesBeforeParams{
		BufferID: bufferID[:],
		ID:       before[:],
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return beforeRowsToMessages(rows), nil
}

// LookupLogBuffer returns the name and kind for a local log buffer.
func LookupLogBuffer(ctx context.Context, d *sql.DB, bufferID uuid.UUID) (name, kind string, err error) {
	row, err := logdb.New(d).LookupLogBuffer(ctx, bufferID[:])
	if err != nil {
		return "", "", err
	}
	return row.Name, row.Kind, nil
}

func recentRowsToMessages(rows []logdb.RecentLogMessagesRow) []logMessageRow {
	out := make([]logMessageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, logMessageRow{
			ID:       scanUUID(r.ID),
			BufferID: scanUUID(r.BufferID),
			MsgID:    r.Msgid,
			TS:       r.Ts,
			Sender:   r.Sender,
			Userhost: r.Userhost,
			Account:  r.Account,
			Kind:     r.Kind,
			Target:   r.Target,
			Content:  r.Content,
		})
	}
	return out
}

func beforeRowsToMessages(rows []logdb.LogMessagesBeforeRow) []logMessageRow {
	out := make([]logMessageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, logMessageRow{
			ID:       scanUUID(r.ID),
			BufferID: scanUUID(r.BufferID),
			MsgID:    r.Msgid,
			TS:       r.Ts,
			Sender:   r.Sender,
			Userhost: r.Userhost,
			Account:  r.Account,
			Kind:     r.Kind,
			Target:   r.Target,
			Content:  r.Content,
		})
	}
	return out
}

// EnsureStatusBuffer ensures the synthetic status buffer exists for a network.
func EnsureStatusBuffer(ctx context.Context, store *MultiStore, networkID uuid.UUID) (uuid.UUID, error) {
	id, _, _, err := store.EnsureBuffer(ctx, networkID, "", BufferStatus)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// UpdateLogBufferTopic updates the topic for a buffer.
func UpdateLogBufferTopic(ctx context.Context, d *sql.DB, name, topic string) error {
	return logdb.New(d).UpdateLogBufferTopic(ctx, logdb.UpdateLogBufferTopicParams{
		Topic: nullableString(topic),
		Name:  name,
	})
}

// UpdateLogBufferLastSeen updates the last seen message ID for a buffer.
func UpdateLogBufferLastSeen(ctx context.Context, d *sql.DB, name string, lastSeenID uuid.UUID) error {
	var idBytes []byte
	if lastSeenID != uuid.Nil {
		idBytes = lastSeenID[:]
	}
	return logdb.New(d).UpdateLogBufferLastSeen(ctx, logdb.UpdateLogBufferLastSeenParams{
		LastSeenID: idBytes,
		Name:       name,
	})
}

// SearchLogMessages searches a per-network log DB using FTS. Returns messages
// joined to their FTS rowid match. The internal rowid is not exposed.
//
// Kept on raw database/sql because sqlc cannot parse `messages_fts MATCH ?`
// against the FTS5 virtual table.
func SearchLogMessages(ctx context.Context, d *sql.DB, query string, bufferID uuid.UUID, limit int) ([]LogMessageRow, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT m.id, m.buffer_id, COALESCE(m.msgid,''), m.ts, m.sender, COALESCE(m.userhost,''),
		     COALESCE(m.account,''), m.kind, COALESCE(m.target,''), m.content
	      FROM messages_fts f
	      JOIN messages m ON m.rowid = f.rowid`
	args := []any{query}
	where := ` WHERE messages_fts MATCH ?`
	if bufferID != uuid.Nil {
		where += ` AND m.buffer_id = ?`
		args = append(args, bufferID[:])
	}
	q += where + ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, limit)
	return logMessagesQuery(ctx, d, q, args...)
}

func logMessagesQuery(ctx context.Context, d *sql.DB, q string, args ...any) ([]LogMessageRow, error) {
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanLogMessageRows(rows, func(m *logMessageRow) error {
		return rows.Scan(&m.ID, &m.BufferID, &m.MsgID, &m.TS,
			&m.Sender, &m.Userhost, &m.Account, &m.Kind, &m.Target, &m.Content)
	})
}

func (s *LogStore) String() string {
	return fmt.Sprintf("LogStore(network_id=%s)", s.NetworkID.String())
}

// MessagePreviewLink is one (message_id, url) association in a per-network log.
type MessagePreviewLink struct {
	MessageID uuid.UUID
	URL       string
	Position  int
}

// InsertMessagePreviewLinks upserts (message_id, url, position) rows. No-op
// on empty input.
//
// Kept on raw database/sql + tx.PrepareContext so the INSERT statement is
// prepared once and reused across the batch. sqlc's emit_prepared_queries
// flag is off (cleaner generated code), so the generated wrapper would
// issue ExecContext per row. For preview-link bursts on IRC message floods
// the per-row prepare overhead is noticeable.
func InsertMessagePreviewLinks(ctx context.Context, d *sql.DB, links []MessagePreviewLink) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO message_previews(message_id, url, position)
		 VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, l := range links {
		mid := l.MessageID
		if _, err := stmt.ExecContext(ctx, mid[:], l.URL, l.Position); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListMessagePreviewLinks returns (message_id, url, position) rows for the
// given message IDs ordered by (message_id, position).
//
// Kept on raw database/sql because the IN-clause arity is dynamic.
func ListMessagePreviewLinks(ctx context.Context, d *sql.DB, messageIDs []uuid.UUID) ([]MessagePreviewLink, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id[:]
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
