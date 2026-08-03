package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadEndpointStoresFileAndServesIt(t *testing.T) {
	uploadDir := t.TempDir()
	srv := &Server{Uploads: UploadConfig{Dir: uploadDir, MaxBytes: 1 << 20}}
	h := srv.Handler()

	imgData := encodePNG(t, 4, 3)
	body, contentType := multipartBody(t, "file", "hello.png", imgData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		URL    string `json:"url"`
		Mime   string `json:"mime"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Bytes  int    `json:"bytes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// No BaseURL override configured, so the URL is made absolute from the
	// request host (httptest defaults it to example.com).
	if !strings.HasPrefix(resp.URL, "http://example.com/uploads/") {
		t.Fatalf("url = %q", resp.URL)
	}
	if resp.Mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", resp.Mime)
	}
	if resp.Width != 4 || resp.Height != 3 {
		t.Fatalf("dims = %dx%d, want 4x3", resp.Width, resp.Height)
	}
	name := filepath.Base(resp.URL)
	data, err := os.ReadFile(filepath.Join(uploadDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, imgData) {
		t.Fatalf("stored content mismatch: got %d bytes, want %d bytes", len(data), len(imgData))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, resp.URL, http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), imgData) {
		t.Fatalf("served content mismatch")
	}
}

func TestUploadEndpointRejectsOversizeFiles(t *testing.T) {
	srv := &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 4}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "big.txt", []byte("12345"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadEndpointUsesBaseURLOverride(t *testing.T) {
	srv := &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 1 << 20, BaseURL: "https://files.example/u"}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "hello.png", encodePNG(t, 2, 2))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.URL, "https://files.example/u/") {
		t.Fatalf("url = %q", resp.URL)
	}
}

func TestUploadEndpointJPEGReturnsImageMime(t *testing.T) {
	srv := &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 1 << 20}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "hello.jpg", encodeJPEG(t, 800, 600))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Mime string `json:"mime"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", resp.Mime)
	}
}

func TestUploadEndpointRejectsNonImagePayload(t *testing.T) {
	srv := &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 1 << 20}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "nope.bin", []byte("nope"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d body=%s, want 415", rec.Code, rec.Body.String())
	}
}

func multipartBody(t *testing.T, fieldName, filename string, data []byte) (body *bytes.Buffer, contentType string) {
	t.Helper()
	body = &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return body, mw.FormDataContentType()
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
