package preview

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
)

// permissive returns a FetcherConfig that allows loopback for httptest.
func permissive() FetcherConfig {
	return FetcherConfig{
		SSRFCheck: func(context.Context, string) error { return nil },
		Timeout:   2 * time.Second,
		MaxBytes:  64 * 1024,
	}
}

func TestFetcherImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
	}))
	defer srv.Close()

	f := NewFetcher(permissive())
	p := f.Fetch(context.Background(), srv.URL+"/pic.png")
	if p.Kind != ircdb.PreviewKindImage {
		t.Fatalf("expected image, got %+v", p)
	}
	if !strings.HasPrefix(p.Mime, "image/") {
		t.Fatalf("missing mime: %q", p.Mime)
	}
}

func TestFetcherOpenGraph(t *testing.T) {
	body := `<html><head>
	<meta property="og:title" content="Test Page">
	<meta property="og:description" content="a description">
	<meta property="og:image" content="https://example.test/thumb.jpg">
	<meta property="og:site_name" content="ExampleSite">
	<title>Ignored fallback</title>
	</head><body>...</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := NewFetcher(permissive())
	p := f.Fetch(context.Background(), srv.URL+"/")
	if p.Kind != ircdb.PreviewKindOpenGraph {
		t.Fatalf("expected opengraph, got %+v", p)
	}
	if p.Title != "Test Page" || p.Description != "a description" ||
		p.ImageURL != "https://example.test/thumb.jpg" || p.SiteName != "ExampleSite" {
		t.Fatalf("unexpected og fields: %+v", p)
	}
}

func TestFetcherHTMLTitleFallback(t *testing.T) {
	body := `<html><head><title>Fallback Title</title></head><body/></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := NewFetcher(permissive())
	p := f.Fetch(context.Background(), srv.URL+"/")
	if p.Kind != ircdb.PreviewKindOpenGraph {
		t.Fatalf("expected opengraph (title fallback), got %+v", p)
	}
	if p.Title != "Fallback Title" {
		t.Fatalf("title = %q", p.Title)
	}
}

func TestFetcherBlocked(t *testing.T) {
	f := NewFetcher(FetcherConfig{
		SSRFCheck: func(context.Context, string) error { return fmt.Errorf("%w: test", ErrBlocked) },
		Timeout:   time.Second,
	})
	p := f.Fetch(context.Background(), "https://example.test/")
	if p.Kind != ircdb.PreviewKindError {
		t.Fatalf("expected error kind, got %+v", p)
	}
}

func TestFetcherActivityPubEnrichment(t *testing.T) {
	var apHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/note", func(w http.ResponseWriter, r *http.Request) {
		apHits++
		if !strings.Contains(r.Header.Get("Accept"), "activity+json") {
			t.Errorf("AP fetch missing Accept header: %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write([]byte(`{
			"type":"Note",
			"content":"<p>Full first paragraph.</p><p>Second one with <a href=\"https://x.test/tags/foo\">#foo</a> tag.</p>",
			"attachment":[{"type":"Document","mediaType":"image/png","url":"https://cdn.test/x.png"}]
		}`))
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head>
			<meta property="og:title" content="Display Name (@user@host)">
			<meta property="og:description" content="truncated...">
			<meta property="og:site_name" content="Mastodon">
			<link rel="alternate" type="application/activity+json" href="http://` + r.Host + `/note">
		</head><body/></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := NewFetcher(permissive())
	p := f.Fetch(context.Background(), srv.URL+"/page")
	if p.Kind != ircdb.PreviewKindOpenGraph {
		t.Fatalf("expected opengraph, got %+v", p)
	}
	if apHits != 1 {
		t.Fatalf("expected 1 AP fetch, got %d", apHits)
	}
	if !strings.Contains(p.Description, "Full first paragraph.") ||
		!strings.Contains(p.Description, "Second one with #foo tag.") {
		t.Fatalf("description not enriched: %q", p.Description)
	}
	if !strings.Contains(p.Description, "\n\n") {
		t.Fatalf("paragraphs not preserved: %q", p.Description)
	}
	if p.ImageURL != "https://cdn.test/x.png" {
		t.Fatalf("image not taken from AP: %q", p.ImageURL)
	}
	if p.Title != "Display Name (@user@host)" {
		t.Fatalf("og title clobbered: %q", p.Title)
	}
}

func TestFetcherOversizedBody(t *testing.T) {
	big := strings.Repeat("x", 200*1024)
	body := `<html><head><meta property="og:title" content="Early"></head><body>` + big + `</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cfg := permissive()
	cfg.MaxBytes = 4 * 1024
	f := NewFetcher(cfg)
	p := f.Fetch(context.Background(), srv.URL+"/")
	if p.Kind != ircdb.PreviewKindOpenGraph {
		t.Fatalf("expected opengraph from early meta, got %+v", p)
	}
	if p.Title != "Early" {
		t.Fatalf("title = %q", p.Title)
	}
}
