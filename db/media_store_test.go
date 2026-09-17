package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lepinkainen/lurker/media"
)

func newTestMediaStore(t *testing.T) *MediaStore {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenMediaStore(filepath.Join(dir, "media.db"))
	if err != nil {
		t.Fatalf("open media store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func insertTestMedia(t *testing.T, s *MediaStore, id, kind, mime string, createdAt time.Time) {
	t.Helper()
	m := media.Media{
		ID:         id,
		Kind:       kind,
		Mime:       mime,
		SourceHash: "hash-" + id,
		Variants: []media.Variant{{
			Name: "main", Key: id + "/main.png", Mime: mime, Width: 1, Height: 1, Bytes: 10,
		}},
		Size:      10,
		Width:     1,
		Height:    1,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if err := s.Insert(context.Background(), m); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func TestMediaStoreListOrdersNewestFirst(t *testing.T) {
	s := newTestMediaStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMedia(t, s, "old", "image", "image/png", t0)
	insertTestMedia(t, s, "new", "image", "image/png", t0.Add(time.Hour))

	items, total, err := s.List(ctx, media.ListFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(items) != 2 || items[0].ID != "new" || items[1].ID != "old" {
		t.Fatalf("items = %+v, want [new, old]", items)
	}
}

func TestMediaStoreListFiltersByKind(t *testing.T) {
	s := newTestMediaStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMedia(t, s, "img1", "image", "image/png", t0)
	insertTestMedia(t, s, "vid1", "video", "video/mp4", t0.Add(time.Minute))

	items, total, err := s.List(ctx, media.ListFilter{Kind: "video"}, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "vid1" {
		t.Fatalf("kind=video items = %+v total=%d", items, total)
	}
}

func TestMediaStoreListFiltersByQ(t *testing.T) {
	s := newTestMediaStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMedia(t, s, "abc123", "image", "image/png", t0)
	insertTestMedia(t, s, "def456", "image", "image/jpeg", t0.Add(time.Minute))

	items, total, err := s.List(ctx, media.ListFilter{Q: "abc"}, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "abc123" {
		t.Fatalf("q=abc items = %+v total=%d", items, total)
	}

	items, total, err = s.List(ctx, media.ListFilter{Q: "jpeg"}, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "def456" {
		t.Fatalf("q=jpeg (mime match) items = %+v total=%d", items, total)
	}
}

func TestMediaStoreListPagination(t *testing.T) {
	s := newTestMediaStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		insertTestMedia(t, s, string(rune('a'+i)), "image", "image/png", t0.Add(time.Duration(i)*time.Minute))
	}

	items, total, err := s.List(ctx, media.ListFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	// Newest-first order over ids a..e (e newest): e,d,c,b,a — offset 2 -> c,b.
	if items[0].ID != "c" || items[1].ID != "b" {
		t.Fatalf("page items = %+v, want [c, b]", items)
	}
}

func TestMediaStoreDeleteRemovesRow(t *testing.T) {
	s := newTestMediaStore(t)
	ctx := context.Background()
	insertTestMedia(t, s, "gone", "image", "image/png", time.Now())

	deleted, err := s.Delete(ctx, "gone")
	if err != nil || !deleted {
		t.Fatalf("delete: deleted=%v err=%v", deleted, err)
	}

	_, ok, err := s.Get(ctx, "gone")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if ok {
		t.Fatal("expected media to be gone after delete")
	}

	deleted, err = s.Delete(ctx, "does-not-exist")
	if err != nil || deleted {
		t.Fatalf("delete unknown: deleted=%v err=%v", deleted, err)
	}
}
