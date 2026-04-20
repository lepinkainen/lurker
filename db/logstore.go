package db

import (
	"context"
	"database/sql"
	"fmt"
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

func (s *LogStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

// LogBuffer is a network-local buffer row plus its owning global network id.
type LogBuffer = bufferRow

// LogMessage mirrors the per-network messages table while keeping the owning
// global network id attached for API responses.
type LogMessage = messageRow

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

func UpsertLogBuffer(ctx context.Context, d *sql.DB, networkID int64, name, kind string) (id int64, created bool, buf LogBuffer, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	now := Now()
	joined := 0
	if kind == BufferChannel {
		joined = 1
	}
	return upsertBufferRow(
		ctx,
		d,
		`SELECT id FROM buffers WHERE name = ?`,
		[]any{name},
		`INSERT INTO buffers(name, kind, joined, created_at) VALUES (?, ?, ?, ?)`,
		[]any{name, kind, joined, now},
		bufferRow{NetworkID: networkID, Name: name, Kind: kind, Joined: joined == 1, CreatedAt: now},
	)
}

func InsertLogMessage(ctx context.Context, d *sql.DB, m LogMessageInput) (id int64, ts string, inserted bool, err error) {
	ts = FormatTime(m.Timestamp)
	insert := messageInsert{
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

func ListLogBuffers(ctx context.Context, d *sql.DB, networkID int64) ([]LogBuffer, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, name, kind, COALESCE(topic,''), joined, COALESCE(last_seen_id,0), created_at
		 FROM buffers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	scanned, err := scanBufferRows(rows, func(b *bufferRow) error {
		var joined int
		if err := rows.Scan(&b.ID, &b.Name, &b.Kind, &b.Topic, &joined, &b.LastSeenID, &b.CreatedAt); err != nil {
			return err
		}
		b.NetworkID = networkID
		b.Joined = joined == 1
		return nil
	})
	if err != nil {
		return nil, err
	}
	return scanned, nil
}

func RecentLogMessages(ctx context.Context, d *sql.DB, networkID, bufferID int64, limit int) ([]LogMessage, error) {
	return logMessagesQuery(ctx, d, networkID,
		`SELECT id, buffer_id, COALESCE(msgid,''), ts, sender,
		        COALESCE(account,''), kind, COALESCE(target,''), content
		 FROM (
		   SELECT * FROM messages WHERE buffer_id = ? ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`,
		bufferID, limit)
}

func LogMessagesBefore(ctx context.Context, d *sql.DB, networkID, bufferID, before int64, limit int) ([]LogMessage, error) {
	return logMessagesQuery(ctx, d, networkID,
		`SELECT id, buffer_id, COALESCE(msgid,''), ts, sender,
		        COALESCE(account,''), kind, COALESCE(target,''), content
		 FROM (
		   SELECT * FROM messages WHERE buffer_id = ? AND id < ?
		   ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`,
		bufferID, before, limit)
}

func LookupLogBuffer(ctx context.Context, d *sql.DB, bufferID int64) (name, kind string, err error) {
	err = d.QueryRowContext(ctx,
		`SELECT name, kind FROM buffers WHERE id = ?`, bufferID,
	).Scan(&name, &kind)
	return
}

func logMessagesQuery(ctx context.Context, d *sql.DB, networkID int64, q string, args ...any) ([]LogMessage, error) {
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanMessageRows(rows, func(m *messageRow) error {
		if err := rows.Scan(&m.ID, &m.BufferID, &m.MsgID, &m.TS,
			&m.Sender, &m.Account, &m.Kind, &m.Target, &m.Content); err != nil {
			return err
		}
		m.NetworkID = networkID
		return nil
	})
}

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

func UpdateLogBufferJoined(ctx context.Context, d *sql.DB, name string, joined bool) error {
	joinedInt := 0
	if joined {
		joinedInt = 1
	}
	_, err := d.ExecContext(ctx, `UPDATE buffers SET joined = ? WHERE name = ?`, joinedInt, name)
	return err
}

func UpdateLogBufferTopic(ctx context.Context, d *sql.DB, name, topic string) error {
	_, err := d.ExecContext(ctx, `UPDATE buffers SET topic = ? WHERE name = ?`, topic, name)
	return err
}

func UpdateLogBufferLastSeen(ctx context.Context, d *sql.DB, name string, lastSeenID int64) error {
	_, err := d.ExecContext(ctx, `UPDATE buffers SET last_seen_id = ? WHERE name = ?`, lastSeenID, name)
	return err
}

func SearchLogMessages(ctx context.Context, d *sql.DB, networkID int64, query string, bufferID int64, limit int) ([]LogMessage, error) {
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
	return logMessagesQuery(ctx, d, networkID, q, args...)
}

func (s *LogStore) String() string {
	return fmt.Sprintf("LogStore(network_id=%d)", s.NetworkID)
}
