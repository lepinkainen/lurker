package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/google/uuid"
)

type resolvedGlobalBuffer struct {
	networkID uuid.UUID
	name      string
	id        uuid.UUID
	logStore  *LogStore
}

// ChannelMember is runtime member state for a channel user.
type ChannelMember struct {
	Nick   string
	Prefix string
	Away   bool
	Self   bool
}

// MultiStore owns the control DB plus zero or more per-network log DB handles.
// It also owns the shared URL-preview cache, which is deliberately a single
// database shared across every network: a link reposted between networks or
// channels resolves to one cached fetch.
type MultiStore struct {
	Control  *sql.DB
	Previews *PreviewStore
	DataDir  string

	mu   sync.RWMutex
	logs map[uuid.UUID]*LogStore
}

// OpenMultiStore opens the control DB, the shared previews DB, and any
// configured per-network log DBs.
func OpenMultiStore(dataDir string) (*MultiStore, error) {
	controlPath := filepath.Join(dataDir, "control.db")
	control, err := OpenControl(controlPath)
	if err != nil {
		return nil, err
	}
	previews, err := OpenPreviewStore(filepath.Join(dataDir, "previews.db"))
	if err != nil {
		_ = control.Close()
		return nil, err
	}
	ms := &MultiStore{Control: control, Previews: previews, DataDir: dataDir, logs: map[uuid.UUID]*LogStore{}}
	if err := ms.OpenConfiguredNetworks(context.Background()); err != nil {
		_ = previews.Close()
		_ = control.Close()
		return nil, err
	}
	return ms, nil
}

