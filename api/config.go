package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) configYAMLPreview(w http.ResponseWriter, r *http.Request) {
	if s.ConfigPreview == nil {
		http.Error(w, "config export not configured", http.StatusNotImplemented)
		return
	}
	current, proposed, err := s.ConfigPreview(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"current":  current,
		"proposed": proposed,
	})
}

func (s *Server) configYAMLSave(w http.ResponseWriter, r *http.Request) {
	if s.ConfigSave == nil {
		http.Error(w, "config export not configured", http.StatusNotImplemented)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.ConfigSave(r.Context(), body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
