package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	ircdb "github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/hub"
	"github.com/lepinkainen/research/irc-service/irc"
)

func TestNetworkCRUDAndDeleteRetainsLogDB(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()
	stores, err := ircdb.OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	h := srv.Handler()

	body := bytes.NewBufferString(`{"name":"Libera","host":"irc.libera.chat","port":6697,"tls":true,"nick":"tester"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/networks", body)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created networkDTO
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected created id")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libera.db")); err != nil {
		t.Fatalf("expected log db to exist: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/networks/"+fmt.Sprintf("%d", created.ID), bytes.NewBufferString(`{"nick":"tester2"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/networks/"+fmt.Sprintf("%d", created.ID), nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libera.db")); err != nil {
		t.Fatalf("expected retained log db: %v", err)
	}
	if _, err := ircdb.GetNetwork(ctx, stores.Control, created.ID); err == nil {
		t.Fatal("expected control db network row deleted")
	}
}

func TestConnectDisconnectEndpointsUpdateState(t *testing.T) {
	ctx := t.Context()

	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	n, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := irc.NewManager(stores, hub.New())
	srv := &Server{Stores: stores, Hub: hub.New(), Manager: mgr}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/networks/"+fmt.Sprintf("%d", n.ID)+"/connect", nil).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status = %d body=%s", rec.Code, rec.Body.String())
	}
	state := mgr.StateSnapshot()[n.ID]
	if state != "connecting" && state != "disconnected" {
		t.Fatalf("unexpected state after connect: %q", state)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/networks/"+fmt.Sprintf("%d", n.ID)+"/disconnect", nil).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := mgr.StateSnapshot()[n.ID]; got != "disconnected" {
		t.Fatalf("state after disconnect = %q, want disconnected", got)
	}
}
