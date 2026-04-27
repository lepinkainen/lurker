package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/updates"
)

func TestUpdateStatusEndpoint(t *testing.T) {
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stores.Close() }()

	checker := updates.New(updates.Config{
		Enabled:  true,
		Image:    "ghcr.io/lepinkainen/lurker",
		Tag:      "latest",
		Interval: time.Hour,
		Current: updates.BuildInfo{
			Version:   "dev",
			Commit:    "abc123",
			BuildTime: "2026-04-27T00:00:00Z",
		},
	})

	srv := &Server{Stores: stores, UpdateChecker: checker}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/update-status", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var got updates.Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("expected enabled=true")
	}
	if got.Image != "ghcr.io/lepinkainen/lurker" {
		t.Fatalf("image = %q", got.Image)
	}
	if got.Tag != "latest" {
		t.Fatalf("tag = %q", got.Tag)
	}
	if got.CurrentCommit != "abc123" {
		t.Fatalf("current commit = %q", got.CurrentCommit)
	}
}
