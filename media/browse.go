package media

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListLimit = 60
	maxListLimit     = 200
)

var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// existsMedia handles GET /api/media/exists?hash=<sha256hex>, the server side
// of client-side dedup: a client hashes the exact bytes it would POST and asks
// whether they are already stored. A hit (200) lets the client skip the upload
// entirely and reuse the returned URL; a miss (404) means "go ahead and POST".
// The upload path still dedups on its own, so this is a bandwidth optimization,
// not the source of truth.
func (s *Service) existsMedia(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("hash")))
	if !sha256HexRe.MatchString(hash) {
		http.Error(w, "invalid or missing hash (want 64-char hex sha-256)", http.StatusBadRequest)
		return
	}
	m, ok, err := s.Store.GetBySourceHash(r.Context(), hash)
	if err != nil {
		http.Error(w, "lookup media: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.writeUploadResponse(w, r, m, http.StatusOK)
}

// mediaListItem is the JSON shape for one row in the GET /api/media
// response. url is built from the item's primary ("main") variant, the
// same key uploadURL uses for the upload response.
type mediaListItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Mime      string `json:"mime"`
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Bytes     int    `json:"bytes"`
	CreatedAt string `json:"created_at"`
}

// listMedia handles GET /api/media?q=&kind=&limit=&offset=, returning a
// page of uploaded media metadata for the admin/browse UI.
func (s *Service) listMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseListLimit(q.Get("limit"))
	offset := parseListOffset(q.Get("offset"))
	filter := ListFilter{
		Q:    q.Get("q"),
		Kind: q.Get("kind"),
	}

	items, total, err := s.Store.List(r.Context(), filter, limit, offset)
	if err != nil {
		http.Error(w, "list media: "+err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]mediaListItem, 0, len(items))
	for _, m := range items {
		url := ""
		if len(m.Variants) > 0 {
			url = s.uploadURL(r, m.Variants[0].Key)
		}
		out = append(out, mediaListItem{
			ID:        m.ID,
			Kind:      m.Kind,
			Mime:      m.Mime,
			URL:       url,
			Width:     m.Width,
			Height:    m.Height,
			Bytes:     m.Size,
			CreatedAt: m.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// deleteMedia handles DELETE /api/media/{id}: removes every stored variant
// from disk, then the metadata row. Missing variant files are tolerated
// (deleteVariant already treats os.IsNotExist as success); a real removal
// error is a 500 rather than silently orphaning the row.
func (s *Service) deleteMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	m, ok, err := s.Store.Get(ctx, id)
	if err != nil {
		http.Error(w, "get media: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	for _, v := range m.Variants {
		if delErr := s.deleteVariant(v.Key); delErr != nil {
			http.Error(w, "delete variant: "+delErr.Error(), http.StatusInternalServerError)
			return
		}
	}

	deleted, err := s.Store.Delete(ctx, id)
	if err != nil {
		http.Error(w, "delete media: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}

	slog.Info("media deleted", "id", id, "variants", len(m.Variants))
	w.WriteHeader(http.StatusNoContent)
}

// parseListLimit parses the limit query param, defaulting and clamping it
// into [1, maxListLimit].
func parseListLimit(s string) int {
	if s == "" {
		return defaultListLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultListLimit
	}
	if n > maxListLimit {
		return maxListLimit
	}
	return n
}

// parseListOffset parses the offset query param, defaulting to 0 and
// rejecting negative values.
func parseListOffset(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
