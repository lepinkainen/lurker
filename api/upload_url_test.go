package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadURL(t *testing.T) {
	newServer := func(baseURL string) *Server {
		return &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 1 << 20, BaseURL: baseURL}}
	}

	t.Run("no base URL, plain request", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		got := srv.uploadURL(req, "abc.jpg")
		want := "http://example.com/uploads/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("X-Forwarded-Proto https", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		got := srv.uploadURL(req, "abc.jpg")
		want := "https://example.com/uploads/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("X-Forwarded-Proto scheme injection rejected", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "javascript")
		got := srv.uploadURL(req, "abc.jpg")
		if !strings.HasPrefix(got, "http://") {
			t.Errorf("uploadURL() = %q, want scheme forced to http://", got)
		}
		if strings.Contains(got, "javascript") {
			t.Errorf("uploadURL() = %q, should not contain injected scheme", got)
		}
	})

	t.Run("X-Forwarded-Proto comma list", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "https, http")
		got := srv.uploadURL(req, "abc.jpg")
		want := "https://example.com/uploads/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("malicious Host with CRLF falls back to relative URL", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Host = "evil.com/\r\nfoo"
		got := srv.uploadURL(req, "abc.jpg")
		want := "/uploads/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("malicious Host with space falls back to relative URL", func(t *testing.T) {
		srv := newServer("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Host = "a b"
		got := srv.uploadURL(req, "abc.jpg")
		want := "/uploads/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("BaseURL set wins regardless of request", func(t *testing.T) {
		srv := newServer("https://cdn.example.com/files")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "javascript")
		req.Host = "a b"
		got := srv.uploadURL(req, "abc.jpg")
		want := "https://cdn.example.com/files/abc.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})
}
