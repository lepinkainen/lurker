package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

const (
	maxHighlightPatterns      = 100
	maxHighlightPatternLength = 64
)

type highlightsRequest struct {
	Patterns []string `json:"patterns"`
}

// highlightsEvent notifies connected clients that the global highlight
// pattern list changed (parity with the buffer_settings event).
type highlightsEvent struct {
	Type     string   `json:"type"`
	Patterns []string `json:"patterns"`
}

func (s *Server) getHighlights(w http.ResponseWriter, r *http.Request) {
	patterns, err := ircdb.ListHighlights(r.Context(), s.Stores.Control)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, highlightsRequest{Patterns: patterns})
}

func (s *Server) putHighlights(w http.ResponseWriter, r *http.Request) {
	var req highlightsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	patterns, err := normalizeHighlightPatterns(req.Patterns)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = ircdb.SetHighlights(r.Context(), s.Stores.Control, patterns); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patterns, err = ircdb.ListHighlights(r.Context(), s.Stores.Control)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	irc.SetHighlightPatterns(patterns)
	if s.Hub != nil {
		s.Hub.Publish(highlightsEvent{Type: "highlights", Patterns: patterns})
	}
	writeJSON(w, http.StatusOK, highlightsRequest{Patterns: patterns})
}

// normalizeHighlightPatterns trims, drops empties, dedupes case-insensitively
// and enforces size limits.
func normalizeHighlightPatterns(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > maxHighlightPatternLength {
			return nil, fmt.Errorf("pattern too long (max %d chars): %q", maxHighlightPatternLength, p)
		}
		key := strings.ToLower(p)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	if len(out) > maxHighlightPatterns {
		return nil, fmt.Errorf("too many patterns (max %d)", maxHighlightPatterns)
	}
	return out, nil
}
