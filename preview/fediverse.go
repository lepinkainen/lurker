package preview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// maxAPBytes caps the AP JSON body. Notes are small; cap defensively.
const maxAPBytes = 128 * 1024

// maxDescriptionRunes truncates the stripped post body. Long enough for a
// typical Mastodon toot (500 chars), generous for Pleroma/Akkoma which allow
// more, but bounded so WS payloads stay sane.
const maxDescriptionRunes = 2000

// activityPubNote is the subset of an AP `Note` we care about. Servers vary;
// only `content` and `attachment` are widely populated.
type activityPubNote struct {
	Type       string `json:"type"`
	Content    string `json:"content"`
	Attachment []struct {
		Type      string `json:"type"`
		MediaType string `json:"mediaType"`
		URL       string `json:"url"`
	} `json:"attachment"`
}

// fediverseResult holds enrichment data extracted from an AP object.
type fediverseResult struct {
	content string // plain-text post body (HTML stripped)
	image   string // first image-ish attachment URL
}

// fetchActivityPub GETs an AP object with content negotiation, returning a
// fediverseResult and ok=true when at least `content` was extracted. SSRF is
// re-checked against the AP href since it may differ from the page URL.
func (f *Fetcher) fetchActivityPub(ctx context.Context, apURL string) (fediverseResult, bool) {
	if err := f.cfg.SSRFCheck(ctx, apURL); err != nil {
		return fediverseResult{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apURL, http.NoBody)
	if err != nil {
		return fediverseResult{}, false
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "application/activity+json, application/ld+json;profile=\"https://www.w3.org/ns/activitystreams\"")
	resp, err := f.client.Do(req)
	if err != nil {
		return fediverseResult{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fediverseResult{}, false
	}
	var note activityPubNote
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPBytes)).Decode(&note); err != nil {
		return fediverseResult{}, false
	}

	out := fediverseResult{}
	if text := htmlToText(note.Content); text != "" {
		out.content = truncateRunes(text, maxDescriptionRunes)
	}
	for _, a := range note.Attachment {
		if strings.HasPrefix(strings.ToLower(a.MediaType), "image/") && a.URL != "" {
			out.image = a.URL
			break
		}
	}
	return out, out.content != ""
}

// htmlToText strips tags from a snippet of HTML, preserving paragraph and line
// breaks. Whitespace is collapsed within blocks; blocks are joined by "\n\n".
// Hashtag/mention links are kept as their visible text (e.g. "#meshtastic").
func htmlToText(in string) string {
	if in == "" {
		return ""
	}
	z := html.NewTokenizer(strings.NewReader(in))
	var blocks []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(collapseWS(cur.String()))
		cur.Reset()
		if s != "" {
			blocks = append(blocks, s)
		}
	}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				flush()
				return strings.Join(blocks, "\n\n")
			}
			flush()
			return strings.Join(blocks, "\n\n")
		case html.TextToken:
			cur.WriteString(string(z.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "br" {
				cur.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "p" || tag == "div" || tag == "li" {
				flush()
			}
		}
	}
}

// collapseWS collapses runs of whitespace (except newlines) to a single space.
// Newlines are preserved so <br> stays meaningful.
func collapseWS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteRune(r)
			prevSpace = false
		case ' ', '\t', '\r', '\f', '\v', ' ':
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// truncateRunes returns s truncated to at most n runes, appending an ellipsis
// when truncation occurred.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
