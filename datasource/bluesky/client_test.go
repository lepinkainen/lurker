package bluesky

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lepinkainen/lurker/internal/httpjson"
)

func TestParseXRPCErrorCode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"error":"ExpiredToken","message":"Token has expired"}`, "ExpiredToken"},
		{`{"message":"no error field"}`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := parseXRPCErrorCode([]byte(c.raw)); got != c.want {
			t.Errorf("parseXRPCErrorCode(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func xrpcBody(code string) []byte {
	return fmt.Appendf(nil, `{"error":%q}`, code)
}

func TestIsExpiredAuthErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"401 unauthorized", &httpjson.Error{Status: 401}, true},
		{"400 ExpiredToken", &httpjson.Error{Status: 400, Body: xrpcBody("ExpiredToken")}, true},
		{"400 InvalidToken", &httpjson.Error{Status: 400, Body: xrpcBody("InvalidToken")}, true},
		{"400 AuthenticationRequired", &httpjson.Error{Status: 400, Body: xrpcBody("AuthenticationRequired")}, true},
		{"400 other code", &httpjson.Error{Status: 400, Body: xrpcBody("InvalidRequest")}, false},
		{"500", &httpjson.Error{Status: 500, Body: xrpcBody("ExpiredToken")}, false},
		{"plain error", errors.New("dial tcp: refused"), false},
		{"wrapped httpError", errOpWrap(&httpjson.Error{Status: 401}), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isExpiredAuthErr(c.err); got != c.want {
				t.Fatalf("isExpiredAuthErr = %v, want %v", got, c.want)
			}
		})
	}
}

func errOpWrap(err error) error {
	return &wrapErr{err}
}

type wrapErr struct{ inner error }

func (w *wrapErr) Error() string { return "op: " + w.inner.Error() }
func (w *wrapErr) Unwrap() error { return w.inner }

// fakePDS is a scripted XRPC server. Each request is logged; per-NSID
// handlers control the responses.
type fakePDS struct {
	t  *testing.T
	mu sync.Mutex
	// requests logs "<nsid> bearer=<token>" per call, in order.
	requests []string
	handlers map[string]http.HandlerFunc
}

func newFakePDS(t *testing.T) (*fakePDS, *httptest.Server) {
	t.Helper()
	f := &fakePDS{t: t, handlers: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nsid := strings.TrimPrefix(r.URL.Path, "/xrpc/")
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		f.requests = append(f.requests, nsid+" bearer="+bearer)
		h := f.handlers[nsid]
		f.mu.Unlock()
		if h == nil {
			http.Error(w, `{"error":"MethodNotImplemented"}`, http.StatusNotFound)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakePDS) handle(nsid string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[nsid] = h
}

func (f *fakePDS) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func sessionHandler(access, refresh string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, SessionResponse{AccessJwt: access, RefreshJwt: refresh, DID: "did:plc:abc", Handle: "tester.bsky.social"})
	}
}

func xrpcError(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
	}
}

func TestLoginStoresSession(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", func(w http.ResponseWriter, r *http.Request) {
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode createSession body: %v", err)
		}
		if req.Identifier != "tester.bsky.social" || req.Password != "app-pass" {
			t.Errorf("credentials = %+v", req)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("createSession must not send Authorization")
		}
		sessionHandler("a1", "r1")(w, r)
	})

	c := NewClient(srv.URL, "tester.bsky.social", "app-pass")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Handle() != "tester.bsky.social" {
		t.Fatalf("Handle = %q", c.Handle())
	}
}

func TestLoginRejectsEmptyTokens(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("", ""))

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err == nil || !strings.Contains(err.Error(), "empty session tokens") {
		t.Fatalf("Login = %v, want empty session tokens error", err)
	}
}

func TestRefreshUsesRefreshJWTAsBearer(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("a1", "r1"))
	f.handle("com.atproto.server.refreshSession", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer r1" {
			t.Errorf("refresh Authorization = %q, want Bearer r1", got)
		}
		sessionHandler("a2", "r2")(w, r)
	})

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	access, refresh := c.accessJwt, c.refreshJwt
	c.mu.Unlock()
	if access != "a2" || refresh != "r2" {
		t.Fatalf("tokens after refresh = %q/%q, want a2/r2", access, refresh)
	}
}

func TestRefreshWithoutTokenFails(t *testing.T) {
	_, srv := newFakePDS(t)
	c := NewClient(srv.URL, "id", "pw")
	if err := c.Refresh(t.Context()); err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("Refresh = %v, want no refresh token error", err)
	}
}

func TestCallAuthedRefreshesOnExpiredAccessToken(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("a1", "r1"))
	f.handle("com.atproto.server.refreshSession", sessionHandler("a2", "r2"))
	f.handle("app.bsky.feed.getTimeline", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.Header.Get("Authorization"), "a1") {
			xrpcError(http.StatusBadRequest, "ExpiredToken")(w, r)
			return
		}
		writeJSON(w, FeedResponse{Cursor: "next"})
	})

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	out, err := c.GetTimeline(t.Context(), 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Cursor != "next" {
		t.Fatalf("cursor = %q, want next", out.Cursor)
	}
	want := []string{
		"com.atproto.server.createSession bearer=",
		"app.bsky.feed.getTimeline bearer=a1",
		"com.atproto.server.refreshSession bearer=r1",
		"app.bsky.feed.getTimeline bearer=a2",
	}
	got := f.log()
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCallAuthedRelogsInWhenRefreshExpired(t *testing.T) {
	f, srv := newFakePDS(t)
	logins := 0
	f.handle("com.atproto.server.createSession", func(w http.ResponseWriter, r *http.Request) {
		logins++
		if logins == 1 {
			sessionHandler("a1", "r1")(w, r)
			return
		}
		sessionHandler("a3", "r3")(w, r)
	})
	f.handle("com.atproto.server.refreshSession", xrpcError(http.StatusBadRequest, "ExpiredToken"))
	f.handle("app.bsky.feed.getTimeline", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.Header.Get("Authorization"), "a3") {
			writeJSON(w, FeedResponse{Cursor: "ok"})
			return
		}
		xrpcError(http.StatusBadRequest, "ExpiredToken")(w, r)
	})

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	out, err := c.GetTimeline(t.Context(), 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Cursor != "ok" {
		t.Fatalf("cursor = %q, want ok", out.Cursor)
	}
	if logins != 2 {
		t.Fatalf("createSession calls = %d, want 2 (initial + re-login)", logins)
	}
}

func TestCallAuthedDoesNotRetryNonAuthErrors(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("a1", "r1"))
	f.handle("app.bsky.feed.getTimeline", xrpcError(http.StatusInternalServerError, "InternalServerError"))

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetTimeline(t.Context(), 50, ""); err == nil {
		t.Fatal("expected error from 500 response")
	}
	for _, line := range f.log() {
		if strings.HasPrefix(line, "com.atproto.server.refreshSession") {
			t.Fatalf("refresh attempted on non-auth error; requests=%v", f.log())
		}
	}
}

func TestGetTimelineClampsLimitAndPassesCursor(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("a1", "r1"))
	var gotLimit, gotCursor string
	f.handle("app.bsky.feed.getTimeline", func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("cursor")
		writeJSON(w, FeedResponse{})
	})

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetTimeline(t.Context(), 0, "page2"); err != nil {
		t.Fatal(err)
	}
	if gotLimit != "50" || gotCursor != "page2" {
		t.Fatalf("limit/cursor = %q/%q, want 50/page2", gotLimit, gotCursor)
	}
	if _, err := c.GetTimeline(t.Context(), 999, ""); err != nil {
		t.Fatal(err)
	}
	if gotLimit != "50" {
		t.Fatalf("limit = %q after out-of-range request, want 50", gotLimit)
	}
}

func TestGetPostsChunksURIsAndSkipsEmpty(t *testing.T) {
	f, srv := newFakePDS(t)
	f.handle("com.atproto.server.createSession", sessionHandler("a1", "r1"))
	var gotURIs []string
	f.handle("app.bsky.feed.getPosts", func(w http.ResponseWriter, r *http.Request) {
		gotURIs = r.URL.Query()["uris"]
		writeJSON(w, map[string][]PostView{"posts": {{URI: "at://one"}}})
	})

	c := NewClient(srv.URL, "id", "pw")
	if err := c.Login(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Empty input short-circuits without HTTP.
	posts, err := c.GetPosts(t.Context(), nil)
	if err != nil || posts != nil {
		t.Fatalf("GetPosts(nil) = %v, %v, want nil, nil", posts, err)
	}
	for _, line := range f.log() {
		if strings.HasPrefix(line, "app.bsky.feed.getPosts") {
			t.Fatal("GetPosts(nil) made an HTTP call")
		}
	}

	uris := make([]string, 30)
	for i := range uris {
		uris[i] = "at://post/" + string(rune('a'+i))
	}
	posts, err = c.GetPosts(t.Context(), uris)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotURIs) != getPostsMax {
		t.Fatalf("sent %d uris, want %d (chunk cap)", len(gotURIs), getPostsMax)
	}
	if len(posts) != 1 || posts[0].URI != "at://one" {
		t.Fatalf("posts = %+v", posts)
	}
}

func TestNewClientNormalizesPDS(t *testing.T) {
	c := NewClient("", "id", "pw")
	if c.PDS() != DefaultPDS {
		t.Fatalf("PDS = %q, want %q", c.PDS(), DefaultPDS)
	}
	c = NewClient("https://pds.example/", "id", "pw")
	if c.PDS() != "https://pds.example" {
		t.Fatalf("PDS = %q, want trailing slash stripped", c.PDS())
	}
}
