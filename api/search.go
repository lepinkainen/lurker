package api

import "net/http"

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}

	networkID, ok := parseOptionalQueryInt64(w, r, "network", "bad network")
	if !ok {
		return
	}
	bufferID, ok := parseOptionalQueryInt64(w, r, "buffer", "bad buffer")
	if !ok {
		return
	}

	results, err := s.Stores.Search(r.Context(), q, networkID, bufferID, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"network": networkID,
		"buffer":  bufferID,
		"results": toMessageDTOs(results),
	})
}
