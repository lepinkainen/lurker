package preview

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchImageHappyPath(t *testing.T) {
	want := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	defer srv.Close()

	f := NewFetcher(permissive())
	data, mime, err := f.FetchImage(context.Background(), srv.URL+"/avatar.png")
	if err != nil {
		t.Fatalf("FetchImage: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", mime)
	}
	if string(data) != string(want) {
		t.Fatalf("data = %v, want %v", data, want)
	}
}

func TestFetchImageRejectsNonImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	f := NewFetcher(permissive())
	_, _, err := f.FetchImage(context.Background(), srv.URL+"/")
	if !errors.Is(err, ErrNotImage) {
		t.Fatalf("expected ErrNotImage, got %v", err)
	}
}

func TestFetchImageBlocked(t *testing.T) {
	f := NewFetcher(FetcherConfig{
		SSRFCheck: func(context.Context, string) error { return fmt.Errorf("%w: test", ErrBlocked) },
		Timeout:   time.Second,
	})
	_, _, err := f.FetchImage(context.Background(), "https://example.test/avatar.png")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}

func TestFetchImageOversized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(make([]byte, 8*1024))
	}))
	defer srv.Close()

	cfg := permissive()
	cfg.MaxBytes = 1024
	f := NewFetcher(cfg)
	_, _, err := f.FetchImage(context.Background(), srv.URL+"/big.png")
	if err == nil {
		t.Fatalf("expected error for oversized image")
	}
}
