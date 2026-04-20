package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type resolvedGlobalBuffer struct {
	networkID int64
	name      string
	localID   int64
	logStore  *LogStore
}

type bufferNameMappings struct {
	localToName  map[int64]string
	nameToGlobal map[string]int64
}

// MultiStore owns the control DB plus zero or more per-network log DB handles.
type MultiStore struct {
	Control *sql.DB
	DataDir string

	mu   sync.RWMutex
	logs map[int64]*LogStore
}

// OpenMultiStore opens the control DB and any configured per-network log DBs.
func OpenMultiStore(dataDir string) (*MultiStore, error) {
	controlPath := filepath.Join(dataDir, "control.db")
	control, err := OpenControl(controlPath)
	if err != nil {
		return nil, err
	}
	ms := &MultiStore{Control: control, DataDir: dataDir, logs: map[int64]*LogStore{}}
	if err := ms.OpenConfiguredNetworks(context.Background()); err != nil {
		_ = control.Close()
		return nil, err
	}
	return ms, nil
}

// Close closes every open log DB and the control DB.
func (ms *MultiStore) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	var firstErr error
	for _, s := range ms.logs {
		if err := s.Close(); err != nil && firstErr == nil {
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
func (ms *MultiStore) LogStore(networkID int64) (*LogStore, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	store := ms.logs[networkID]
	if store == nil {
		return nil, fmt.Errorf("log store for network %d not open", networkID)
	}
	return store, nil
}

// CloseNetwork closes the log DB for a network if it is open.
func (ms *MultiStore) CloseNetwork(networkID int64) error {
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
func (ms *MultiStore) DeleteNetwork(ctx context.Context, networkID int64) error {
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
func (ms *MultiStore) NetworkIDs() []int64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	out := make([]int64, 0, len(ms.logs))
	for id := range ms.logs {
		out = append(out, id)
	}
	slices.Sort(out)
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

// UpsertBufferRegistry creates or looks up a global buffer registry row.
func (ms *MultiStore) UpsertBufferRegistry(ctx context.Context, networkID int64, name, kind string) (id int64, created bool, buf Buffer, err error) {
	return UpsertBufferRegistry(ctx, ms.Control, networkID, name, kind)
}

// LookupBuffer resolves a global buffer ID to network/name/kind.
func (ms *MultiStore) LookupBuffer(ctx context.Context, bufferID int64) (networkID int64, name, kind string, err error) {
	return LookupBufferRegistry(ctx, ms.Control, bufferID)
}

// ListAllBuffers returns every registered buffer enriched with log DB state.
func (ms *MultiStore) ListAllBuffers(ctx context.Context) ([]Buffer, error) {
	nets, err := ListNetworks(ctx, ms.Control)
	if err != nil {
		return nil, err
	}
	var out []Buffer
	for _, n := range nets {
		logStore, err := ms.LogStore(n.ID)
		if err != nil {
			return nil, err
		}
		registryRows, err := ms.Control.QueryContext(ctx,
			`SELECT id, name, kind, created_at FROM buffer_registry WHERE network_id = ? ORDER BY id`, n.ID)
		if err != nil {
			return nil, err
		}
		for registryRows.Next() {
			var b Buffer
			scanErr := registryRows.Scan(&b.ID, &b.Name, &b.Kind, &b.CreatedAt)
			if scanErr != nil {
				_ = registryRows.Close()
				return nil, scanErr
			}
			b.NetworkID = n.ID
			var joined int
			var lastSeenID int64
			if err := logStore.DB.QueryRowContext(ctx,
				`SELECT COALESCE(topic,''), joined, COALESCE(last_seen_id,0) FROM buffers WHERE name = ?`, b.Name,
			).Scan(&b.Topic, &joined, &lastSeenID); err == nil {
				b.Joined = joined == 1
				b.LastSeenID = lastSeenID
			}
			out = append(out, b)
		}
		if err := registryRows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// RecentMessages returns recent messages for a global buffer ID.
func (ms *MultiStore) RecentMessages(ctx context.Context, globalBufferID int64, limit int) ([]StoredMessage, error) {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return nil, err
	}
	msgs, err := RecentLogMessages(ctx, buf.logStore.DB, buf.networkID, buf.localID, limit)
	if err != nil {
		return nil, err
	}
	return toStoredMessages(globalBufferID, msgs), nil
}

// MessagesBefore returns messages before a given message ID.
func (ms *MultiStore) MessagesBefore(ctx context.Context, globalBufferID, before int64, limit int) ([]StoredMessage, error) {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return nil, err
	}
	msgs, err := LogMessagesBefore(ctx, buf.logStore.DB, buf.networkID, buf.localID, before, limit)
	if err != nil {
		return nil, err
	}
	return toStoredMessages(globalBufferID, msgs), nil
}

// MarkBufferLastSeen updates the last seen message ID for a global buffer.
func (ms *MultiStore) MarkBufferLastSeen(ctx context.Context, globalBufferID, lastSeenID int64) error {
	buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
	if err != nil {
		return err
	}
	return UpdateLogBufferLastSeen(ctx, buf.logStore.DB, buf.name, lastSeenID)
}

// Search searches stored messages and maps results back to global buffer IDs.
func (ms *MultiStore) Search(ctx context.Context, query string, networkID, globalBufferID int64, limit int) ([]StoredMessage, error) {
	if globalBufferID > 0 {
		buf, err := ms.resolveGlobalBuffer(ctx, globalBufferID)
		if err != nil {
			return nil, err
		}
		msgs, err := SearchLogMessages(ctx, buf.logStore.DB, buf.networkID, query, buf.localID, limit)
		if err != nil {
			return nil, err
		}
		return toStoredMessages(globalBufferID, msgs), nil
	}

	if networkID > 0 {
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

func (ms *MultiStore) searchNetwork(ctx context.Context, networkID int64, query string, limit int) ([]StoredMessage, error) {
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return nil, err
	}
	msgs, err := SearchLogMessages(ctx, logStore.DB, networkID, query, 0, limit)
	if err != nil {
		return nil, err
	}
	mappings, err := ms.bufferNameMappings(ctx, networkID, logStore)
	if err != nil {
		return nil, err
	}
	return mapSearchMessagesToGlobal(msgs, mappings)
}

func mapSearchMessagesToGlobal(msgs []LogMessage, mappings bufferNameMappings) ([]StoredMessage, error) {
	out := make([]StoredMessage, 0, len(msgs))
	for _, m := range msgs {
		name, ok := mappings.localToName[m.BufferID]
		if !ok {
			return nil, fmt.Errorf("missing local buffer mapping for network %d buffer %d", m.NetworkID, m.BufferID)
		}
		globalBufferID, ok := mappings.nameToGlobal[name]
		if !ok {
			return nil, fmt.Errorf("missing global buffer mapping for network %d buffer %q", m.NetworkID, name)
		}
		out = append(out, StoredMessage{
			ID: m.ID, NetworkID: m.NetworkID, BufferID: globalBufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out, nil
}

func (ms *MultiStore) resolveGlobalBuffer(ctx context.Context, globalBufferID int64) (resolvedGlobalBuffer, error) {
	networkID, name, _, err := ms.LookupBuffer(ctx, globalBufferID)
	if err != nil {
		return resolvedGlobalBuffer{}, err
	}
	logStore, err := ms.LogStore(networkID)
	if err != nil {
		return resolvedGlobalBuffer{}, err
	}
	var localID int64
	if err := logStore.DB.QueryRowContext(ctx, `SELECT id FROM buffers WHERE name = ?`, name).Scan(&localID); err != nil {
		return resolvedGlobalBuffer{}, err
	}
	return resolvedGlobalBuffer{networkID: networkID, name: name, localID: localID, logStore: logStore}, nil
}

func (ms *MultiStore) bufferNameMappings(ctx context.Context, networkID int64, logStore *LogStore) (bufferNameMappings, error) {
	localRows, err := logStore.DB.QueryContext(ctx, `SELECT id, name FROM buffers`)
	if err != nil {
		return bufferNameMappings{}, err
	}
	defer func() { _ = localRows.Close() }()
	localToName := map[int64]string{}
	for localRows.Next() {
		var id int64
		var name string
		scanErr := localRows.Scan(&id, &name)
		if scanErr != nil {
			return bufferNameMappings{}, scanErr
		}
		localToName[id] = name
	}
	rowsErr := localRows.Err()
	if rowsErr != nil {
		return bufferNameMappings{}, rowsErr
	}

	globalRows, err := ms.Control.QueryContext(ctx, `SELECT id, name FROM buffer_registry WHERE network_id = ?`, networkID)
	if err != nil {
		return bufferNameMappings{}, err
	}
	defer func() { _ = globalRows.Close() }()
	nameToGlobal := map[string]int64{}
	for globalRows.Next() {
		var id int64
		var name string
		if err := globalRows.Scan(&id, &name); err != nil {
			return bufferNameMappings{}, err
		}
		nameToGlobal[name] = id
	}
	if err := globalRows.Err(); err != nil {
		return bufferNameMappings{}, err
	}

	return bufferNameMappings{localToName: localToName, nameToGlobal: nameToGlobal}, nil
}

func toStoredMessages(globalBufferID int64, in []LogMessage) []StoredMessage {
	out := make([]StoredMessage, 0, len(in))
	for _, m := range in {
		out = append(out, StoredMessage{
			ID: m.ID, NetworkID: m.NetworkID, BufferID: globalBufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out
}