// Close closes every open log DB, the preview DB, and the control DB.
func (ms *MultiStore) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	var firstErr error
	for _, s := range ms.logs {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if ms.Previews != nil {
		if err := ms.Previews.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if ms.Control != nil {
		if err := ms.Control.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// OpenConfiguredNetworks opens log DBs for all networks in the control DB.
func (ms *MultiStore) OpenConfiguredNetworks(ctx context.Context) error {
	nets, err := ListNetworks(ctx, ms.Control)
	if err != nil {
		return err
	}
	for _, n := range nets {
		if _, err := ms.OpenNetwork(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// OpenNetwork opens or returns the log DB for a network.
func (ms *MultiStore) OpenNetwork(ctx context.Context, n Network) (*LogStore, error) {
	ms.mu.RLock()
	if existing := ms.logs[n.ID]; existing != nil {
		ms.mu.RUnlock()
		return existing, nil
	}
	ms.mu.RUnlock()

	path, err := LogDBPath(ms.DataDir, n.Name)
	if err != nil {
		return nil, err
	}
	logStore, err := OpenLogStore(n.ID, path)
	if err != nil {
		return nil, err
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if existing := ms.logs[n.ID]; existing != nil {
		_ = logStore.Close()
		return existing, nil
	}
	ms.logs[n.ID] = logStore
	return logStore, nil
}

// LogStore returns an already-open log DB for a network.
func (ms *MultiStore) LogStore(networkID uuid.UUID) (*LogStore, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	store := ms.logs[networkID]
	if store == nil {
		return nil, fmt.Errorf("log store for network %s not open", networkID.String())
	}
	return store, nil
}

// CloseNetwork closes the log DB for a network if it is open.
func (ms *MultiStore) CloseNetwork(networkID uuid.UUID) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	store := ms.logs[networkID]
	if store == nil {
		return nil
	}
	delete(ms.logs, networkID)
	return store.Close()
}

// DeleteNetwork removes a network from the control DB and closes its log DB.
func (ms *MultiStore) DeleteNetwork(ctx context.Context, networkID uuid.UUID) error {
	if _, err := GetNetwork(ctx, ms.Control, networkID); err != nil {
		return err
	}
	if err := ms.CloseNetwork(networkID); err != nil {
		return err
	}
	if err := DeleteNetwork(ctx, ms.Control, networkID); err != nil {
		return err
	}
	return nil
}

// RenameNetworkLogDB renames the on-disk per-network log DB file.
func (ms *MultiStore) RenameNetworkLogDB(oldName, newName string) error {
	oldPath, err := LogDBPath(ms.DataDir, oldName)
	if err != nil {
		return err
	}
	newPath, err := LogDBPath(ms.DataDir, newName)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(oldPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target log db already exists: %s", newPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// NetworkIDs returns sorted IDs for currently open networks.
func (ms *MultiStore) NetworkIDs() []uuid.UUID {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	out := make([]uuid.UUID, 0, len(ms.logs))
	for id := range ms.logs {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return out
}

// UpsertNetwork inserts or updates a network and ensures its log DB is open.
func (ms *MultiStore) UpsertNetwork(ctx context.Context, n Network) (Network, error) {
	nrow, err := UpsertNetwork(ctx, ms.Control, n)
	if err != nil {
		return Network{}, err
	}
	if _, err := ms.OpenNetwork(ctx, nrow); err != nil {
		return Network{}, err
	}
	return nrow, nil
}

// ErrBufferIDMismatch indicates the per-network log DB holds a buffer row
// whose UUID does not match the global registry. This should never happen in
// normal operation; it indicates external corruption or a bug.
var ErrBufferIDMismatch = errors.New("db: buffer id mismatch between registry and log DB")

// EnsureBuffer is the single authoritative path for buffer creation. It
// ensures a row exists in both buffer_registry (control DB) and buffers
// (per-network log DB) with the SAME UUID. If the log row is missing while
// the registry row exists, it self-heals by inserting with the registry id.
// If the log row exists with a different UUID, returns ErrBufferIDMismatch.
func (ms *MultiStore) EnsureBuffer(ctx context.Context, networkID uuid.UUID, name, kind string) (id uuid.UUID, created bool, buf Buffer, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return uuid.Nil, false, Buffer{}, err
	}

	// Resolve registry row by its canonical key. If absent, create a fresh
	// UUIDv7; if another writer wins the race, re-select the row it created.
	now := Now()
	buf = Buffer{NetworkID: networkID, Name: name, Kind: kind, CreatedAt: now}
	switch err := ms.Control.QueryRowContext(ctx,
		`SELECT id, kind, created_at FROM buffer_registry WHERE network_id = ? AND name = ?`,
		networkID[:], name,
	).Scan(&buf.ID, &buf.Kind, &buf.CreatedAt); {
	case err == nil:
		// Existing registry row.
	case errors.Is(err, sql.ErrNoRows):
		buf.ID = newID()
		if _, ierr := ms.Control.ExecContext(ctx,
			`INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?)`,
			buf.ID[:], networkID[:], name, kind, now,
		); ierr != nil {
			if rerr := ms.Control.QueryRowContext(ctx,
				`SELECT id, kind, created_at FROM buffer_registry WHERE network_id = ? AND name = ?`,
				networkID[:], name,
			).Scan(&buf.ID, &buf.Kind, &buf.CreatedAt); rerr != nil {
				return uuid.Nil, false, Buffer{}, ierr
			}
		} else {
			created = true
		}
	default:
		return uuid.Nil, false, Buffer{}, err
	}

	// Verify or self-heal the per-network log buffer row. Same name with a
	// different UUID is corruption: never silently preserve divergent IDs.
	var logID uuid.UUID
	var lastSeen []byte
	switch err := logStore.DB.QueryRowContext(ctx,
		`SELECT id, COALESCE(topic,''), last_seen_id, created_at FROM buffers WHERE name = ?`, name,
	).Scan(&logID, &buf.Topic, &lastSeen, &buf.CreatedAt); {
	case err == nil:
		if logID != buf.ID {
			return uuid.Nil, false, Buffer{}, fmt.Errorf("%w: name=%q registry=%s log=%s",
				ErrBufferIDMismatch, name, buf.ID, logID)
		}
		buf.LastSeenID = scanUUID(lastSeen)
	case errors.Is(err, sql.ErrNoRows):
		if _, ierr := logStore.DB.ExecContext(ctx,
			`INSERT INTO buffers(id, name, kind, created_at) VALUES (?, ?, ?, ?)`,
			buf.ID[:], name, buf.Kind, buf.CreatedAt,
		); ierr != nil {
			// Possible race: re-select and verify instead of silently diverging.
			if rerr := logStore.DB.QueryRowContext(ctx,
				`SELECT id, COALESCE(topic,''), last_seen_id, created_at FROM buffers WHERE name = ?`, name,
			).Scan(&logID, &buf.Topic, &lastSeen, &buf.CreatedAt); rerr != nil {
				return uuid.Nil, false, Buffer{}, ierr
			}
			if logID != buf.ID {
				return uuid.Nil, false, Buffer{}, fmt.Errorf("%w: name=%q registry=%s log=%s",
					ErrBufferIDMismatch, name, buf.ID, logID)
			}
			buf.LastSeenID = scanUUID(lastSeen)
		}
	case err != nil:
		return uuid.Nil, false, Buffer{}, err
	}

	settings, serr := GetBufferSettings(ctx, ms.Control, buf.ID)
	if serr == nil {
		buf.ShowEmbeds = settings.ShowEmbeds
		buf.ShowPresenceEvents = settings.ShowPresenceEvents
		buf.CollapsePresenceEvents = settings.CollapsePresenceEvents
		buf.Pinned = settings.Pinned
	} else {
		buf.ShowEmbeds = true
		buf.ShowPresenceEvents = true
	}
	return buf.ID, created, buf, nil
}

// LookupBuffer resolves a global buffer ID to network/name/kind.
func (ms *MultiStore) LookupBuffer(ctx context.Context, bufferID uuid.UUID) (networkID uuid.UUID, name, kind string, err error) {
	return LookupBufferRegistry(ctx, ms.Control, bufferID)
}

// ListAllBuffers returns every registered buffer enriched with log DB state.
func (ms *MultiStore) ListAllBuffers(ctx context.Context) ([]Buffer, error) {
	nets, err := ListNetworks(ctx, ms.Control)
	if err != nil {
		return nil, err
	}
	settings, err := ListBufferSettings(ctx, ms.Control)
	if err != nil {
		return nil, err
	}
	var out []Buffer
	for _, n := range nets {
		logStore, err := ms.LogStore(n.ID)
		if err != nil {
			return nil, err
		}
		bs, err := ms.networkBuffers(ctx, n, logStore, settings)
		if err != nil {
			return nil, err
		}
		out = append(out, bs...)
	}
	return out, nil
}

func (ms *MultiStore) networkBuffers(ctx context.Context, n Network, logStore *LogStore, settings map[uuid.UUID]BufferSettings) ([]Buffer, error) {
	registryRows, err := ms.Control.QueryContext(ctx,
		`SELECT id, name, kind, created_at FROM buffer_registry WHERE network_id = ? ORDER BY id`, n.ID[:])
	if err != nil {
		return nil, err
	}
	defer func() { _ = registryRows.Close() }()
	var out []Buffer
	for registryRows.Next() {
		var b Buffer
		if err := registryRows.Scan(&b.ID, &b.Name, &b.Kind, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.NetworkID = n.ID
		if s, ok := settings[b.ID]; ok {
			b.ShowEmbeds = s.ShowEmbeds
			b.ShowPresenceEvents = s.ShowPresenceEvents
			b.CollapsePresenceEvents = s.CollapsePresenceEvents
			b.Pinned = s.Pinned
		} else {
			b.ShowEmbeds = true
			b.ShowPresenceEvents = true
		}
		var lastSeenBytes []byte
		if err := logStore.DB.QueryRowContext(ctx,
			`SELECT COALESCE(topic,''), last_seen_id FROM buffers WHERE name = ?`, b.Name,
		).Scan(&b.Topic, &lastSeenBytes); err == nil {
			b.LastSeenID = scanUUID(lastSeenBytes)
		}
		out = append(out, b)
	}
	return out, registryRows.Err()
}

// RecentMessages returns recent messages for a global buffer ID.
func (ms *MultiStore) RecentMessages(ctx context.Context, globalBufferID uuid.UUID, limit int) ([]StoredMessage, error) {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return nil, err
	}
	msgs, err := RecentLogMessages(ctx, buf.logStore.DB, buf.id, limit)
	if err != nil {
		return nil, err
	}
	return toStoredMessages(buf.networkID, globalBufferID, msgs), nil
}

// MessagesBefore returns messages before a given message ID.
func (ms *MultiStore) MessagesBefore(ctx context.Context, globalBufferID, before uuid.UUID, limit int) ([]StoredMessage, error) {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return nil, err
	}
	msgs, err := LogMessagesBefore(ctx, buf.logStore.DB, buf.id, before, limit)
	if err != nil {
		return nil, err
	}
	return toStoredMessages(buf.networkID, globalBufferID, msgs), nil
}

// MarkBufferLastSeen updates the last seen message ID for a global buffer.
func (ms *MultiStore) MarkBufferLastSeen(ctx context.Context, globalBufferID, lastSeenID uuid.UUID) error {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return err
	}
	return UpdateLogBufferLastSeen(ctx, buf.logStore.DB, buf.name, lastSeenID)
}

// Search searches stored messages and maps results back to global buffer IDs.
func (ms *MultiStore) Search(ctx context.Context, query string, networkID, globalBufferID uuid.UUID, limit int) ([]StoredMessage, error) {
	if globalBufferID != uuid.Nil {
		buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
		if err != nil {
			return nil, err
		}
		msgs, err := SearchLogMessages(ctx, buf.logStore.DB, query, buf.id, limit)
		if err != nil {
			return nil, err
		}
		return toStoredMessages(buf.networkID, globalBufferID, msgs), nil
	}

	if networkID != uuid.Nil {
		return ms.searchNetwork(ctx, networkID, query, limit)
	}

	var out []StoredMessage
	for _, id := range ms.NetworkIDs() {
		mapped, err := ms.searchNetwork(ctx, id, query, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, mapped...)
	}
	return out, nil
}

func (ms *MultiStore) searchNetwork(ctx context.Context, networkID uuid.UUID, query string, limit int) ([]StoredMessage, error) {
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return nil, err
	}
	msgs, err := SearchLogMessages(ctx, logStore.DB, query, uuid.Nil, limit)
	if err != nil {
		return nil, err
	}
	out := make([]StoredMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, StoredMessage{
			ID: m.ID, NetworkID: networkID, BufferID: m.BufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out, nil
}

func (ms *MultiStore) resolveGlobalBuffer(ctx context.Context, globalBufferID uuid.UUID) (resolvedGlobalBuffer, error) {
	networkID, name, _, err := ms.LookupBuffer(ctx, globalBufferID)
	if err != nil {
		return resolvedGlobalBuffer{}, err
	}
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return resolvedGlobalBuffer{}, err
	}
	return resolvedGlobalBuffer{networkID: networkID, name: name, id: globalBufferID, logStore: logStore}, nil
}

func toStoredMessages(networkID, globalBufferID uuid.UUID, in []LogMessageRow) []StoredMessage {
	out := make([]StoredMessage, 0, len(in))
	for _, m := range in {
		out = append(out, StoredMessage{
			ID: m.ID, NetworkID: networkID, BufferID: globalBufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out
}
