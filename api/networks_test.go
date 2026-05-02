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

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/irc"
)

func TestNetworkCRUDAndDeleteRetainsLogDB(t *testing.T) {
	ctx := t.Context()
	dataDir := t.TempDir()
	stores, err := ircdb.OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

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
	if created.ID == uuid.Nil {
		t.Fatal("expected created id")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libera.db")); err != nil {
		t.Fatalf("expected log db to exist: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/networks/"+created.ID.String(), bytes.NewBufferString(`{"nick":"tester2"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/networks/"+created.ID.String(), http.NoBody)
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

func TestReorderNetworksEndpointPersistsOrder(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n1, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	n2, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "OFTC", Host: "127.0.0.2", Port: 2, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/networks/reorder", bytes.NewBufferString(fmt.Sprintf(`{"ids":[%q,%q]}`, n2.ID, n1.ID))).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d body=%s", rec.Code, rec.Body.String())
	}

	nets, err := ircdb.ListNetworks(ctx, stores.Control)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 2 || nets[0].ID != n2.ID || nets[1].ID != n1.ID {
		t.Fatalf("unexpected order after reorder: %+v", nets)
	}
}

func TestReorderNetworksEndpointRejectsInvalidPermutation(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n1, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "OFTC", Host: "127.0.0.2", Port: 2, TLS: false, Nick: "tester"}); err != nil {
		t.Fatal(err)
	}

	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	h := srv.Handler()

	otherID := uuid.New()
	for _, body := range []string{
		fmt.Sprintf(`{"ids":[%q]}`, n1.ID),
		fmt.Sprintf(`{"ids":[%q,%q]}`, n1.ID, n1.ID),
		fmt.Sprintf(`{"ids":[%q,%q]}`, n1.ID, otherID),
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/networks/reorder", bytes.NewBufferString(body)).WithContext(ctx)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("reorder invalid permutation status = %d body=%s", rec.Code, rec.Body.String())
		}
	}

}

func TestNetworkConnectCommandsEndpointsAndStateDoesNotLeak(t *testing.T) {
	ctx := t.Context()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.OpenNetwork(ctx, n); err != nil {
		t.Fatal(err)
	}
	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/networks/"+n.ID.String()+"/connect-commands", bytes.NewBufferString(`{"commands":["  PRIVMSG NickServ :IDENTIFY secret  ","","MODE tester +x"]}`)).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put commands status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body connectCommandsRequest
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Commands) != 2 || body.Commands[0] != "PRIVMSG NickServ :IDENTIFY secret" || body.Commands[1] != "MODE tester +x" {
		t.Fatalf("commands = %#v", body.Commands)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/networks/"+n.ID.String()+"/connect-commands", http.NoBody).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get commands status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("IDENTIFY secret")) {
		t.Fatalf("expected command response, body=%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/state", http.NoBody).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("state status = %d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("IDENTIFY secret")) || bytes.Contains(rec.Body.Bytes(), []byte("connect_commands")) {
		t.Fatalf("state leaked connect commands: %s", rec.Body.String())
	}
}

func TestConnectDisconnectEndpointsUpdateState(t *testing.T) {
	ctx := t.Context()

	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	n, err := ircdb.CreateNetwork(ctx, stores.Control, ircdb.Network{Name: "Libera", Host: "127.0.0.1", Port: 1, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := irc.NewManager(stores, hub.New())
	srv := &Server{Stores: stores, Hub: hub.New(), Manager: mgr}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/networks/"+n.ID.String()+"/connect", http.NoBody).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status = %d body=%s", rec.Code, rec.Body.String())
	}
	state := mgr.StateSnapshot()[n.ID]
	if state != "connecting" && state != "disconnected" {
		t.Fatalf("unexpected state after connect: %q", state)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/networks/"+n.ID.String()+"/disconnect", http.NoBody).WithContext(ctx)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := mgr.StateSnapshot()[n.ID]; got != "disconnected" {
		t.Fatalf("state after disconnect = %q, want disconnected", got)
	}
}
