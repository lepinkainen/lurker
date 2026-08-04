package media

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadURL(t *testing.T) {
	newService := func(baseURL string) *Service {
		return &Service{Cfg: Config{Dir: t.TempDir(), MaxBytes: 1 << 20, BaseURL: baseURL}}
	}

	t.Run("no base URL, plain request", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		got := srv.uploadURL(req, "id123.jpg")
		want := "http://example.com/uploads/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("X-Forwarded-Proto https", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		got := srv.uploadURL(req, "id123.jpg")
		want := "https://example.com/uploads/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("X-Forwarded-Proto scheme injection rejected", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "javascript")
		got := srv.uploadURL(req, "id123.jpg")
		if !strings.HasPrefix(got, "http://") {
			t.Errorf("uploadURL() = %q, want scheme forced to http://", got)
		}
		if strings.Contains(got, "javascript") {
			t.Errorf("uploadURL() = %q, should not contain injected scheme", got)
		}
	})

	t.Run("X-Forwarded-Proto comma list", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "https, http")
		got := srv.uploadURL(req, "id123.jpg")
		want := "https://example.com/uploads/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("malicious Host with CRLF falls back to relative URL", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Host = "evil.com/\r\nfoo"
		got := srv.uploadURL(req, "id123.jpg")
		want := "/uploads/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("malicious Host with space falls back to relative URL", func(t *testing.T) {
		srv := newService("")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Host = "a b"
		got := srv.uploadURL(req, "id123.jpg")
		want := "/uploads/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})

	t.Run("BaseURL set wins regardless of request", func(t *testing.T) {
		srv := newService("https://cdn.example.com/files")
		req := httptest.NewRequest("GET", "/api/upload", nil)
		req.Header.Set("X-Forwarded-Proto", "javascript")
		req.Host = "a b"
		got := srv.uploadURL(req, "id123.jpg")
		want := "https://cdn.example.com/files/id123.jpg"
		if got != want {
			t.Errorf("uploadURL() = %q, want %q", got, want)
		}
	})
}
