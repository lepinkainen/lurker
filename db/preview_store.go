package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lepinkainen/lurker/db/internal/previewdb"
)

// PreviewKind enumerates URL-preview outcomes.
const (
	PreviewKindImage     = "image"
	PreviewKindOpenGraph = "opengraph"
	PreviewKindNone      = "none"  // URL fetched, nothing worth previewing
	PreviewKindError     = "error" // fetch failed or blocked by SSRF guard
)

// URLPreview is the cached metadata for one URL in the shared preview DB.
type URLPreview struct {
	URL         string
	Kind        string
	Title       string
	Description string
	ImageURL    string
	SiteName    string
	Width       int
	Height      int
	Mime        string
	FetchedAt   time.Time
	Error       string
}

// Displayable reports whether the preview should be sent to clients.
func (p URLPreview) Displayable() bool {
	return p.Kind == PreviewKindImage || p.Kind == PreviewKindOpenGraph
}

// PreviewStore wraps the shared previews.db handle.
type PreviewStore struct {
	DB *sql.DB
	// Now is the clock used for FetchedAt stamping and TTL comparisons.
	// Tests inject a fixed clock; production code leaves it nil and the
	// store falls back to time.Now.
	Now func() time.Time
}

func (s *PreviewStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// OpenPreviewStore opens the shared preview cache at path.
func OpenPreviewStore(path string) (*PreviewStore, error) {
	d, err := OpenPreviews(path)
	if err != nil {
		return nil, err
	}
	return &PreviewStore{DB: d}, nil
}

// Close closes the underlying SQLite handle.
func (s *PreviewStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func parseFetchedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func rowToPreview(r previewdb.GetURLPreviewRow) URLPreview {
	return URLPreview{
		URL:         r.Url,
		Kind:        r.Kind,
		Title:       r.Title,
		Description: r.Description,
		ImageURL:    r.ImageUrl,
		SiteName:    r.SiteName,
		Width:       int(r.Width),
		Height:      int(r.Height),
		Mime:        r.Mime,
		FetchedAt:   parseFetchedAt(r.FetchedAt),
		Error:       r.Error,
	}
}

// Get returns a cached preview if present and fresher than ttl. When ttl is
// zero the entry is returned regardless of age.
func (s *PreviewStore) Get(ctx context.Context, url string, ttl time.Duration) (URLPreview, bool, error) {
	r, err := previewdb.New(s.DB).GetURLPreview(ctx, url)
	if errors.Is(err, sql.ErrNoRows) {
		return URLPreview{}, false, nil
	}
	if err != nil {
		return URLPreview{}, false, err
	}
	p := rowToPreview(r)
	if ttl > 0 && !p.FetchedAt.IsZero() && s.now().Sub(p.FetchedAt) > ttl {
		return p, false, nil
	}
	return p, true, nil
}

func nullInt(n int) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}

// Put upserts a preview keyed on URL.
func (s *PreviewStore) Put(ctx context.Context, p URLPreview) error {
	if p.FetchedAt.IsZero() {
		p.FetchedAt = s.now().UTC()
	}
	return previewdb.New(s.DB).UpsertURLPreview(ctx, previewdb.UpsertURLPreviewParams{
		Url:         p.URL,
		Kind:        p.Kind,
		Title:       nullableString(p.Title),
		Description: nullableString(p.Description),
		ImageUrl:    nullableString(p.ImageURL),
		SiteName:    nullableString(p.SiteName),
		Width:       nullInt(p.Width),
		Height:      nullInt(p.Height),
		Mime:        nullableString(p.Mime),
		FetchedAt:   p.FetchedAt.UTC().Format(time.RFC3339Nano),
		Error:       nullableString(p.Error),
	})
}

// GetMany returns cached previews for the given URL set. Missing URLs are
// omitted from the result map.
//
// Kept on raw database/sql because the IN-clause arity is dynamic.
func (s *PreviewStore) GetMany(ctx context.Context, urls []string) (map[string]URLPreview, error) {
	out := map[string]URLPreview{}
	if len(urls) == 0 {
		return out, nil
	}
	seen := map[string]struct{}{}
	args := make([]any, 0, len(urls))
	placeholders := make([]string, 0, len(urls))
	for _, u := range urls {
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		args = append(args, u)
		placeholders = append(placeholders, "?")
	}
	q := fmt.Sprintf(
		`SELECT url, kind, COALESCE(title,''), COALESCE(description,''),
		        COALESCE(image_url,''), COALESCE(site_name,''),
		        COALESCE(width,0), COALESCE(height,0), COALESCE(mime,''),
		        fetched_at, COALESCE(error,'')
		 FROM url_previews WHERE url IN (%s)`,
		strings.Join(placeholders, ","))
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p URLPreview
		var fetched string
		if err := rows.Scan(&p.URL, &p.Kind, &p.Title, &p.Description, &p.ImageURL, &p.SiteName,
			&p.Width, &p.Height, &p.Mime, &fetched, &p.Error); err != nil {
			return nil, err
		}
		p.FetchedAt = parseFetchedAt(fetched)
		out[p.URL] = p
	}
	return out, rows.Err()
}
