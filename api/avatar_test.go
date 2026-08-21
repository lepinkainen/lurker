package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/preview"
)

// avatarStubManager satisfies both the full api.manager surface (via the
// embedded mockManager, so it can be assigned to Server.Manager) and the
// avatarManager sub-interface the avatar handler type-asserts for.
type avatarStubManager struct {
	*mockManager
	urls map[string]string
}

func newAvatarStubManager() *avatarStubManager {
	return &avatarStubManager{mockManager: newMockManager(), urls: map[string]string{}}
}

func avatarKey(networkID uuid.UUID, nick string) string {
	return networkID.String() + "|" + strings.ToLower(nick)
}

func (m *avatarStubManager) setAvatar(networkID uuid.UUID, nick, url string) {
	m.urls[avatarKey(networkID, nick)] = url
}

func (m *avatarStubManager) AvatarURL(networkID uuid.UUID, nick string) (string, bool) {
	url, ok := m.urls[avatarKey(networkID, nick)]
	return url, ok
}

var _ avatarManager = (*avatarStubManager)(nil)

// permissiveFetcher builds a preview.Fetcher whose SSRF check always
// passes, mirroring preview/fetcher_test.go's permissive() helper (which is
// unexported to the preview package) so tests can reach an httptest.Server
// on 127.0.0.1.
func permissiveFetcher() *preview.Fetcher {
	return preview.NewFetcher(preview.FetcherConfig{
		SSRFCheck: func(context.Context, string) error { return nil },
	})
}

func TestAvatarHandlerUnknownNickReturns404(t *testing.T) {
	mgr := newAvatarStubManager()
	s := &Server{Manager: mgr}

	req := httptest.NewRequest(http.MethodGet, "/api/avatar?network="+uuid.New().String()+"&nick=alice", nil)
	rec := httptest.NewRecorder()
	s.avatar(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAvatarHandlerMissingNetworkOrNickReturns400(t *testing.T) {
	mgr := newAvatarStubManager()
	s := &Server{Manager: mgr}

	cases := []string{
		"/api/avatar",
		"/api/avatar?nick=alice",
		"/api/avatar?network=" + uuid.New().String(),
	}
	for _, target := range cases {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		s.avatar(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

func TestAvatarHandlerServesKnownAvatarThroughFetcher(t *testing.T) {
	want := []byte{0x89, 'P', 'N', 'G'}
	var lastPath string
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(want)
	}))
	defer imgSrv.Close()

	mgr := newAvatarStubManager()
	networkID := uuid.New()
	mgr.setAvatar(networkID, "alice", imgSrv.URL+"/avatar-{size}.png")
	s := &Server{Manager: mgr, PreviewFetcher: permissiveFetcher()}

	req := httptest.NewRequest(http.MethodGet, "/api/avatar?network="+networkID.String()+"&nick=alice", nil)
	rec := httptest.NewRecorder()
	s.avatar(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	if rec.Body.String() != string(want) {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
	if lastPath != "/avatar-64.png" {
		t.Fatalf("fetched path = %q, want /avatar-64.png (default size, {size} substituted)", lastPath)
	}
}

func TestAvatarHandlerClampsSizeInResolvedURL(t *testing.T) {
	var lastPath string
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
	}))
	defer imgSrv.Close()

	mgr := newAvatarStubManager()
	networkID := uuid.New()
	mgr.setAvatar(networkID, "alice", imgSrv.URL+"/avatar-{size}.png")
	s := &Server{Manager: mgr, PreviewFetcher: permissiveFetcher()}

	cases := []struct {
		size, wantPath string
	}{
		{"1000", "/avatar-256.png"}, // clamps down to the largest allowed size
		{"50", "/avatar-64.png"},    // nearest allowed size
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/avatar?network="+networkID.String()+"&nick=alice&size="+tc.size, nil)
		rec := httptest.NewRecorder()
		s.avatar(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("size=%s: status = %d, want 200", tc.size, rec.Code)
		}
		if lastPath != tc.wantPath {
			t.Errorf("size=%s: fetched path = %q, want %q", tc.size, lastPath, tc.wantPath)
		}
	}
}

func TestClampAvatarSize(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", defaultAvatarSize},
		{"0", defaultAvatarSize},
		{"-5", defaultAvatarSize},
		{"not-a-number", defaultAvatarSize},
		{"16", 16},
		{"32", 32},
		{"64", 64},
		{"128", 128},
		{"256", 256},
		{"1000", 256},
		{"50", 64},
		{"100", 128},
	}
	for _, tc := range cases {
		if got := clampAvatarSize(tc.raw); got != tc.want {
			t.Errorf("clampAvatarSize(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}
