package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	ircdb "github.com/lepinkainen/lurker/db"
)

// FetcherConfig controls outbound HTTP behavior. Zero values use defaults.
type FetcherConfig struct {
	UserAgent string
	Timeout   time.Duration
	MaxBytes  int64
	Resolver  Resolver
	// SSRFCheck, when set, replaces the default CheckURL call. Useful in
	// tests that need to reach httptest.Server on 127.0.0.1.
	SSRFCheck func(ctx context.Context, url string) error
}

// Fetcher fetches one URL and returns a URLPreview. One Fetcher is safe for
// concurrent use.
type Fetcher struct {
	cfg    FetcherConfig
	client *http.Client
}

// NewFetcher builds a Fetcher with an HTTP client that enforces SSRF on every
// hop (initial + redirects) and caps redirects to 3.
func NewFetcher(cfg FetcherConfig) *Fetcher {
	if cfg.UserAgent == "" {
		// Many sites (YouTube, news outlets) strip OpenGraph for unknown
		// scrapers. A generic browser-ish UA gets us the tags without
		// pretending to be a crawler that honors robots.txt differently.
		cfg.UserAgent = "Mozilla/5.0 (compatible; lurker-preview/1; +https://github.com/lepinkainen/lurker)"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 512 * 1024
	}
	if cfg.SSRFCheck == nil {
		resolver := cfg.Resolver
		cfg.SSRFCheck = func(ctx context.Context, u string) error {
			return CheckURL(ctx, resolver, u)
		}
	}
	f := &Fetcher{cfg: cfg}
	f.client = &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return cfg.SSRFCheck(req.Context(), req.URL.String())
		},
	}
	return f
}

// Fetch resolves preview metadata for url. It returns a populated URLPreview
// even on failure (kind=error), so callers can cache negative results.
func (f *Fetcher) Fetch(ctx context.Context, target string) ircdb.URLPreview {
	out := ircdb.URLPreview{URL: target, FetchedAt: time.Now().UTC()}

	if err := f.cfg.SSRFCheck(ctx, target); err != nil {
		out.Kind = ircdb.PreviewKindError
		out.Error = err.Error()
		return out
	}

	// Site-specific shortcuts that bypass HTML scraping when a richer
	// metadata endpoint is available.
	if parsed, perr := url.Parse(target); perr == nil && IsYouTube(parsed) {
		if yt, ok := f.fetchYouTube(ctx, target); ok {
			yt.FetchedAt = out.FetchedAt
			return yt
		}
	}

	// HEAD first to classify cheaply. Some hosts reject HEAD; fall back to a
	// bounded GET in that case.
	head, err := f.doRequest(ctx, http.MethodHead, target)
	if err == nil {
		defer func() { _ = head.Body.Close() }()
		ct := contentType(head.Header.Get("Content-Type"))
		if strings.HasPrefix(ct, "image/") {
			out.Kind = ircdb.PreviewKindImage
			out.Mime = ct
			return out
		}
		if !strings.HasPrefix(ct, "text/html") {
			out.Kind = ircdb.PreviewKindNone
			out.Mime = ct
			return out
		}
	}

	// HTML: bounded GET + OpenGraph parse.
	resp, err := f.doRequest(ctx, http.MethodGet, target)
	if err != nil {
		out.Kind = ircdb.PreviewKindError
		out.Error = err.Error()
		return out
	}
	defer func() { _ = resp.Body.Close() }()
	ct := contentType(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "image/") {
		out.Kind = ircdb.PreviewKindImage
		out.Mime = ct
		return out
	}
	if !strings.HasPrefix(ct, "text/html") {
		out.Kind = ircdb.PreviewKindNone
		out.Mime = ct
		return out
	}
	og, err := parseOpenGraph(io.LimitReader(resp.Body, f.cfg.MaxBytes))
	if err != nil {
		out.Kind = ircdb.PreviewKindError
		out.Error = err.Error()
		return out
	}
	if og.empty() {
		out.Kind = ircdb.PreviewKindNone
		out.Mime = ct
		return out
	}
	out.Kind = ircdb.PreviewKindOpenGraph
	out.Mime = ct
	out.Title = og.title
	out.Description = og.description
	out.ImageURL = og.image
	out.SiteName = og.siteName
	return out
}

func (f *Fetcher) doRequest(ctx context.Context, method, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("preview: http %d", resp.StatusCode)
	}
	return resp, nil
}

func contentType(raw string) string {
	if raw == "" {
		return ""
	}
	ct, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(raw, ";")[0]))
	}
	return strings.ToLower(ct)
}

type openGraph struct {
	title       string
	description string
	image       string
	siteName    string
	htmlTitle   string
}

func (o openGraph) empty() bool {
	return o.title == "" && o.description == "" && o.image == "" && o.siteName == "" && o.htmlTitle == ""
}

// parseOpenGraph scans an HTML stream for <meta property="og:*"> tags plus a
// fallback <title>. It stops at </head> to avoid tokenizing large bodies.
func parseOpenGraph(r io.Reader) (openGraph, error) {
	out := openGraph{}
	z := html.NewTokenizer(r)
	var inTitle bool
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			err := z.Err()
			if errors.Is(err, io.EOF) {
				if out.title == "" && out.htmlTitle != "" {
					out.title = out.htmlTitle
				}
				return out, nil
			}
			return out, err
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "title" {
				inTitle = true
				continue
			}
			if tag == "body" {
				// Past the head; stop scanning.
				if out.title == "" && out.htmlTitle != "" {
					out.title = out.htmlTitle
				}
				return out, nil
			}
			if tag != "meta" || !hasAttr {
				continue
			}
			var prop, name2, content string
			for {
				k, v, more := z.TagAttr()
				key := strings.ToLower(string(k))
				val := string(v)
				switch key {
				case "property":
					prop = strings.ToLower(val)
				case "name":
					name2 = strings.ToLower(val)
				case "content":
					content = val
				}
				if !more {
					break
				}
			}
			assignMeta(&out, prop, name2, content)
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "title" {
				inTitle = false
			}
			if string(name) == "head" {
				if out.title == "" && out.htmlTitle != "" {
					out.title = out.htmlTitle
				}
				return out, nil
			}
		case html.TextToken:
			if inTitle {
				out.htmlTitle += string(z.Text())
			}
		}
	}
}

func assignMeta(o *openGraph, prop, name, content string) {
	if content == "" {
		return
	}
	key := prop
	if key == "" {
		key = name
	}
	switch key {
	case "og:title", "twitter:title":
		if o.title == "" {
			o.title = content
		}
	case "og:description", "twitter:description", "description":
		if o.description == "" {
			o.description = content
		}
	case "og:image", "twitter:image", "twitter:image:src":
		if o.image == "" {
			o.image = content
		}
	case "og:site_name":
		if o.siteName == "" {
			o.siteName = content
		}
	}
}
