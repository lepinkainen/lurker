package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ircdb "github.com/lepinkainen/lurker/db"
)

func TestClassifyRemoteAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		wantClass  tailscaleClass
		wantIP     string
	}{
		{"loopback v4", "127.0.0.1:54321", classLocal, "127.0.0.1"},
		{"loopback v6", "[::1]:54321", classLocal, "::1"},
		{"tailscale cgnat", "100.101.102.103:443", classTailscale, "100.101.102.103"},
		{"tailscale cgnat low", "100.64.0.1:1", classTailscale, "100.64.0.1"},
		{"tailscale cgnat high", "100.127.255.255:1", classTailscale, "100.127.255.255"},
		{"tailscale v6 ula", "[fd7a:115c:a1e0::1]:443", classTailscale, "fd7a:115c:a1e0::1"},
		{"lan rfc1918", "192.168.1.10:443", classExternal, "192.168.1.10"},
		{"public", "8.8.8.8:443", classExternal, "8.8.8.8"},
		{"just outside cgnat low", "100.63.255.255:1", classExternal, "100.63.255.255"},
		{"just outside cgnat high", "100.128.0.0:1", classExternal, "100.128.0.0"},
		{"unparseable", "garbage", classUnknown, "garbage"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cls, ip := classifyRemoteAddr(tc.remoteAddr)
			if cls != tc.wantClass {
				t.Errorf("class = %q, want %q", cls, tc.wantClass)
			}
			if ip != tc.wantIP {
				t.Errorf("ip = %q, want %q", ip, tc.wantIP)
			}
		})
	}
}

func TestTailscaleStatusEndpoint(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	srv := &Server{Stores: stores}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tailscale-status", http.NoBody)
	req.RemoteAddr = "100.101.102.103:54321"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != string(classTailscale) {
		t.Errorf("status = %q, want %q", got["status"], classTailscale)
	}
	if got["remote_ip"] != "100.101.102.103" {
		t.Errorf("remote_ip = %q", got["remote_ip"])
	}
}
