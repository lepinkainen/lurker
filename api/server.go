// Package api is the HTTP + WebSocket surface consumed by clients. It
// owns no IRC state itself; all reads come from the SQLite store and all
// outbound IRC writes go through irc.Manager.
package api

import (
	"io/fs"
	"net/http"

	ircdb "github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/hub"
	"github.com/lepinkainen/research/irc-service/irc"
)

// Server bundles the dependencies every API handler needs.
type Server struct {
	Stores  *ircdb.MultiStore
	Hub     *hub.Hub
	Manager *irc.Manager
	Web     fs.FS // embedded web UI; nil disables serving
}

// Handler returns an http.Handler with all routes wired. Route pattern
// syntax uses Go 1.22+ ServeMux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/buffers/{id}/history", s.history)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("POST /api/networks", s.createNetwork)
	mux.HandleFunc("PATCH /api/networks/{id}", s.patchNetwork)
	mux.HandleFunc("DELETE /api/networks/{id}", s.deleteNetwork)
	mux.HandleFunc("POST /api/networks/{id}/connect", s.connectNetwork)
	mux.HandleFunc("POST /api/networks/{id}/disconnect", s.disconnectNetwork)

	if s.Web != nil {
		mux.Handle("GET /", http.FileServer(http.FS(s.Web)))
	}
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Stores.Control.PingContext(r.Context()); err != nil {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}
