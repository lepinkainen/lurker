package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func runsClient(t *testing.T, shas ...string) *http.Client {
	t.Helper()
	type run struct {
		HeadSHA string `json:"head_sha"`
	}
	runs := make([]run, len(shas))
	for i, s := range shas {
		runs[i] = run{HeadSHA: s}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{base: srv.URL}}
}

// rewriteTransport sends every request to the test server regardless of host.
type rewriteTransport struct{ base string }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	nreq := req.Clone(req.Context())
	u, err := nreq.URL.Parse(rt.base + req.URL.Path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = req.URL.RawQuery
	nreq.URL = u
	return http.DefaultTransport.RoundTrip(nreq)
}

func TestNewAppliesDefaults(t *testing.T) {
	c := New(Config{Enabled: true, Interval: time.Minute})
	if c.cfg.Repo != "lepinkainen/lurker" {
		t.Fatalf("repo = %q", c.cfg.Repo)
	}
	if c.cfg.Interval != time.Hour {
		t.Fatalf("interval not clamped: %v", c.cfg.Interval)
	}
	if !c.Status().Enabled {
		t.Fatal("expected enabled status")
	}
}

func TestCheckNowUpdateAvailable(t *testing.T) {
	c := New(Config{
		Enabled: true,
		Current: BuildInfo{Commit: "aaaa111122223333aaaa111122223333aaaa1111"},
		Client:  runsClient(t, "bbbb111122223333aaaa111122223333aaaa1111"),
	})
	c.CheckNow(context.Background())
	st := c.Status()
	if !st.UpdateAvailable {
		t.Fatalf("expected update available, status %+v", st)
	}
	if st.RemoteVersion != "bbbb111" {
		t.Fatalf("remote version = %q", st.RemoteVersion)
	}
	if st.Error != "" {
		t.Fatalf("unexpected error %q", st.Error)
	}
}

func TestCheckNowUpToDate(t *testing.T) {
	sha := "aaaa111122223333aaaa111122223333aaaa1111"
	c := New(Config{Enabled: true, Current: BuildInfo{Commit: sha}, Client: runsClient(t, sha)})
	c.CheckNow(context.Background())
	if st := c.Status(); st.UpdateAvailable {
		t.Fatalf("expected up to date, status %+v", st)
	}
}

func TestCheckNowDevBuildNeverFlags(t *testing.T) {
	c := New(Config{Enabled: true, Current: BuildInfo{Commit: "unknown"}, Client: runsClient(t, "abcdef1234")})
	c.CheckNow(context.Background())
	st := c.Status()
	if st.UpdateAvailable {
		t.Fatal("dev build must not report update")
	}
	if st.RemoteVersion != "abcdef1" {
		t.Fatalf("remote version = %q", st.RemoteVersion)
	}
}

func TestCheckNowRecordsError(t *testing.T) {
	c := New(Config{Enabled: true, Client: runsClient(t)}) // zero runs
	c.CheckNow(context.Background())
	st := c.Status()
	if st.Error == "" {
		t.Fatal("expected error recorded")
	}
	if st.UpdateAvailable {
		t.Fatal("error check must not flag update")
	}
}

func TestCheckNowIsNoopWhenDisabled(t *testing.T) {
	c := New(Config{Enabled: false, Client: runsClient(t, "abc")})
	c.CheckNow(context.Background())
	if st := c.Status(); !st.CheckedAt.IsZero() {
		t.Fatalf("disabled checker ran: %+v", st)
	}
}

func TestUpdateAvailableShortHashEitherSide(t *testing.T) {
	full := "aaaa111122223333aaaa111122223333aaaa1111"
	if updateAvailable(full[:7], full) || updateAvailable(full, full[:7]) {
		t.Fatal("short-vs-full same commit must not flag update")
	}
	if !updateAvailable("bbbb111", full) {
		t.Fatal("different commit must flag update")
	}
	if updateAvailable("", full) || updateAvailable(full, "") {
		t.Fatal("missing data must not flag update")
	}
}
