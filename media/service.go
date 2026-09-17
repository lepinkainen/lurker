package media

import "net/http"

// Config controls upload limits and the URLs handed back to clients. Where
// the bytes actually land is decided by the injected Blobs implementation,
// not here.
type Config struct {
	MaxBytes int64
	// BaseURL is the prefix returned URLs are built from — the CDN's public
	// base for the object-storage backend, or an optional override for the
	// disk backend (empty means derive it from the request).
	BaseURL string
	// ServeDir is the directory GET /uploads/{name} serves from. Set only for
	// the disk backend; empty means that route 404s.
	ServeDir string
}

// Service bundles the dependencies the media HTTP handlers need: a metadata
// Store, a blob backend, and upload limits. A nil Blobs disables uploads
// (every write path 404s) — it never means "fall back to local disk".
type Service struct {
	Store Store
	Blobs Blobs
	Cfg   Config
}

// Handler returns an http.Handler with the media routes wired, for
// standalone use (e.g. tests). In the main binary, RegisterRoutes is used
// directly against the api.Server's shared mux instead.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return mux
}

// RegisterRoutes wires the media routes onto mux. Route pattern syntax uses
// Go 1.22+ ServeMux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/upload", s.upload)
	mux.HandleFunc("GET /uploads/{name}", s.serveUpload)
	mux.HandleFunc("GET /api/media", s.listMedia)
	mux.HandleFunc("GET /api/media/exists", s.existsMedia)
	mux.HandleFunc("DELETE /api/media/{id}", s.deleteMedia)
}
