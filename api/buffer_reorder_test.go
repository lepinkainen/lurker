package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/irc"
)

func newBufferReorderTestServer(t *testing.T) (*ircdb.MultiStore, *hub.Hub, http.Handler, ircdb.Network) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	n, err := ircdb.CreateNetwork(t.Context(), stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.OpenNetwork(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	h := hub.New()
	srv := &Server{Stores: stores, Hub: h, Manager: irc.NewManager(t.Context(), stores, hub.New())}
	return stores, h, srv.Handler(), n
}

func ensureChannel(t *testing.T, stores *ircdb.MultiStore, networkID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id, _, _, err := stores.EnsureBuffer(t.Context(), networkID, name, ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func postBufferReorder(h http.Handler, networkID string, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/networks/"+networkID+"/buffers/reorder", bytes.NewBufferString(body))
	h.ServeHTTP(rec, req)
	return rec
}

func channelOrder(t *testing.T, stores *ircdb.MultiStore, networkID uuid.UUID) map[uuid.UUID]int64 {
	t.Helper()
	bufs, err := stores.ListAllBuffers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out := map[uuid.UUID]int64{}
	for _, b := range bufs {
		if b.NetworkID == networkID && b.Kind == ircdb.BufferChannel {
			out[b.ID] = b.SortOrder
		}
	}
	return out
}

func TestReorderNetworkBuffersPersistsOrder(t *testing.T) {
	stores, h, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")
	c := ensureChannel(t, stores, n.ID, "#gamma")

	events, _, unsub := h.Subscribe(8)
	defer unsub()

	rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q,%q]}`, c, a, b))
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d body=%s", rec.Code, rec.Body.String())
	}

	orders := channelOrder(t, stores, n.ID)
	if orders[c] != 0 || orders[a] != 1 || orders[b] != 2 {
		t.Fatalf("unexpected sort orders: %v", orders)
	}

	ev := <-events
	re, ok := ev.(bufferReorderEvent)
	if !ok {
		t.Fatalf("expected bufferReorderEvent, got %T", ev)
	}
	if re.Type != "buffer_reorder" || re.NetworkID != n.ID || len(re.Buffers) != 3 {
		t.Fatalf("unexpected event: %+v", re)
	}
}

func TestReorderNetworkBuffersPartialSetAppendsOmitted(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")
	c := ensureChannel(t, stores, n.ID, "#gamma")

	// Seed #beta at the head so a later partial set that omits it would leave
	// it colliding with the new head if omitted channels were left untouched.
	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q,%q]}`, b, a, c)); rec.Code != http.StatusOK {
		t.Fatalf("seed reorder status = %d", rec.Code)
	}
	// Partial reorder of two channels; the submitted ids form the prefix and
	// #beta is renumbered to the tail rather than keeping its stale 0.
	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q]}`, a, c)); rec.Code != http.StatusOK {
		t.Fatalf("partial reorder status = %d", rec.Code)
	}
	orders := channelOrder(t, stores, n.ID)
	if orders[a] != 0 || orders[c] != 1 || orders[b] != 2 {
		t.Fatalf("unexpected sort orders after partial reorder: %v", orders)
	}
}

func TestReorderNetworkBuffersKeepsOrdersDenseAndUnique(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")
	c := ensureChannel(t, stores, n.ID, "#gamma")
	d := ensureChannel(t, stores, n.ID, "#delta")

	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q,%q,%q]}`, d, c, b, a)); rec.Code != http.StatusOK {
		t.Fatalf("seed reorder status = %d", rec.Code)
	}
	// Every partial set must still leave a dense, collision-free 0..n-1 over
	// all of the network's channels.
	for _, ids := range [][]uuid.UUID{{a}, {c, a}, {b, d, a}, {a, b, c, d}} {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("%q", id)
		}
		body := fmt.Sprintf(`{"ids":[%s]}`, strings.Join(parts, ","))
		if rec := postBufferReorder(handler, n.ID.String(), body); rec.Code != http.StatusOK {
			t.Fatalf("reorder %s status = %d", body, rec.Code)
		}
		orders := channelOrder(t, stores, n.ID)
		if len(orders) != 4 {
			t.Fatalf("expected 4 channels, got %v", orders)
		}
		seen := map[int64]bool{}
		for id, order := range orders {
			if order < 0 || order > 3 {
				t.Fatalf("after %s: channel %s order %d out of range: %v", body, id, order, orders)
			}
			if seen[order] {
				t.Fatalf("after %s: duplicate sort_order %d: %v", body, order, orders)
			}
			seen[order] = true
		}
		// The submitted ids are the ordered prefix.
		for want, id := range ids {
			if orders[id] != int64(want) {
				t.Fatalf("after %s: channel %s order = %d, want %d", body, id, orders[id], want)
			}
		}
	}
}

