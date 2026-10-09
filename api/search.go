package api

import "net/http"

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}

	networkID, ok := parseOptionalQueryUUID(w, r, "network", "bad network")
	if !ok {
		return
	}
	bufferID, ok := parseOptionalQueryUUID(w, r, "buffer", "bad buffer")
	if !ok {
		return
	}
	before, ok := parseOptionalQueryUUID(w, r, "before", "bad before")
	if !ok {
		return
	}

	limit := clampLimit(r.URL.Query().Get("limit"), 50, 200)
	results, err := s.Stores.Search(r.Context(), q, networkID, bufferID, before, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"network": networkID,
		"buffer":  bufferID,
		"results": s.toMessageDTOs(r.Context(), results),
	})
}
