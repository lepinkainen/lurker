package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/preview"
)

// avatarManager is the IRC surface the avatar proxy needs: resolving a
// nick's IRCv3 metadata avatar URL. The URL itself never reaches the
// browser — the handler fetches it server-side through preview's
// SSRF-guarded client and serves only the decoded bytes.
type avatarManager interface {
	AvatarURL(networkID uuid.UUID, nick string) (string, bool)
}

// avatarSizes is the allow-list of pixel sizes /api/avatar accepts. Bounding
// the value keeps the cache key space small and blocks absurd {size}
// substitutions; requests clamp to the nearest allowed size.
var avatarSizes = []int{16, 32, 64, 128, 256}

const (
	defaultAvatarSize = 64
	avatarCacheTTL    = time.Hour
)

// avatarCacheEntry is one cached fetch, keyed by the resolved (post-size-
// substitution) URL.
type avatarCacheEntry struct {
	data      []byte
	mime      string
	expiresAt time.Time
}

// avatarCache is a small mutex-guarded, TTL-expiring cache for proxied
// avatar bytes. One Server owns one avatarCache; it is not shared globally.
type avatarCache struct {
	mu      sync.Mutex
	entries map[string]avatarCacheEntry
}

func newAvatarCache() *avatarCache {
	return &avatarCache{entries: map[string]avatarCacheEntry{}}
}

func (c *avatarCache) get(key string) (avatarCacheEntry, bool) {
	if c == nil {
		return avatarCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return avatarCacheEntry{}, false
	}
	return e, true
}

func (c *avatarCache) put(key string, e avatarCacheEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = e
}

// avatar handles GET /api/avatar?network=<uuid>&nick=<nick>&size=<px>. It
// looks up nick's IRCv3 metadata avatar URL on the given network, substitutes
// a literal "{size}" token in the URL (many avatar URL templates carry one)
// with the clamped pixel size, fetches the image through preview's
// SSRF-guarded client, and serves the bytes from a small in-memory cache
// keyed by the resolved URL. The browser never talks to the remote host.
func (s *Server) avatar(w http.ResponseWriter, r *http.Request) {
	networkID, ok := parseOptionalQueryUUID(w, r, "network", "bad network id")
	if !ok {
		return
	}
	nick := r.URL.Query().Get("nick")
	if networkID == uuid.Nil || nick == "" {
		http.Error(w, "network and nick are required", http.StatusBadRequest)
		return
	}

	am, ok := s.Manager.(avatarManager)
	if !ok || am == nil {
		http.NotFound(w, r)
		return
	}
	rawURL, ok := am.AvatarURL(networkID, nick)
	if !ok {
		http.NotFound(w, r)
		return
	}

	size := clampAvatarSize(r.URL.Query().Get("size"))
	resolvedURL := strings.ReplaceAll(rawURL, "{size}", strconv.Itoa(size))

	if entry, ok := s.avatarCache.get(resolvedURL); ok {
		serveAvatar(w, entry)
		return
	}

	if s.PreviewFetcher == nil {
		http.NotFound(w, r)
		return
	}
	data, mime, err := s.PreviewFetcher.FetchImage(r.Context(), resolvedURL)
	if err != nil {
		switch {
		case errors.Is(err, preview.ErrBlocked):
			http.Error(w, "avatar blocked", http.StatusNotFound)
		case errors.Is(err, preview.ErrNotImage):
			http.Error(w, "avatar not an image", http.StatusBadGateway)
		default:
			http.Error(w, "avatar fetch failed", http.StatusBadGateway)
		}
		return
	}

	entry := avatarCacheEntry{data: data, mime: mime, expiresAt: time.Now().Add(avatarCacheTTL)}
	s.avatarCache.put(resolvedURL, entry)
	serveAvatar(w, entry)
}

func serveAvatar(w http.ResponseWriter, e avatarCacheEntry) {
	w.Header().Set("Content-Type", e.mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(e.data)
}

// clampAvatarSize maps raw to the nearest entry in avatarSizes, defaulting to
// defaultAvatarSize when raw is missing or unparseable.
func clampAvatarSize(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultAvatarSize
	}
	best := avatarSizes[0]
	bestDiff := absInt(n - best)
	for _, sz := range avatarSizes[1:] {
		if d := absInt(n - sz); d < bestDiff {
			best, bestDiff = sz, d
		}
	}
	return best
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
