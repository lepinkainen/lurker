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
	"github.com/lepinkainen/lurker/db/internal/controldb"
	"github.com/lepinkainen/lurker/db/internal/logdb"
)

type resolvedGlobalBuffer struct {
	networkID uuid.UUID
	name      string
	id        uuid.UUID
	logStore  *LogStore
}

// MultiStore owns the control DB plus zero or more per-network log DB handles.
// It also owns the shared URL-preview cache, which is deliberately a single
// database shared across every network: a link reposted between networks or
// channels resolves to one cached fetch.
type MultiStore struct {
	Control  *sql.DB
	Previews *PreviewStore
	Media    *MediaStore
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
	mediaStore, err := OpenMediaStore(filepath.Join(dataDir, "media.db"))
	if err != nil {
		_ = previews.Close()
		_ = control.Close()
		return nil, err
	}
	ms := &MultiStore{Control: control, Previews: previews, Media: mediaStore, DataDir: dataDir, logs: map[uuid.UUID]*LogStore{}}
	if err := ms.OpenConfiguredNetworks(context.Background()); err != nil {
		_ = mediaStore.Close()
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
	if ms.Media != nil {
		if err := ms.Media.Close(); err != nil && firstErr == nil {
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

// DeleteBuffer permanently removes a buffer: its message history and log-DB
// row, then its registry row (buffer_settings cascades). The log DB is
// cleared first: the two SQLite files cannot share a transaction, and a crash
// after the log delete merely leaves an empty-but-visible buffer that can be
// deleted again. The reverse order would leave orphaned history that
// EnsureBuffer's adopt-existing-id logic silently resurrects on the next
// join/PM with the same name.
func (ms *MultiStore) DeleteBuffer(ctx context.Context, bufferID uuid.UUID) error {
	networkID, _, kind, err := ms.LookupBuffer(ctx, bufferID)
	if err != nil {
		return err
	}
	if kind == BufferStatus {
		return fmt.Errorf("db: cannot delete status buffer %s", bufferID)
	}
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return err
	}
	tx, err := logStore.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := logdb.New(tx)
	if err := q.DeleteLogMessagesForBuffer(ctx, bufferID[:]); err != nil {
		return err
	}
	if err := q.DeleteLogBuffer(ctx, bufferID[:]); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return controldb.New(ms.Control).DeleteBufferRegistry(ctx, bufferID[:])
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

// ErrMessageNotFound indicates a mark-read referenced a message id that does
// not exist in the target buffer (fabricated, cross-buffer, or not yet
// persisted). The read position is left untouched.
var ErrMessageNotFound = errors.New("db: message not found in buffer")

// EnsureBuffer is the single authoritative path for buffer creation. It
// ensures a row exists in both buffer_registry (control DB) and buffers
// (per-network log DB) with the SAME UUID. If the log row is missing while
// the registry row exists, it self-heals by inserting with the registry id.
//
// The per-network log DB is the durable source of message history, so when the
// registry row is absent but a log row for the same name already exists (the
// deleted-then-recreated-network case: deleting a network cascades away its
// buffer_registry rows but intentionally keeps the log DB file), the new
// registry row adopts the surviving log row's UUID instead of minting a fresh
// one. This reconnects the recreated network to its preserved history.
//
// The genuine-corruption path is unchanged: if BOTH a registry row and a log
// row exist with divergent UUIDs, EnsureBuffer returns ErrBufferIDMismatch.
func (ms *MultiStore) EnsureBuffer(ctx context.Context, networkID uuid.UUID, name, kind string) (id uuid.UUID, created bool, buf Buffer, err error) {
	name, kind = normalizeBufferIdentity(name, kind)
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return uuid.Nil, false, Buffer{}, err
	}

	// Peek at the log DB first so a registry insert can adopt the surviving
	// log-row UUID rather than minting a fresh one that would then collide.
	adoptedID, adopt, err := ms.peekLogBufferID(ctx, logStore, name)
	if err != nil {
		return uuid.Nil, false, Buffer{}, err
	}

	now := Now()
	buf = Buffer{NetworkID: networkID, Name: name, Kind: kind, CreatedAt: now}
	if created, err = ms.ensureBufferRegistryRow(ctx, networkID, name, kind, now, adoptedID, adopt, &buf); err != nil {
		return uuid.Nil, false, Buffer{}, err
	}
	if err := ms.ensureBufferLogRow(ctx, logStore, name, &buf); err != nil {
		return uuid.Nil, false, Buffer{}, err
	}

	applyBufferSettings(ctx, ms.Control, &buf)
	return buf.ID, created, buf, nil
}

// peekLogBufferID returns the UUID of an existing per-network log buffer row by
// name. found is false (with nil error) when no such row exists.
func (ms *MultiStore) peekLogBufferID(ctx context.Context, logStore *LogStore, name string) (id uuid.UUID, found bool, err error) {
	return LookupLogBufferIDByName(ctx, logStore.DB, name)
}

// ensureBufferRegistryRow looks up the registry row by (network_id, name). When
// absent it inserts a new row: it adopts adoptedID (the surviving log-row UUID)
// when adopt is true, otherwise mints a fresh UUIDv7. If another writer wins the
// insert race, it re-selects to adopt the winning ID.
func (ms *MultiStore) ensureBufferRegistryRow(ctx context.Context, networkID uuid.UUID, name, kind, now string, adoptedID uuid.UUID, adopt bool, buf *Buffer) (bool, error) {
	q := controldb.New(ms.Control)
	lookup := controldb.LookupBufferRegistryByNameParams{NetworkID: networkID[:], Name: name}
	row, err := q.LookupBufferRegistryByName(ctx, lookup)
	switch {
	case err == nil:
		id, perr := parseUUID(row.ID)
		if perr != nil {
			return false, perr
		}
		buf.ID = id
		buf.Kind = row.Kind
		buf.CreatedAt = row.CreatedAt
		buf.SortOrder = row.SortOrder
		return false, nil
	case errors.Is(err, sql.ErrNoRows):
		if adopt {
			buf.ID = adoptedID
		} else {
			buf.ID = newID()
		}
		// New channels append to the end of any manual order; 0 (alphabetical
		// tie) while the network's order is untouched. Queries/status stay 0.
		var sortOrder int64
		if kind == BufferChannel {
			if sortOrder, err = q.NextChannelSortOrder(ctx, networkID[:]); err != nil {
				return false, err
			}
		}
		ierr := q.InsertBufferRegistry(ctx, controldb.InsertBufferRegistryParams{
			ID:        buf.ID[:],
			NetworkID: networkID[:],
			Name:      name,
			Kind:      kind,
			CreatedAt: now,
			SortOrder: sortOrder,
		})
		if ierr == nil {
			buf.SortOrder = sortOrder
			return true, nil
		}
		row2, rerr := q.LookupBufferRegistryByName(ctx, lookup)
		if rerr != nil {
			return false, ierr
		}
		id, perr := parseUUID(row2.ID)
		if perr != nil {
			return false, perr
		}
		buf.ID = id
		buf.Kind = row2.Kind
		buf.CreatedAt = row2.CreatedAt
		buf.SortOrder = row2.SortOrder
		return false, nil
	default:
		return false, err
	}
}

// ensureBufferLogRow verifies or inserts the per-network log row, matching
// the registry ID. Same name with a different UUID is corruption: never
// silently preserve divergent IDs.
func (ms *MultiStore) ensureBufferLogRow(ctx context.Context, logStore *LogStore, name string, buf *Buffer) error {
	q := logdb.New(logStore.DB)
	row, err := q.LookupLogBufferByName(ctx, name)
	switch {
	case err == nil:
		logID, perr := parseUUID(row.ID)
		if perr != nil {
			return perr
		}
		if logID != buf.ID {
			return fmt.Errorf("%w: name=%q registry=%s log=%s",
				ErrBufferIDMismatch, name, buf.ID, logID)
		}
		buf.Topic = row.Topic
		buf.LastSeenID = scanUUID(row.LastSeenID)
		buf.CreatedAt = row.CreatedAt
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ms.insertBufferLogRow(ctx, logStore, name, buf)
	default:
		return err
	}
}

func (ms *MultiStore) insertBufferLogRow(ctx context.Context, logStore *LogStore, name string, buf *Buffer) error {
	q := logdb.New(logStore.DB)
	ierr := q.InsertLogBuffer(ctx, logdb.InsertLogBufferParams{
		ID:        buf.ID[:],
		Name:      name,
		Kind:      buf.Kind,
		CreatedAt: buf.CreatedAt,
	})
	if ierr == nil {
		return nil
	}
	row, rerr := q.LookupLogBufferByName(ctx, name)
	if rerr != nil {
		return ierr
	}
	logID, perr := parseUUID(row.ID)
	if perr != nil {
		return perr
	}
	if logID != buf.ID {
		return fmt.Errorf("%w: name=%q registry=%s log=%s",
			ErrBufferIDMismatch, name, buf.ID, logID)
	}
	buf.Topic = row.Topic
	buf.LastSeenID = scanUUID(row.LastSeenID)
	buf.CreatedAt = row.CreatedAt
	return nil
}

func applyBufferSettings(ctx context.Context, control *sql.DB, buf *Buffer) {
	settings, err := GetBufferSettings(ctx, control, buf.ID)
	if err != nil {
		buf.ShowEmbeds = true
		buf.ShowPresenceEvents = true
	} else {
		buf.ShowEmbeds = settings.ShowEmbeds
		buf.ShowPresenceEvents = settings.ShowPresenceEvents
		buf.CollapsePresenceEvents = settings.CollapsePresenceEvents
		buf.Pinned = settings.Pinned
		buf.Archived = settings.Archived
		buf.PinOrder = settings.PinOrder
	}
	// Status windows default link previews off and can't be toggled
	// (UpdateBufferSettings rejects them), so they never carry a settings row.
	if buf.Kind == BufferStatus {
		buf.ShowEmbeds = false
	}
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
	rows, err := controldb.New(ms.Control).ListBufferRegistryForNetwork(ctx, n.ID[:])
	if err != nil {
		return nil, err
	}
	logBufs, err := ListLogBuffers(ctx, logStore.DB)
	if err != nil {
		return nil, err
	}
	logByName := make(map[string]logBufferRow, len(logBufs))
	for _, lb := range logBufs {
		logByName[lb.Name] = lb
	}
	out := make([]Buffer, 0, len(rows))
	for _, r := range rows {
		id, err := parseUUID(r.ID)
		if err != nil {
			return nil, err
		}
		b := Buffer{
			ID:        id,
			NetworkID: n.ID,
			Name:      r.Name,
			Kind:      r.Kind,
			CreatedAt: r.CreatedAt,
			SortOrder: r.SortOrder,
		}
		if s, ok := settings[b.ID]; ok {
			b.ShowEmbeds = s.ShowEmbeds
			b.ShowPresenceEvents = s.ShowPresenceEvents
			b.CollapsePresenceEvents = s.CollapsePresenceEvents
			b.Pinned = s.Pinned
			b.Archived = s.Archived
			b.PinOrder = s.PinOrder
		} else {
			b.ShowEmbeds = true
			b.ShowPresenceEvents = true
		}
		if b.Kind == BufferStatus {
			b.ShowEmbeds = false
		}
		if lb, ok := logByName[b.Name]; ok {
			b.Topic = lb.Topic
			b.TopicSetBy = lb.TopicSetBy
			b.TopicSetAt = lb.TopicSetAt
			b.LastSeenID = lb.LastSeenID
		}
		out = append(out, b)
	}
	return out, nil
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

// MarkBufferLastSeen advances the last seen message ID for a global buffer
// and returns the effective read position afterwards. The message must exist
// in that buffer (ErrMessageNotFound otherwise), and the position never
// regresses: a stale ack from a racing client is a no-op and the newer stored
// position is returned, so callers broadcast the winning state.
func (ms *MultiStore) MarkBufferLastSeen(ctx context.Context, globalBufferID, lastSeenID uuid.UUID) (uuid.UUID, error) {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return uuid.Nil, err
	}
	ok, err := LogMessageInBuffer(ctx, buf.logStore.DB, globalBufferID, lastSeenID)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: %s in buffer %s", ErrMessageNotFound, lastSeenID, buf.name)
	}
	if err := UpdateLogBufferLastSeen(ctx, buf.logStore.DB, buf.name, lastSeenID); err != nil {
		return uuid.Nil, err
	}
	return LogBufferLastSeen(ctx, buf.logStore.DB, buf.name)
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
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Userhost: m.Userhost, Account: m.Account,
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
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Userhost: m.Userhost, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out
}

// BatchRecentMessages returns the last limit messages for each of the given
// buffer IDs in a single per-network log DB query, keyed by buffer ID.
func (ms *MultiStore) BatchRecentMessages(ctx context.Context, networkID uuid.UUID, bufferIDs []uuid.UUID, limit int) (map[uuid.UUID][]StoredMessage, error) {
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return nil, err
	}
	byBuf, err := BatchRecentLogMessages(ctx, logStore.DB, bufferIDs, limit)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]StoredMessage, len(byBuf))
	for bufID, msgs := range byBuf {
		stored := make([]StoredMessage, 0, len(msgs))
		for _, m := range msgs {
			stored = append(stored, StoredMessage{
				ID: m.ID, NetworkID: networkID, BufferID: bufID,
				MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Userhost: m.Userhost, Account: m.Account,
				Kind: m.Kind, Target: m.Target, Content: m.Content,
			})
		}
		out[bufID] = stored
	}
	return out, nil
}

// BatchUnreadCandidates returns unread candidates for multiple buffers in a
// single per-network log DB query. cutoffs maps buffer ID to last-seen message
// ID (uuid.Nil = no cutoff). limit caps per-buffer row count; must be > 0.
func (ms *MultiStore) BatchUnreadCandidates(ctx context.Context, networkID uuid.UUID, cutoffs map[uuid.UUID]uuid.UUID, limit int) (map[uuid.UUID][]UnreadCandidate, error) {
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return nil, err
	}
	return BatchUnreadCandidates(ctx, logStore.DB, cutoffs, limit)
}
