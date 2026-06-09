package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewAppliesDefaults(t *testing.T) {
	c := New(Config{})
	if c.cfg.Image != "ghcr.io/lepinkainen/lurker" {
		t.Errorf("Image default = %q", c.cfg.Image)
	}
	if c.cfg.Tag != "latest" {
		t.Errorf("Tag default = %q", c.cfg.Tag)
	}
	if c.cfg.Interval < time.Hour {
		t.Errorf("Interval < 1h = %v", c.cfg.Interval)
	}
	if c.cfg.Platform.OS != "linux" || c.cfg.Platform.Architecture != "amd64" {
		t.Errorf("Platform default = %+v", c.cfg.Platform)
	}
	if c.client == nil || c.logger == nil {
		t.Fatal("client / logger should be defaulted")
	}
}

func TestNewSeedsStatusFromConfig(t *testing.T) {
	c := New(Config{
		Enabled: true,
		Image:   "ghcr.io/example/x",
		Tag:     "v1",
		Current: BuildInfo{Version: "1.0.0", Commit: "abc", BuildTime: "now"},
	})
	st := c.Status()
	if !st.Enabled || st.Image != "ghcr.io/example/x" || st.Tag != "v1" {
		t.Fatalf("status seed = %+v", st)
	}
	if st.CurrentVersion != "1.0.0" || st.CurrentCommit != "abc" {
		t.Fatalf("current build info not propagated: %+v", st)
	}
}

func TestStartIsNoopWhenDisabled(t *testing.T) {
	c := New(Config{Enabled: false})
	c.Start(context.Background())
	// No goroutine, no panic. Status must remain the seed.
	if c.Status().Error != "" {
		t.Fatalf("unexpected error: %q", c.Status().Error)
	}
}

func TestCheckNowIsNoopWhenDisabled(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()
	c := New(Config{Enabled: false, Image: "ghcr.io/x/y"})
	c.CheckNow(context.Background())
	if called {
		t.Fatal("disabled CheckNow should not hit registry")
	}
}

func TestSetStatusOverwritesAtomically(t *testing.T) {
	c := New(Config{Enabled: true})
	c.setStatus(Status{Enabled: true, Image: "img", RemoteCommit: "rc"})
	if got := c.Status(); got.RemoteCommit != "rc" || got.Image != "img" {
		t.Fatalf("setStatus = %+v", got)
	}
	c.setStatus(Status{Enabled: true, Image: "img2"})
	if got := c.Status(); got.RemoteCommit != "" || got.Image != "img2" {
		t.Fatalf("second setStatus = %+v", got)
	}
}

func TestParseChallengeValid(t *testing.T) {
	ch, err := parseChallenge(`Bearer realm="https://auth/token",service="reg",scope="repository:r:pull"`)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Realm != "https://auth/token" || ch.Service != "reg" || ch.Scope != "repository:r:pull" {
		t.Fatalf("challenge = %+v", ch)
	}
}

func TestParseChallengeMissing(t *testing.T) {
	if _, err := parseChallenge(""); err == nil {
		t.Fatal("expected error for empty challenge")
	}
}

func TestParseChallengeIncomplete(t *testing.T) {
	if _, err := parseChallenge(`Bearer scope="x"`); err == nil {
		t.Fatal("expected error for incomplete challenge")
	}
}

func TestBasicAuthEncoding(t *testing.T) {
	got := basicAuth("user", "pass")
	if !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("missing Basic prefix: %q", got)
	}
}

// Mock GHCR registry: serves manifest list → image manifest → config blob with
// labels that report a different commit so compareRemote returns true.
func newMockGHCR(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	mux := http.NewServeMux()
	var tokenURL string

	mux.HandleFunc("/v2/lepinkainen/lurker/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="ghcr.io",scope="repository:lepinkainen/lurker:pull"`, tokenURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", mediaTypeOCIImageIndex)
		w.Header().Set("Docker-Content-Digest", "sha256:list")
		body := manifestList{
			SchemaVersion: 2,
			MediaType:     mediaTypeOCIImageIndex,
			Manifests: []manifestListEntry{{
				MediaType: mediaTypeOCIImageManifest,
				Digest:    "sha256:plat",
				Platform:  manifestPlatform{OS: "linux", Architecture: "amd64"},
			}},
		}
		_ = json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("/v2/lepinkainen/lurker/manifests/sha256:plat", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", mediaTypeOCIImageManifest)
		w.Header().Set("Docker-Content-Digest", "sha256:plat")
		body := imageManifest{SchemaVersion: 2}
		body.Config.MediaType = "application/vnd.oci.image.config.v1+json"
		body.Config.Digest = "sha256:config"
		_ = json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("/v2/lepinkainen/lurker/blobs/sha256:config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]any{
				"Labels": map[string]string{
					"org.opencontainers.image.version":  "9.9.9",
					"org.opencontainers.image.revision": "remote-commit",
					"org.opencontainers.image.created":  "2030-01-01T00:00:00Z",
				},
			},
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"token":"test-token"}`)
	})

	srv := httptest.NewServer(mux)
	tokenURL = srv.URL + "/token"
	return srv, srv.Close
}

func TestCheckNowSetsStatusOnSuccess(t *testing.T) {
	srv, stop := newMockGHCR(t)
	defer stop()

	// Reroute ghcr.io to the test server by patching the http.Client via a
	// custom Transport that rewrites the request URL.
	client := &http.Client{
		Transport: ghcrRewriter{base: srv.URL},
		Timeout:   3 * time.Second,
	}

	c := New(Config{
		Enabled:  true,
		Image:    "ghcr.io/lepinkainen/lurker",
		Tag:      "latest",
		Current:  BuildInfo{Commit: "local-commit", Version: "0.1.0"},
		Platform: Platform{OS: "linux", Architecture: "amd64"},
		Client:   client,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	c.CheckNow(context.Background())

	st := c.Status()
	if st.Error != "" {
		t.Fatalf("unexpected error: %s", st.Error)
	}
	if st.RemoteCommit != "remote-commit" {
		t.Fatalf("RemoteCommit = %q", st.RemoteCommit)
	}
	if !st.UpdateAvailable {
		t.Fatalf("UpdateAvailable should be true when commits differ: %+v", st)
	}
	if st.CheckedAt.IsZero() {
		t.Fatal("CheckedAt should be set")
	}
}

func TestCheckNowRecordsErrorOnNetworkFailure(t *testing.T) {
	// Transport that always fails.
	client := &http.Client{Transport: errTransport{}}
	c := New(Config{
		Enabled: true,
		Image:   "ghcr.io/lepinkainen/lurker",
		Tag:     "latest",
		Current: BuildInfo{Commit: "abc"},
		Client:  client,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	c.CheckNow(context.Background())
	if st := c.Status(); st.Error == "" {
		t.Fatal("expected Error to be set after network failure")
	}
}

// ghcrRewriter forwards any request whose host is ghcr.io or auth.docker.io to
// the local test server.
type ghcrRewriter struct{ base string }

func (g ghcrRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(g.base + req.URL.Path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = req.URL.RawQuery
	req.URL = u
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

type errTransport struct{}

func (errTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("simulated network failure")
}
