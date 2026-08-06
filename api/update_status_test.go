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
		Interval: time.Hour,
		Current:  updates.BuildInfo{Commit: "abc123"},
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
	if got.UpdateAvailable {
		t.Fatal("no check ran yet, expected update_available=false")
	}
}
