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

	if yt, ok := f.tryYouTube(ctx, target, out.FetchedAt); ok {
		return yt
	}

	if classified, ok := f.classifyHead(ctx, target); ok {
		return classified
	}

	resp, err := f.doRequest(ctx, http.MethodGet, target)
	if err != nil {
		out.Kind = ircdb.PreviewKindError
		out.Error = err.Error()
		return out
	}
	defer func() { _ = resp.Body.Close() }()
	ct := contentType(resp.Header.Get("Content-Type"))
	if kind, ok := classifyContentType(ct); ok {
		out.Kind = kind
		out.Mime = ct
		return out
	}
	og, err := parseOpenGraph(io.LimitReader(resp.Body, f.cfg.MaxBytes))
	if err != nil {
		out.Kind = ircdb.PreviewKindError
		out.Error = err.Error()
		return out
	}
	f.enrichFromActivityPub(ctx, &og)

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

// tryYouTube short-circuits HTML scraping when target points at YouTube and a
// richer metadata endpoint is available.
func (f *Fetcher) tryYouTube(ctx context.Context, target string, fetchedAt time.Time) (ircdb.URLPreview, bool) {
	parsed, perr := url.Parse(target)
	if perr != nil || !IsYouTube(parsed) {
		return ircdb.URLPreview{}, false
	}
	yt, ok := f.fetchYouTube(ctx, target)
	if !ok {
		return ircdb.URLPreview{}, false
	}
	yt.FetchedAt = fetchedAt
	return yt, true
}

// classifyHead probes the URL with HEAD to skip the bounded GET for images and
// non-HTML content. Hosts that reject HEAD fall through to GET.
func (f *Fetcher) classifyHead(ctx context.Context, target string) (ircdb.URLPreview, bool) {
	head, err := f.doRequest(ctx, http.MethodHead, target)
	if err != nil {
		return ircdb.URLPreview{}, false
	}
	defer func() { _ = head.Body.Close() }()
	ct := contentType(head.Header.Get("Content-Type"))
	kind, ok := classifyContentType(ct)
	if !ok {
		return ircdb.URLPreview{}, false
	}
	return ircdb.URLPreview{URL: target, Kind: kind, Mime: ct, FetchedAt: time.Now().UTC()}, true
}

// classifyContentType returns a non-HTML preview kind for image and unknown
// content. Returns ok=false when ct is text/html and HTML scraping should run.
func classifyContentType(ct string) (string, bool) {
	if strings.HasPrefix(ct, "image/") {
		return ircdb.PreviewKindImage, true
	}
	if !strings.HasPrefix(ct, "text/html") {
		return ircdb.PreviewKindNone, true
	}
	return "", false
}

// enrichFromActivityPub augments og with full ActivityPub content when the
// page advertises an AP alternate link. Mastodon's og:description is heavily
// truncated; AP `content` carries the whole post.
func (f *Fetcher) enrichFromActivityPub(ctx context.Context, og *openGraph) {
	if og.apURL == "" {
		return
	}
	ap, ok := f.fetchActivityPub(ctx, og.apURL)
	if !ok {
		return
	}
	if ap.content != "" {
		og.description = ap.content
	}
	if og.image == "" && ap.image != "" {
		og.image = ap.image
	}
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
	apURL       string // ActivityPub alternate link href, if present
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
		switch z.Next() {
		case html.ErrorToken:
			err := z.Err()
			if errors.Is(err, io.EOF) {
				out.finalize()
				return out, nil
			}
			return out, err
		case html.StartTagToken, html.SelfClosingTagToken:
			if handleStartTag(z, &out, &inTitle) {
				out.finalize()
				return out, nil
			}
		case html.EndTagToken:
			if handleEndTag(z, &inTitle) {
				out.finalize()
				return out, nil
			}
		case html.TextToken:
			if inTitle {
				out.htmlTitle += string(z.Text())
			}
		}
	}
}

func (o *openGraph) finalize() {
	if o.title == "" && o.htmlTitle != "" {
		o.title = o.htmlTitle
	}
}

// handleStartTag returns true when scanning should stop (entered <body>).
func handleStartTag(z *html.Tokenizer, out *openGraph, inTitle *bool) bool {
	name, hasAttr := z.TagName()
	switch string(name) {
	case "title":
		*inTitle = true
	case "link":
		if hasAttr {
			handleLinkTag(z, out)
		}
	case "body":
		return true
	case "meta":
		if hasAttr {
			prop, n, content := readMetaAttrs(z)
			assignMeta(out, prop, n, content)
		}
	}
	return false
}

// handleEndTag returns true when scanning should stop (closed </head>).
func handleEndTag(z *html.Tokenizer, inTitle *bool) bool {
	name, _ := z.TagName()
	switch string(name) {
	case "title":
		*inTitle = false
	case "head":
		return true
	}
	return false
}

func handleLinkTag(z *html.Tokenizer, out *openGraph) {
	var rel, typ, href string
	for {
		k, v, more := z.TagAttr()
		switch strings.ToLower(string(k)) {
		case "rel":
			rel = strings.ToLower(string(v))
		case "type":
			typ = strings.ToLower(string(v))
		case "href":
			href = string(v)
		}
		if !more {
			break
		}
	}
	if out.apURL != "" || href == "" {
		return
	}
	if strings.Contains(rel, "alternate") && typ == "application/activity+json" {
		out.apURL = href
	}
}

func readMetaAttrs(z *html.Tokenizer) (prop, name, content string) {
	for {
		k, v, more := z.TagAttr()
		switch strings.ToLower(string(k)) {
		case "property":
			prop = strings.ToLower(string(v))
		case "name":
			name = strings.ToLower(string(v))
		case "content":
			content = string(v)
		}
		if !more {
			break
		}
	}
	return
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
