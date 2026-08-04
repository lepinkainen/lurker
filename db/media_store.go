package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lepinkainen/lurker/db/internal/mediadb"
	"github.com/lepinkainen/lurker/media"
)

// MediaStore wraps the media.db handle and implements media.Store. It is
// the only piece of the db package that knows about the media package;
// media itself never imports db (see media/store.go).
type MediaStore struct {
	DB *sql.DB
	// Now is the clock used for CreatedAt/UpdatedAt stamping. Tests inject a
	// fixed clock; production code leaves it nil and the store falls back
	// to time.Now.
	Now func() time.Time
}

func (s *MediaStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// OpenMediaStore opens the media metadata database at path.
func OpenMediaStore(path string) (*MediaStore, error) {
	d, err := OpenMedia(path)
	if err != nil {
		return nil, err
	}
	return &MediaStore{DB: d}, nil
}

// Close closes the underlying SQLite handle.
func (s *MediaStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func parseMediaTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func rowToMedia(r mediadb.Medium) (media.Media, error) {
	var variants []media.Variant
	if err := json.Unmarshal([]byte(r.Variants), &variants); err != nil {
		return media.Media{}, err
	}
	return media.Media{
		ID:         r.ID,
		Kind:       r.Kind,
		Mime:       r.Mime,
		SourceHash: r.SourceHash,
		Variants:   variants,
		Size:       int(r.Size),
		Width:      int(r.Width),
		Height:     int(r.Height),
		CreatedAt:  parseMediaTime(r.CreatedAt),
		UpdatedAt:  parseMediaTime(r.UpdatedAt),
	}, nil
}

// GetBySourceHash looks up a media record by its content hash, the basis
// for upload dedup.
func (s *MediaStore) GetBySourceHash(ctx context.Context, hash string) (media.Media, bool, error) {
	r, err := mediadb.New(s.DB).GetMediaBySourceHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Media{}, false, nil
	}
	if err != nil {
		return media.Media{}, false, err
	}
	m, err := rowToMedia(r)
	if err != nil {
		return media.Media{}, false, err
	}
	return m, true, nil
}

// Get looks up a media record by ID.
func (s *MediaStore) Get(ctx context.Context, id string) (media.Media, bool, error) {
	r, err := mediadb.New(s.DB).GetMedia(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Media{}, false, nil
	}
	if err != nil {
		return media.Media{}, false, err
	}
	m, err := rowToMedia(r)
	if err != nil {
		return media.Media{}, false, err
	}
	return m, true, nil
}

// Insert stores a new media record. CreatedAt/UpdatedAt are stamped with the
// store's clock when zero.
func (s *MediaStore) Insert(ctx context.Context, m media.Media) error {
	now := s.now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	variants, err := json.Marshal(m.Variants)
	if err != nil {
		return err
	}
	return mediadb.New(s.DB).InsertMedia(ctx, mediadb.InsertMediaParams{
		ID:         m.ID,
		Kind:       m.Kind,
		Mime:       m.Mime,
		SourceHash: m.SourceHash,
		Variants:   string(variants),
		Size:       int64(m.Size),
		Width:      int64(m.Width),
		Height:     int64(m.Height),
		CreatedAt:  m.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:  m.UpdatedAt.Format(time.RFC3339Nano),
	})
}

// List returns a page of media records ordered by newest first, honoring
// f.Kind (exact match) and f.Q (substring match against id/mime).
//
// Kept on raw database/sql because the WHERE clause is built dynamically
// from the filter, mirroring PreviewStore.GetMany.
func (s *MediaStore) List(ctx context.Context, f media.ListFilter, limit, offset int) ([]media.Media, int, error) {
	var where []string
	var args []any
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Q != "" {
		where = append(where, "(id LIKE ? OR mime LIKE ?)")
		like := "%" + f.Q + "%"
		args = append(args, like, like)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM media %s`, whereClause)
	if err := s.DB.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQ := fmt.Sprintf(
		`SELECT id, kind, mime, source_hash, variants, size, width, height, created_at, updated_at
		 FROM media %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		whereClause)
	listArgs := append(append([]any{}, args...), int64(limit), int64(offset))
	rows, err := s.DB.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]media.Media, 0, limit)
	for rows.Next() {
		var r mediadb.Medium
		if err := rows.Scan(&r.ID, &r.Kind, &r.Mime, &r.SourceHash, &r.Variants, &r.Size, &r.Width, &r.Height,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		m, err := rowToMedia(r)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Delete removes a media record by ID. It does not delete the underlying
// stored variant bytes; callers own that (see media.Service.deleteVariant).
func (s *MediaStore) Delete(ctx context.Context, id string) (bool, error) {
	n, err := mediadb.New(s.DB).DeleteMedia(ctx, id)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
