package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func newEndpointsTestServer(t *testing.T) (*ircdb.MultiStore, *Server) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	return stores, &Server{
		Stores:    stores,
		Hub:       hub.New(),
		AppName:   "lurker",
		Version:   "v1.2.3",
		GitHash:   "abc123",
		BuildTime: "2024-01-01T00:00:00Z",
	}
}

func TestHealthzReturns200WhenDBReachable(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	s.healthz(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestHealthzReturns503WhenDBClosed(t *testing.T) {
	stores, s := newEndpointsTestServer(t)
	if err := stores.Control.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	s.healthz(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestWhoamiReturnsBuildMetadata(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/whoami", http.NoBody)
	s.whoami(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "lurker" || got["version"] != "v1.2.3" || got["hash"] != "abc123" || got["build_time"] != "2024-01-01T00:00:00Z" {
		t.Fatalf("body = %+v", got)
	}
}

func TestListThemesEmptyWhenLoaderNil(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/themes", http.NoBody)
	s.listThemes(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	themes, ok := got["themes"].([]any)
	if !ok {
		t.Fatalf("themes field = %T", got["themes"])
	}
	if len(themes) != 0 {
		t.Fatalf("expected empty themes, got %v", themes)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
}

func TestPatchBufferSettingsRejectsInvalidJSON(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	id := uuid.New()
	r := httptest.NewRequest(http.MethodPatch, "/api/buffers/"+id.String()+"/settings", http.NoBody)
	r.SetPathValue("id", id.String())
	s.patchBufferSettings(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPatchBufferSettingsRejectsBadBufferID(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/buffers/not-a-uuid/settings", http.NoBody)
	r.SetPathValue("id", "not-a-uuid")
	s.patchBufferSettings(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPatchBufferSettingsReturns404ForUnknownBuffer(t *testing.T) {
	_, s := newEndpointsTestServer(t)
	w := httptest.NewRecorder()
	id := uuid.New()
	r := httptest.NewRequest(http.MethodPatch, "/api/buffers/"+id.String()+"/settings", bytes.NewBufferString(`{"show_embeds":false}`))
	r.SetPathValue("id", id.String())
	s.patchBufferSettings(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", w.Code, w.Body.String())
	}
}

func TestPatchBufferSettingsPersistsAndPublishes(t *testing.T) {
	stores, s := newEndpointsTestServer(t)
	ctx := t.Context()
	n, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	bufID, _, _, err := stores.EnsureBuffer(ctx, n.ID, "#go", ircdb.BufferChannel)
	if err != nil {
		t.Fatal(err)
	}

	events, unsub := s.Hub.Subscribe(4)
	defer unsub()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/buffers/"+bufID.String()+"/settings", bytes.NewBufferString(`{"pinned":true,"show_embeds":false}`))
	r.SetPathValue("id", bufID.String())
	s.patchBufferSettings(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var resp bufferSettingsEvent
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != bufID || !resp.Pinned || resp.ShowEmbeds {
		t.Fatalf("response = %+v", resp)
	}

	select {
	case ev := <-events:
		bs, ok := ev.(bufferSettingsEvent)
		if !ok || bs.ID != bufID || !bs.Pinned {
			t.Fatalf("unexpected hub event: %#v", ev)
		}
	default:
		t.Fatal("expected hub buffer_settings event")
	}
}
