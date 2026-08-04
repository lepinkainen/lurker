package media

import "net/http"

// Config controls local file upload storage and returned URLs. Phase 1 is
// local-disk only; a later phase adds a bucket-backed Store implementation
// without changing this shape's handler-facing seams (storeVariant,
// deleteVariant, uploadURL).
type Config struct {
	Dir      string
	MaxBytes int64
	BaseURL  string
}

// Service bundles the dependencies the media HTTP handlers need: a metadata
// Store and local-disk storage config.
type Service struct {
	Store Store
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
