package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const uploadMultipartSlop = 1 << 20 // 1 MiB

func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	if s.Blobs == nil || s.Cfg.MaxBytes <= 0 {
		http.Error(w, "uploads not configured", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxBytes+uploadMultipartSlop)
	file, _, err := readUploadFormFile(r)
	if err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr), errors.Is(err, multipart.ErrMessageTooLarge):
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, s.Cfg.MaxBytes+1))
	if err != nil {
		http.Error(w, "read upload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if int64(len(data)) > s.Cfg.MaxBytes {
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}

	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	if classifyMedia(http.DetectContentType(sniff)) == mediaKindVideo {
		slog.Info("upload rejected", "reason", "video not yet supported", "bytes", len(data))
		http.Error(w, "video not yet supported", http.StatusUnsupportedMediaType)
		return
	}
	// mediaKindImage and mediaKindUnknown both fall through to optimizeImage,
	// which is the real validation gate: http.DetectContentType does not
	// reliably recognize WebP, so we don't want to hard-reject on the sniff
	// alone. optimizeImage's decode attempt covers HEIC, unrecognized, and
	// non-image payloads.

	ctx := r.Context()
	sourceHash := hex.EncodeToString(sha256Sum(data))
	existing, dedupHit, lookupErr := s.Store.GetBySourceHash(ctx, sourceHash)
	if lookupErr != nil {
		http.Error(w, "lookup upload: "+lookupErr.Error(), http.StatusInternalServerError)
		return
	}
	if dedupHit {
		slog.Info("upload dedup hit", "id", existing.ID, "source_hash", sourceHash, "bytes", len(data))
		s.writeUploadResponse(w, r, existing, http.StatusCreated)
		return
	}

	optimized, err := optimizeImage(data)
	if err != nil {
		slog.Info("upload rejected", "reason", err.Error(), "bytes", len(data),
			"sniffed", http.DetectContentType(sniff))
		http.Error(w, "unsupported media type: "+err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	m, key, err := s.allocateAndStore(ctx, sourceHash, optimized)
	if err != nil {
		switch {
		case errors.Is(err, errUploadIDExhausted):
			slog.Error("upload id allocation failed", "attempts", maxUploadIDAttempts)
			http.Error(w, "could not allocate a unique upload id", http.StatusInternalServerError)
		case errors.Is(err, errBlobBackend):
			// Storage backend is broken (bad credentials, bucket gone, network
			// down). Nothing was recorded, and we never silently divert the
			// bytes somewhere else.
			slog.Error("upload storage backend failed", "err", err, "bytes", len(optimized.Data))
			http.Error(w, "storage backend unavailable: "+err.Error(), http.StatusBadGateway)
		default:
			http.Error(w, "save upload: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	id := m.ID

	url := s.uploadURL(r, key)
	slog.Info("upload stored", "id", id, "key", key, "mime", optimized.Mime,
		"width", optimized.Width, "height", optimized.Height,
		"in_bytes", len(data), "out_bytes", len(optimized.Data), "url", url)
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":    url,
		"id":     id,
		"mime":   optimized.Mime,
		"width":  optimized.Width,
		"height": optimized.Height,
		"bytes":  len(optimized.Data),
	})
}

// writeUploadResponse writes the JSON body describing a stored record, reusing
// its primary ("main") variant. Shared by the upload dedup hit (201) and the
// client-side dedup precheck endpoint (200).
func (s *Service) writeUploadResponse(w http.ResponseWriter, r *http.Request, m Media, status int) {
	if len(m.Variants) == 0 {
		http.Error(w, "stored media has no variants", http.StatusInternalServerError)
		return
	}
	primary := m.Variants[0]
	writeJSON(w, status, map[string]any{
		"url":    s.uploadURL(r, primary.Key),
		"id":     m.ID,
		"mime":   m.Mime,
		"width":  m.Width,
		"height": m.Height,
		"bytes":  m.Size,
	})
}

// serveUpload serves a variant from local disk. Only the disk backend sets
// Cfg.ServeDir; with object storage the bytes live in the bucket and are
// fetched from the CDN instead, so this route 404s.
func (s *Service) serveUpload(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.ServeDir == "" {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "" || name != r.PathValue("name") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.Cfg.ServeDir, name))
}

// uploadURL builds the URL returned to the client for a stored upload. When
// Cfg.BaseURL is set it wins — that is the CDN's public base for the object
// storage backend, where the bytes are only reachable by that URL. Otherwise
// (disk backend, no base URL) the URL is made absolute from the incoming
// request so the pasted link resolves for other IRC clients: a relative
// "/uploads/..." is useless once it leaves this origin. key is the stored
// "<id>.ext" name.
func (s *Service) uploadURL(r *http.Request, key string) string {
	if s.Cfg.BaseURL != "" {
		return strings.TrimRight(s.Cfg.BaseURL, "/") + "/" + key
	}
	if !validUploadHost(r.Host) {
		// Host header is untrustworthy (spaces, CRLF, quotes, ...); fall back
		// to a relative URL rather than embed it in an absolute one. Still
		// resolves fine for clients on the same origin.
		return "/uploads/" + key
	}
	return uploadScheme(r) + "://" + r.Host + "/uploads/" + key
}

// uploadHostRe matches a plausible host[:port] (including bracketed IPv6),
// rejecting spaces, quotes, CRLF, or other characters that could break out of
// a URL when embedded verbatim.
var uploadHostRe = regexp.MustCompile(`^[A-Za-z0-9.\-:\[\]]+$`)

// validUploadHost reports whether h is safe to embed in a returned URL.
func validUploadHost(h string) bool { return h != "" && uploadHostRe.MatchString(h) }

// uploadScheme picks http/https for request-derived upload URLs, honoring a
// reverse proxy's X-Forwarded-Proto (Tailscale serve / TLS terminators) before
// falling back to the connection's own TLS state. Only "http" or "https" are
// accepted from the header — anything else (e.g. a spoofed "javascript") is
// ignored so it can't smuggle a dangerous scheme into a pasted link.
func uploadScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		if i := strings.IndexByte(proto, ','); i >= 0 {
			proto = proto[:i]
		}
		if proto = strings.ToLower(strings.TrimSpace(proto)); proto == "http" || proto == "https" {
			return proto
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func readUploadFormFile(r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	if err := r.ParseMultipartForm(uploadMultipartSlop); err != nil {
		return nil, nil, err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, fmt.Errorf("missing multipart file field %q", "file")
	}
	if header.Filename == "" {
		return nil, nil, fmt.Errorf("upload filename is required")
	}
	return file, header, nil
}

// maxUploadIDAttempts bounds how many random IDs we try before giving up on
// finding an unused one. With a 10-char base62 id (~8.4e17 keyspace) a
// collision is astronomically unlikely; the retry loop just makes uniqueness
// provable rather than merely probable.
const maxUploadIDAttempts = 5

var errUploadIDExhausted = errors.New("exhausted upload id attempts")

// errBlobBackend marks a failure that came from the blob backend rather than
// from the request or the metadata store. The handler maps it to 502: the
// upload was fine, the storage backend was not.
var errBlobBackend = errors.New("media storage backend")

// allocateAndStore picks an unused media id, stores the optimized bytes under
// "<id><ext>", and inserts the metadata row — retrying on the (vanishingly
// rare) id collision. It returns the stored record and its primary variant
// key. Cleanup on a failed insert removes the just-stored blob so a failure
// never leaves an orphan.
func (s *Service) allocateAndStore(ctx context.Context, sourceHash string, opt optimizedImage) (Media, string, error) {
	for range maxUploadIDAttempts {
		id, err := newMediaID()
		if err != nil {
			return Media{}, "", err
		}
		if _, exists, getErr := s.Store.Get(ctx, id); getErr != nil {
			return Media{}, "", getErr
		} else if exists {
			continue // id already taken; draw another
		}
		key := id + opt.Ext
		// The Store.Get check above covers ids this instance recorded; Exists
		// also catches a blob left behind by an interrupted upload.
		if taken, existsErr := s.Blobs.Exists(ctx, key); existsErr != nil {
			return Media{}, "", fmt.Errorf("%w: %w", errBlobBackend, existsErr)
		} else if taken {
			continue
		}
		if storeErr := s.Blobs.Put(ctx, key, opt.Data, opt.Mime); storeErr != nil {
			if errors.Is(storeErr, os.ErrExist) {
				continue // lost a race for this key; draw another id
			}
			return Media{}, "", fmt.Errorf("%w: %w", errBlobBackend, storeErr)
		}
		m := Media{
			ID:         id,
			Kind:       "image",
			Mime:       opt.Mime,
			SourceHash: sourceHash,
			Variants: []Variant{{
				Name:   "main",
				Key:    key,
				Mime:   opt.Mime,
				Width:  opt.Width,
				Height: opt.Height,
				Bytes:  len(opt.Data),
			}},
			Size:   len(opt.Data),
			Width:  opt.Width,
			Height: opt.Height,
		}
		if insErr := s.Store.Insert(ctx, m); insErr != nil {
			_ = s.Blobs.Delete(ctx, key)
			return Media{}, "", insErr
		}
		return m, key, nil
	}
	return Media{}, "", errUploadIDExhausted
}

// mediaIDLen is the length of a generated media id in base62 characters.
const mediaIDLen = 10

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newMediaID generates a random URL- and filename-safe base62 media id, used
// as the stored key stem ("<id><ext>"). It uses rejection sampling so the
// alphabet maps onto random bytes without modulo bias.
func newMediaID() (string, error) {
	// 256 % 62 == 8, so bytes >= 248 would bias the low residues; reject them.
	const rejectFrom = 256 - (256 % len(base62Alphabet))
	out := make([]byte, mediaIDLen)
	buf := make([]byte, 1)
	for i := 0; i < mediaIDLen; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if int(buf[0]) >= rejectFrom {
			continue
		}
		out[i] = base62Alphabet[int(buf[0])%len(base62Alphabet)]
		i++
	}
	return string(out), nil
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}
