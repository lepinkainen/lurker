package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestPreviewStore(t *testing.T) *PreviewStore {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenPreviewStore(filepath.Join(dir, "previews.db"))
	if err != nil {
		t.Fatalf("open preview store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPreviewStorePutGet(t *testing.T) {
	s := newTestPreviewStore(t)
	ctx := context.Background()
	p := URLPreview{URL: "https://ex.test/a", Kind: PreviewKindImage, Mime: "image/png"}
	if err := s.Put(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := s.Get(ctx, p.URL, time.Hour)
	if err != nil || !ok {
		t.Fatalf("get missing: ok=%v err=%v", ok, err)
	}
	if got.Kind != PreviewKindImage || got.Mime != "image/png" {
		t.Fatalf("bad payload: %+v", got)
	}
}

func TestPreviewStoreTTL(t *testing.T) {
	s := newTestPreviewStore(t)
	ctx := context.Background()
	p := URLPreview{
		URL:       "https://ex.test/stale",
		Kind:      PreviewKindNone,
		FetchedAt: time.Now().Add(-2 * time.Hour).UTC(),
	}
	if err := s.Put(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, ok, err := s.Get(ctx, p.URL, time.Hour)
	if err != nil {
		t.Fatalf("get err: %v", err)
	}
	if ok {
		t.Fatalf("expected ttl miss")
	}
}

func TestPreviewStoreGetMany(t *testing.T) {
	s := newTestPreviewStore(t)
	ctx := context.Background()
	for _, u := range []string{"https://a.test", "https://b.test", "https://c.test"} {
		if err := s.Put(ctx, URLPreview{URL: u, Kind: PreviewKindNone}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetMany(ctx, []string{"https://a.test", "https://c.test", "https://a.test", "https://missing.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if _, ok := got["https://a.test"]; !ok {
		t.Fatalf("missing a.test")
	}
	if _, ok := got["https://c.test"]; !ok {
		t.Fatalf("missing c.test")
	}
}
