package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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

// Get returns a cached preview if present and fresher than ttl. When ttl is
// zero the entry is returned regardless of age.
func (s *PreviewStore) Get(ctx context.Context, url string, ttl time.Duration) (URLPreview, bool, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT url, kind, COALESCE(title,''), COALESCE(description,''),
		        COALESCE(image_url,''), COALESCE(site_name,''),
		        COALESCE(width,0), COALESCE(height,0), COALESCE(mime,''),
		        fetched_at, COALESCE(error,'')
		 FROM url_previews WHERE url = ?`, url)
	var p URLPreview
	var fetched string
	err := row.Scan(&p.URL, &p.Kind, &p.Title, &p.Description, &p.ImageURL, &p.SiteName,
		&p.Width, &p.Height, &p.Mime, &fetched, &p.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return URLPreview{}, false, nil
	}
	if err != nil {
		return URLPreview{}, false, err
	}
	t, parseErr := time.Parse(time.RFC3339Nano, fetched)
	if parseErr != nil {
		t, _ = time.Parse(time.RFC3339, fetched)
	}
	p.FetchedAt = t
	if ttl > 0 && !t.IsZero() && time.Since(t) > ttl {
		return p, false, nil
	}
	return p, true, nil
}

// Put upserts a preview keyed on URL.
func (s *PreviewStore) Put(ctx context.Context, p URLPreview) error {
	if p.FetchedAt.IsZero() {
		p.FetchedAt = time.Now().UTC()
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO url_previews(url, kind, title, description, image_url, site_name,
		                          width, height, mime, fetched_at, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(url) DO UPDATE SET
		   kind=excluded.kind,
		   title=excluded.title,
		   description=excluded.description,
		   image_url=excluded.image_url,
		   site_name=excluded.site_name,
		   width=excluded.width,
		   height=excluded.height,
		   mime=excluded.mime,
		   fetched_at=excluded.fetched_at,
		   error=excluded.error`,
		p.URL, p.Kind, nullableString(p.Title), nullableString(p.Description),
		nullableString(p.ImageURL), nullableString(p.SiteName),
		nullInt(p.Width), nullInt(p.Height), nullableString(p.Mime),
		p.FetchedAt.UTC().Format(time.RFC3339Nano), nullableString(p.Error),
	)
	return err
}

// GetMany returns cached previews for the given URL set. Missing URLs are
// omitted from the result map.
func (s *PreviewStore) GetMany(ctx context.Context, urls []string) (map[string]URLPreview, error) {
	out := map[string]URLPreview{}
	if len(urls) == 0 {
		return out, nil
	}
	// Deduplicate to keep the IN list tight.
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
		t, parseErr := time.Parse(time.RFC3339Nano, fetched)
		if parseErr != nil {
			t, _ = time.Parse(time.RFC3339, fetched)
		}
		p.FetchedAt = t
		out[p.URL] = p
	}
	return out, rows.Err()
}

// PurgeExpired deletes rows older than ttl. Intended for periodic
// maintenance; callers may ignore for now.
func (s *PreviewStore) PurgeExpired(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl).UTC().Format(time.RFC3339Nano)
	res, err := s.DB.ExecContext(ctx, `DELETE FROM url_previews WHERE fetched_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
