package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const uploadMultipartSlop = 1 << 20

var uploadExtRe = regexp.MustCompile(`^[a-z0-9]{1,12}$`)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads.Dir == "" || s.Uploads.MaxBytes <= 0 {
		http.Error(w, "uploads disabled", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.Uploads.MaxBytes+uploadMultipartSlop)
	file, header, err := readUploadFormFile(r)
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

	mkdirErr := os.MkdirAll(s.Uploads.Dir, 0o755)
	if mkdirErr != nil {
		http.Error(w, "create upload dir: "+mkdirErr.Error(), http.StatusInternalServerError)
		return
	}

	name, err := newUploadName(header.Filename)
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

	n, copyErr := io.Copy(dst, io.LimitReader(file, s.Uploads.MaxBytes+1))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		http.Error(w, "save upload: "+copyErr.Error(), http.StatusInternalServerError)
		return
	}
	if closeErr != nil {
		_ = os.Remove(path)
		http.Error(w, "close upload file: "+closeErr.Error(), http.StatusInternalServerError)
		return
	}
	if n > s.Uploads.MaxBytes {
		_ = os.Remove(path)
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"url": s.uploadURL(name)})
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

func newUploadName(original string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := hex.EncodeToString(buf)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(original), "."))
	if uploadExtRe.MatchString(ext) {
		name += "." + ext
	}
	return name, nil
}
