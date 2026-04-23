package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	ircdb "github.com/lepinkainen/lurker/db"
)

// IsYouTube reports whether u is a YouTube video/short/live URL.
func IsYouTube(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	switch host {
	case "youtube.com":
		p := u.Path
		return strings.HasPrefix(p, "/watch") ||
			strings.HasPrefix(p, "/shorts/") ||
			strings.HasPrefix(p, "/live/") ||
			strings.HasPrefix(p, "/embed/")
	case "youtu.be":
		return strings.TrimPrefix(u.Path, "/") != ""
	}
	return false
}

// fetchYouTube asks YouTube's oEmbed endpoint for canonical metadata. oEmbed
// is public, unauthenticated, and returns JSON with title/author/thumbnail
// even for pages where the HTML strips OG for non-browser UAs.
func (f *Fetcher) fetchYouTube(ctx context.Context, target string) (ircdb.URLPreview, bool) {
	endpoint := "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape(target)
	if err := f.cfg.SSRFCheck(ctx, endpoint); err != nil {
		return ircdb.URLPreview{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return ircdb.URLPreview{}, false
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return ircdb.URLPreview{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ircdb.URLPreview{}, false
	}
	var oe struct {
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
		ProviderName string `json:"provider_name"`
		Width        int    `json:"thumbnail_width"`
		Height       int    `json:"thumbnail_height"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&oe); err != nil {
		return ircdb.URLPreview{}, false
	}
	if oe.Title == "" {
		return ircdb.URLPreview{}, false
	}
	desc := oe.AuthorName
	if desc != "" {
		desc = fmt.Sprintf("by %s", oe.AuthorName)
	}
	provider := oe.ProviderName
	if provider == "" {
		provider = "YouTube"
	}
	return ircdb.URLPreview{
		URL:         target,
		Kind:        ircdb.PreviewKindOpenGraph,
		Title:       oe.Title,
		Description: desc,
		ImageURL:    oe.ThumbnailURL,
		SiteName:    provider,
		Width:       oe.Width,
		Height:      oe.Height,
		Mime:        "text/html",
	}, true
}
