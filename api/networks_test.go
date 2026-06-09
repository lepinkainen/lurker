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

func newNetworkTestServer(t *testing.T) (*ircdb.MultiStore, http.Handler) {
	t.Helper()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	return stores, srv.Handler()
}

func createTestNetwork(t *testing.T, stores *ircdb.MultiStore, name string) ircdb.Network {
	t.Helper()
	n, err := ircdb.CreateNetwork(t.Context(), stores.Control, ircdb.Network{Name: name, Host: "127.0.0.1", Port: 6697, TLS: false, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stores.OpenNetwork(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	return n
}

func doNetworkRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var reqBody *bytes.Buffer
	if body == "" {
		reqBody = bytes.NewBuffer(nil)
	} else {
		reqBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	h.ServeHTTP(rec, req)
	return rec
}

// newNetworkTestServerInDir is like newNetworkTestServer but exposes the data
// directory so log-DB file retention can be asserted.
func newNetworkTestServerInDir(t *testing.T) (string, *ircdb.MultiStore, http.Handler) {
	t.Helper()
	dataDir := t.TempDir()
	stores, err := ircdb.OpenMultiStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	srv := &Server{Stores: stores, Hub: hub.New(), Manager: irc.NewManager(stores, hub.New())}
	return dataDir, stores, srv.Handler()
}

func createNetworkViaAPI(t *testing.T, h http.Handler, body string) networkDTO {
	t.Helper()
	rec := doNetworkRequest(h, http.MethodPost, "/api/networks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created networkDTO
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestNetworkCreate(t *testing.T) {
	dataDir, _, h := newNetworkTestServerInDir(t)
	created := createNetworkViaAPI(t, h, `{"name":"Libera","host":"irc.libera.chat","port":6697,"tls":true,"nick":"tester"}`)
	if created.ID == uuid.Nil {
		t.Fatal("expected created id")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libera.db")); err != nil {
		t.Fatalf("expected log db to exist: %v", err)
	}
}

func TestNetworkPatch(t *testing.T) {
	_, stores, h := newNetworkTestServerInDir(t)
	created := createNetworkViaAPI(t, h, `{"name":"Libera","host":"irc.libera.chat","port":6697,"tls":true,"nick":"tester"}`)

	rec := doNetworkRequest(h, http.MethodPatch, "/api/networks/"+created.ID.String(), `{"nick":"tester2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := ircdb.GetNetwork(t.Context(), stores.Control, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Nick != "tester2" {
		t.Fatalf("nick = %q, want tester2", updated.Nick)
	}
}

func TestNetworkDelete(t *testing.T) {
	ctx := t.Context()
	_, stores, h := newNetworkTestServerInDir(t)
	created := createNetworkViaAPI(t, h, `{"name":"Libera","host":"irc.libera.chat","port":6697,"tls":true,"nick":"tester"}`)

	rec := doNetworkRequest(h, http.MethodDelete, "/api/networks/"+created.ID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := ircdb.GetNetwork(ctx, stores.Control, created.ID); err == nil {
		t.Fatal("expected control db network row deleted")
	}
}

func TestNetworkDeleteRetainsLogDB(t *testing.T) {
	dataDir, _, h := newNetworkTestServerInDir(t)
	created := createNetworkViaAPI(t, h, `{"name":"Libera","host":"irc.libera.chat","port":6697,"tls":true,"nick":"tester"}`)

	rec := doNetworkRequest(h, http.MethodDelete, "/api/networks/"+created.ID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "libera.db")); err != nil {
		t.Fatalf("expected retained log db after network delete: %v", err)
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

func TestNetworkConnectCommandsPut(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	rec := doNetworkRequest(h, http.MethodPut, "/api/networks/"+n.ID.String()+"/connect-commands",
		`{"commands":["  PRIVMSG NickServ :IDENTIFY secret  ","","MODE tester +x"]}`)
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
}

func TestNetworkConnectCommandsGet(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	if rec := doNetworkRequest(h, http.MethodPut, "/api/networks/"+n.ID.String()+"/connect-commands",
		`{"commands":["PRIVMSG NickServ :IDENTIFY secret"]}`); rec.Code != http.StatusOK {
		t.Fatalf("put commands status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec := doNetworkRequest(h, http.MethodGet, "/api/networks/"+n.ID.String()+"/connect-commands", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get commands status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("IDENTIFY secret")) {
		t.Fatalf("expected command response, body=%s", rec.Body.String())
	}
}

func TestNetworkConnectCommandsStateDoesNotLeak(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	if rec := doNetworkRequest(h, http.MethodPut, "/api/networks/"+n.ID.String()+"/connect-commands",
		`{"commands":["PRIVMSG NickServ :IDENTIFY secret"]}`); rec.Code != http.StatusOK {
		t.Fatalf("put commands status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec := doNetworkRequest(h, http.MethodGet, "/api/state", "")
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

func TestCreateNetworkEndpointValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "missing name", body: `{"host":"irc.example.test","port":6697,"nick":"tester"}`},
		{name: "missing host", body: `{"name":"Libera","port":6697,"nick":"tester"}`},
		{name: "missing port", body: `{"name":"Libera","host":"irc.example.test","nick":"tester"}`},
		{name: "missing nick", body: `{"name":"Libera","host":"irc.example.test","port":6697}`},
		{name: "invalid name", body: `{"name":"bad name","host":"irc.example.test","port":6697,"nick":"tester"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h := newNetworkTestServer(t)
			rec := doNetworkRequest(h, http.MethodPost, "/api/networks", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateNetworkEndpointPersistsConnectCommands(t *testing.T) {
	stores, h := newNetworkTestServer(t)

	rec := doNetworkRequest(h, http.MethodPost, "/api/networks", `{"name":"Libera","host":"irc.example.test","port":6697,"nick":"tester","connect_commands":["  MODE tester +x  ",""]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created networkDTO
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	commands, err := ircdb.ListNetworkConnectCommands(t.Context(), stores.Control, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "MODE tester +x" {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestCreateNetworkEndpointRejectsDuplicateName(t *testing.T) {
	_, h := newNetworkTestServer(t)

	rec := doNetworkRequest(h, http.MethodPost, "/api/networks", `{"name":"Libera","host":"irc.example.test","port":6697,"tls":false,"nick":"tester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("initial create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doNetworkRequest(h, http.MethodPost, "/api/networks", `{"name":"libera","host":"irc2.example.test","port":6697,"nick":"tester2"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate create status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchNetworkEndpointValidation(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")
	missingID := uuid.New()

	for _, tc := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "bad uuid", path: "/api/networks/not-a-uuid", body: `{"nick":"newnick"}`, want: http.StatusBadRequest},
		{name: "missing network", path: "/api/networks/" + missingID.String(), body: `{"nick":"newnick"}`, want: http.StatusNotFound},
		{name: "invalid json", path: "/api/networks/" + n.ID.String(), body: `{`, want: http.StatusBadRequest},
		{name: "invalid name", path: "/api/networks/" + n.ID.String(), body: `{"name":"bad name"}`, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doNetworkRequest(h, http.MethodPatch, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestPatchNetworkEndpointPartialUpdatePreservesOtherFields(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	rec := doNetworkRequest(h, http.MethodPatch, "/api/networks/"+n.ID.String(), `{"nick":"tester2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := ircdb.GetNetwork(t.Context(), stores.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Nick != "tester2" || updated.Name != n.Name || updated.Host != n.Host || updated.Port != n.Port || updated.TLS != n.TLS {
		t.Fatalf("updated network = %+v, original = %+v", updated, n)
	}

	rec = doNetworkRequest(h, http.MethodPatch, "/api/networks/"+n.ID.String(), `{"tls":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tls-only patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err = ircdb.GetNetwork(t.Context(), stores.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.TLS || updated.Nick != "tester2" || updated.Name != n.Name || updated.Host != n.Host || updated.Port != n.Port {
		t.Fatalf("updated network = %+v", updated)
	}
}

func TestPatchNetworkEndpointRejectsDuplicateRename(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")
	_ = createTestNetwork(t, stores, "OFTC")

	rec := doNetworkRequest(h, http.MethodPatch, "/api/networks/"+n.ID.String(), `{"name":"oftc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate rename status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchNetworkEndpointDisabledAndConnectCommandsOnly(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	rec := doNetworkRequest(h, http.MethodPatch, "/api/networks/"+n.ID.String(), `{"disabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := ircdb.GetNetwork(t.Context(), stores.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Disabled || updated.Name != n.Name || updated.Nick != n.Nick {
		t.Fatalf("updated network = %+v, original = %+v", updated, n)
	}

	rec = doNetworkRequest(h, http.MethodPatch, "/api/networks/"+n.ID.String(), `{"connect_commands":["  MODE tester +x  ",""]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("commands-only patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	commands, err := ircdb.ListNetworkConnectCommands(t.Context(), stores.Control, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "MODE tester +x" {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestDeleteNetworkEndpointEdges(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{name: "bad uuid", path: "/api/networks/not-a-uuid", want: http.StatusBadRequest},
		{name: "missing network", path: "/api/networks/" + uuid.New().String(), want: http.StatusNotFound},
		{name: "existing network", path: "/api/networks/" + n.ID.String(), want: http.StatusNoContent},
		{name: "already deleted", path: "/api/networks/" + n.ID.String(), want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doNetworkRequest(h, http.MethodDelete, tc.path, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	rec := doNetworkRequest(h, http.MethodPost, "/api/networks", `{"name":"Libera","host":"irc.example.test","port":6697,"nick":"tester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recreate status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConnectDisconnectEndpointEdges(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")

	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "connect bad uuid", method: http.MethodPost, path: "/api/networks/not-a-uuid/connect", want: http.StatusBadRequest},
		{name: "connect missing", method: http.MethodPost, path: "/api/networks/" + uuid.New().String() + "/connect", want: http.StatusNotFound},
		{name: "disconnect bad uuid", method: http.MethodPost, path: "/api/networks/not-a-uuid/disconnect", want: http.StatusBadRequest},
		{name: "disconnect unknown state", method: http.MethodPost, path: "/api/networks/" + uuid.New().String() + "/disconnect", want: http.StatusOK},
		{name: "connect existing", method: http.MethodPost, path: "/api/networks/" + n.ID.String() + "/connect", want: http.StatusOK},
		{name: "disconnect existing", method: http.MethodPost, path: "/api/networks/" + n.ID.String() + "/disconnect", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doNetworkRequest(h, tc.method, tc.path, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestNetworkConnectCommandsEndpointValidation(t *testing.T) {
	stores, h := newNetworkTestServer(t)
	n := createTestNetwork(t, stores, "Libera")
	missingID := uuid.New()

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "get bad uuid", method: http.MethodGet, path: "/api/networks/not-a-uuid/connect-commands", want: http.StatusBadRequest},
		{name: "get missing", method: http.MethodGet, path: "/api/networks/" + missingID.String() + "/connect-commands", want: http.StatusNotFound},
		{name: "put bad uuid", method: http.MethodPut, path: "/api/networks/not-a-uuid/connect-commands", body: `{"commands":[]}`, want: http.StatusBadRequest},
		{name: "put invalid json", method: http.MethodPut, path: "/api/networks/" + n.ID.String() + "/connect-commands", body: `{`, want: http.StatusBadRequest},
		{name: "put missing", method: http.MethodPut, path: "/api/networks/" + missingID.String() + "/connect-commands", body: `{"commands":[]}`, want: http.StatusNotFound},
		{name: "put empty", method: http.MethodPut, path: "/api/networks/" + n.ID.String() + "/connect-commands", body: `{"commands":[]}`, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doNetworkRequest(h, tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestReorderNetworksEndpointValidation(t *testing.T) {
	_, h := newNetworkTestServer(t)

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "empty ids", body: `{"ids":[]}`},
		{name: "malformed uuid", body: `{"ids":["not-a-uuid"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doNetworkRequest(h, http.MethodPost, "/api/networks/reorder", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
