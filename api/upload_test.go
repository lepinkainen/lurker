package api

import (
	"bytes"
	"encoding/json"
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
	srv := &Server{Uploads: UploadConfig{Dir: uploadDir, MaxBytes: 1024}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "hello.txt", []byte("hello upload"))
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
	if !strings.HasPrefix(resp.URL, "/uploads/") {
		t.Fatalf("url = %q", resp.URL)
	}
	name := strings.TrimPrefix(resp.URL, "/uploads/")
	data, err := os.ReadFile(filepath.Join(uploadDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello upload" {
		t.Fatalf("stored content = %q", string(data))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, resp.URL, http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello upload" {
		t.Fatalf("served content = %q", rec.Body.String())
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
	srv := &Server{Uploads: UploadConfig{Dir: t.TempDir(), MaxBytes: 1024, BaseURL: "https://files.example/u"}}
	h := srv.Handler()

	body, contentType := multipartBody(t, "file", "hello.txt", []byte("hello upload"))
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
