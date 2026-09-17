// Package media is a self-contained image upload + storage service: HTTP
// handlers, optimization, and a small Store interface for metadata
// persistence. It is deliberately detachable — it must not import the
// repo's hub, irc, api, or db packages, so it (and its metadata store) can
// be pulled into a separate process/container later without unwinding
// dependencies.
package media

import (
	"context"
	"time"
)

// Variant is one stored rendition of a media item (currently always a
// single "main" variant per upload; later phases may add thumbnails).
type Variant struct {
	Name   string
	Key    string
	Mime   string
	Width  int
	Height int
	Bytes  int
}

// Media is the metadata record for one uploaded item, keyed by SourceHash
// for dedup.
type Media struct {
	ID         string
	Kind       string
	Mime       string
	SourceHash string
	Variants   []Variant
	Size       int
	Width      int
	Height     int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ListFilter narrows List results. Phase 1 leaves Q/Kind unimplemented; see
// db.MediaStore.List.
type ListFilter struct {
	Q    string
	Kind string
}

// Store persists Media metadata. The concrete implementation (db.MediaStore)
// lives in the db package and owns its own media.db file; media never
// imports db, so this interface is the only coupling between the two.
type Store interface {
	GetBySourceHash(ctx context.Context, hash string) (Media, bool, error)
	Get(ctx context.Context, id string) (Media, bool, error)
	Insert(ctx context.Context, m Media) error
	List(ctx context.Context, f ListFilter, limit, offset int) (items []Media, total int, err error)
	Delete(ctx context.Context, id string) (bool, error)
}
