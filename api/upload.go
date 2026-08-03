package api

import (
	"crypto/rand"
	"encoding/hex"
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

var uploadExtRe = regexp.MustCompile(`^[a-z0-9]{1,12}$`)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads.Dir == "" || s.Uploads.MaxBytes <= 0 {
		http.Error(w, "uploads disabled", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.Uploads.MaxBytes+uploadMultipartSlop)
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

	data, err := io.ReadAll(io.LimitReader(file, s.Uploads.MaxBytes+1))
	if err != nil {
		http.Error(w, "read upload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if int64(len(data)) > s.Uploads.MaxBytes {
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

	optimized, err := optimizeImage(data)
	if err != nil {
		slog.Info("upload rejected", "reason", err.Error(), "bytes", len(data),
			"sniffed", http.DetectContentType(sniff))
		http.Error(w, "unsupported media type: "+err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	mkdirErr := os.MkdirAll(s.Uploads.Dir, 0o755)
	if mkdirErr != nil {
		http.Error(w, "create upload dir: "+mkdirErr.Error(), http.StatusInternalServerError)
		return
	}
	name, err := newUploadName(optimized.Ext)
	if err != nil {
		http.Error(w, "create upload name: "+err.Error(), http.StatusInternalServerError)
		return
	}
	path := filepath.Join(s.Uploads.Dir, name)
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, "create upload file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, writeErr := dst.Write(optimized.Data)
	closeErr := dst.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		http.Error(w, "save upload: "+writeErr.Error(), http.StatusInternalServerError)
		return
	}
	if closeErr != nil {
		_ = os.Remove(path)
		http.Error(w, "close upload file: "+closeErr.Error(), http.StatusInternalServerError)
		return
	}
	url := s.uploadURL(r, name)
	slog.Info("upload stored", "name", name, "mime", optimized.Mime,
		"width", optimized.Width, "height", optimized.Height,
		"in_bytes", len(data), "out_bytes", len(optimized.Data), "url", url)
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":    url,
		"mime":   optimized.Mime,
		"width":  optimized.Width,
		"height": optimized.Height,
		"bytes":  len(optimized.Data),
	})
}

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads.Dir == "" {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "" || name != r.PathValue("name") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.Uploads.Dir, name))
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

// newUploadName generates a random on-disk file name using the given
// extension (without leading dot, e.g. "jpg"). The extension comes from the
// optimizer's output format, not the client-supplied filename, so it always
// reflects what was actually written to disk.
func newUploadName(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := hex.EncodeToString(buf)
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if uploadExtRe.MatchString(ext) {
		name += "." + ext
	}
	return name, nil
}