// Regression: archiving a channel, reordering the remaining visible channels,
// then unarchiving must not drop the channel back into the middle of the list.
func TestReorderNetworkBuffersArchivedChannelReappearsAtTail(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")
	c := ensureChannel(t, stores, n.ID, "#gamma")

	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q,%q]}`, a, b, c)); rec.Code != http.StatusOK {
		t.Fatalf("seed reorder status = %d", rec.Code)
	}

	archived := true
	if _, err := ircdb.UpdateBufferSettings(t.Context(), stores.Control, b, ircdb.BufferSettingsPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	// A client reorders only the channels it renders, omitting the archived one.
	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q]}`, c, a)); rec.Code != http.StatusOK {
		t.Fatalf("visible reorder status = %d", rec.Code)
	}

	archived = false
	if _, err := ircdb.UpdateBufferSettings(t.Context(), stores.Control, b, ircdb.BufferSettingsPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	orders := channelOrder(t, stores, n.ID)
	if orders[c] != 0 || orders[a] != 1 || orders[b] != 2 {
		t.Fatalf("unarchived channel did not land at the tail: %v", orders)
	}
}

func TestReorderNetworkBuffersRejectsInvalid(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	q, _, _, err := stores.EnsureBuffer(t.Context(), n.ID, "alice", ircdb.BufferQuery)
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"empty":      `{"ids":[]}`,
		"duplicate":  fmt.Sprintf(`{"ids":[%q,%q]}`, a, a),
		"foreign id": fmt.Sprintf(`{"ids":[%q]}`, uuid.New()),
		"query kind": fmt.Sprintf(`{"ids":[%q]}`, q),
	} {
		if rec := postBufferReorder(handler, n.ID.String(), body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body=%s", name, rec.Code, rec.Body.String())
		}
	}

	if rec := postBufferReorder(handler, n.ID.String(), "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d", rec.Code)
	}
}

func TestReorderNetworkBuffersUnknownNetwork(t *testing.T) {
	_, _, handler, _ := newBufferReorderTestServer(t)
	rec := postBufferReorder(handler, uuid.New().String(), `{"ids":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown network status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewChannelAppendsToManualOrder(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")

	// Untouched network: new channels keep sort_order 0 (alphabetical default).
	if orders := channelOrder(t, stores, n.ID); orders[a] != 0 || orders[b] != 0 {
		t.Fatalf("expected all-zero sort orders before manual reorder: %v", orders)
	}

	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q]}`, b, a)); rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d", rec.Code)
	}

	// Manual order exists: a newly joined channel appends after it.
	c := ensureChannel(t, stores, n.ID, "#aaa-new")
	orders := channelOrder(t, stores, n.ID)
	if orders[b] != 0 || orders[a] != 1 || orders[c] != 2 {
		t.Fatalf("expected new channel appended to manual order: %v", orders)
	}

	// Queries stay at 0 regardless.
	q, _, buf, err := stores.EnsureBuffer(t.Context(), n.ID, "someone", ircdb.BufferQuery)
	if err != nil {
		t.Fatal(err)
	}
	if buf.SortOrder != 0 {
		t.Fatalf("query buffer %s sort_order = %d, want 0", q, buf.SortOrder)
	}
}

func TestStateExposesBufferSortOrder(t *testing.T) {
	stores, _, handler, n := newBufferReorderTestServer(t)
	a := ensureChannel(t, stores, n.ID, "#alpha")
	b := ensureChannel(t, stores, n.ID, "#beta")

	if rec := postBufferReorder(handler, n.ID.String(), fmt.Sprintf(`{"ids":[%q,%q]}`, b, a)); rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d", rec.Code)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("state status = %d", rec.Code)
	}
	var state stateDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]int64{}
	for _, buf := range state.Buffers {
		got[buf.ID] = buf.SortOrder
	}
	if got[b] != 0 || got[a] != 1 {
		t.Fatalf("state sort orders = %v", got)
	}
}
