package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeStore is a minimal in-memory Store for exercising the upload/dedup
// flow without a real database.
type fakeStore struct {
	mu     sync.Mutex
	byHash map[string]Media
	byID   map[string]Media
}

func newFakeStore() *fakeStore {
	return &fakeStore{byHash: map[string]Media{}, byID: map[string]Media{}}
}

func (f *fakeStore) GetBySourceHash(_ context.Context, hash string) (Media, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byHash[hash]
	return m, ok, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (Media, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	return m, ok, nil
}

func (f *fakeStore) Insert(_ context.Context, m Media) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byHash[m.SourceHash] = m
	f.byID[m.ID] = m
	return nil
}

func (f *fakeStore) List(_ context.Context, filt ListFilter, limit, offset int) ([]Media, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	matched := make([]Media, 0, len(f.byID))
	for _, m := range f.byID {
		if filt.Kind != "" && m.Kind != filt.Kind {
			continue
		}
		if filt.Q != "" && !strings.Contains(m.ID, filt.Q) && !strings.Contains(m.Mime, filt.Q) {
			continue
		}
		matched = append(matched, m)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.After(matched[j].CreatedAt) })

	total := len(matched)
	if offset >= len(matched) {
		return []Media{}, total, nil
	}
	end := min(offset+limit, len(matched))
	return matched[offset:end], total, nil
}

func (f *fakeStore) Delete(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	if !ok {
		return false, nil
	}
	delete(f.byID, id)
	delete(f.byHash, m.SourceHash)
	return true, nil
}

// uploadKey extracts the "<id>.ext" key from a returned upload URL
// (everything after the last "/uploads/" segment).
func uploadKey(t *testing.T, url string) string {
	t.Helper()
	_, after, ok := strings.Cut(url, "/uploads/")
	if !ok {
		t.Fatalf("url %q does not contain /uploads/", url)
	}
	return after
}

// newDiskService builds a Service on the local-disk blob backend, the shape
// most of these tests exercise. The object-storage backend is covered by
// fakeBlobs below and by internal/objstore.
func newDiskService(dir string, maxBytes int64, baseURL string) *Service {
	return &Service{
		Store: newFakeStore(),
		Blobs: &DiskBlobs{Dir: dir},
		Cfg:   Config{MaxBytes: maxBytes, BaseURL: baseURL, ServeDir: dir},
	}
}

// fakeBlobs is an in-memory Blobs standing in for a remote backend. putErr,
// when set, makes every Put fail the way a broken bucket would.
type fakeBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	deleted []string
	putErr  error
	// existsHits makes the first N Exists calls report a hit regardless of
	// key, simulating a blob left behind by an interrupted upload.
	existsHits  int
	existsCalls int
}

func newFakeBlobs() *fakeBlobs {
	return &fakeBlobs{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeBlobs) Put(_ context.Context, key string, data []byte, contentType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	if _, dup := f.objects[key]; dup {
		return fmt.Errorf("put %s: %w", key, os.ErrExist)
	}
	f.objects[key] = data
	f.types[key] = contentType
	return nil
}

func (f *fakeBlobs) Exists(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.existsCalls++
	if f.existsCalls <= f.existsHits {
		return true, nil
	}
	_, ok := f.objects[key]
	return ok, nil
}

func (f *fakeBlobs) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}

func (f *fakeBlobs) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.objects))
	for k := range f.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestNewMediaID(t *testing.T) {
	seen := map[string]struct{}{}
	for range 1000 {
		id, err := newMediaID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != mediaIDLen {
			t.Fatalf("id %q length = %d, want %d", id, len(id), mediaIDLen)
		}
		for _, c := range id {
			if !strings.ContainsRune(base62Alphabet, c) {
				t.Fatalf("id %q contains non-base62 char %q", id, c)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q within 1000 draws", id)
		}
		seen[id] = struct{}{}
	}
}

func TestUploadEndpointStoresFileAndServesIt(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
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
		ID     string `json:"id"`
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
	if resp.ID == "" {
		t.Fatal("id is empty")
	}
	if resp.Mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", resp.Mime)
	}
	if resp.Width != 4 || resp.Height != 3 {
		t.Fatalf("dims = %dx%d, want 4x3", resp.Width, resp.Height)
	}

	key := uploadKey(t, resp.URL)
	wantKey := resp.ID + ".png"
	if key != wantKey {
		t.Fatalf("key = %q, want %q", key, wantKey)
	}
	data, err := os.ReadFile(filepath.Join(uploadDir, filepath.FromSlash(key)))
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

func TestUploadEndpointDedupsIdenticalContent(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	imgData := encodePNG(t, 5, 5)

	post := func() (id, url string, status int) {
		body, contentType := multipartBody(t, "file", "hello.png", imgData)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
		req.Header.Set("Content-Type", contentType)
		h.ServeHTTP(rec, req)
		var resp struct {
			URL string `json:"url"`
			ID  string `json:"id"`
		}
		if rec.Code == http.StatusCreated {
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
		}
		return resp.ID, resp.URL, rec.Code
	}

	id1, url1, status1 := post()
	if status1 != http.StatusCreated {
		t.Fatalf("first upload status = %d", status1)
	}
	id2, url2, status2 := post()
	if status2 != http.StatusCreated {
		t.Fatalf("second upload status = %d", status2)
	}
	if id1 != id2 {
		t.Fatalf("ids differ on dedup: %q vs %q", id1, id2)
	}
	if url1 != url2 {
		t.Fatalf("urls differ on dedup: %q vs %q", url1, url2)
	}

	// Only one file should have been written to disk.
	var fileCount int
	err := filepath.Walk(uploadDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fileCount++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileCount != 1 {
		t.Fatalf("file count = %d, want 1 (dedup should skip re-storing)", fileCount)
	}
}

func TestUploadEndpointRejectsOversizeFiles(t *testing.T) {
	srv := newDiskService(t.TempDir(), 4, "")
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
	srv := newDiskService(t.TempDir(), 1<<20, "https://files.example/u")
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
	srv := newDiskService(t.TempDir(), 1<<20, "")
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
	srv := newDiskService(t.TempDir(), 1<<20, "")
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

// postImage uploads a PNG and returns the recorder, for tests that only care
// about the status and body.
func postImage(t *testing.T, h http.Handler, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartBody(t, "file", name, data)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	return rec
}

func TestUploadEndpointWithoutBlobsBackendIsDisabled(t *testing.T) {
	// A nil Blobs means "no backend configured" — never "write to some
	// default directory".
	srv := &Service{Store: newFakeStore(), Cfg: Config{MaxBytes: 1 << 20}}
	h := srv.Handler()

	rec := postImage(t, h, "hello.png", encodePNG(t, 2, 2))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestUploadEndpointObjectStorageBackend(t *testing.T) {
	blobs := newFakeBlobs()
	store := newFakeStore()
	uploadDir := t.TempDir()
	srv := &Service{
		Store: store,
		Blobs: blobs,
		// No ServeDir: with a remote backend nothing is served locally.
		Cfg: Config{MaxBytes: 1 << 20, BaseURL: "https://cdn.example.com"},
	}
	h := srv.Handler()

	imgData := encodePNG(t, 4, 3)
	rec := postImage(t, h, "hello.png", imgData)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	wantKey := resp.ID + ".png"
	if want := "https://cdn.example.com/" + wantKey; resp.URL != want {
		t.Fatalf("url = %q, want %q", resp.URL, want)
	}
	if got := blobs.keys(); len(got) != 1 || got[0] != wantKey {
		t.Fatalf("stored keys = %v, want [%s]", got, wantKey)
	}
	if !bytes.Equal(blobs.objects[wantKey], imgData) {
		t.Fatal("stored bytes differ from uploaded bytes")
	}
	if ct := blobs.types[wantKey]; ct != "image/png" {
		t.Fatalf("stored content-type = %q, want image/png", ct)
	}
	// Nothing may leak onto local disk when a remote backend is selected.
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("local dir got %d entries, want none", len(entries))
	}
	// And /uploads/ serves nothing.
	serveRec := httptest.NewRecorder()
	h.ServeHTTP(serveRec, httptest.NewRequest(http.MethodGet, "/uploads/"+wantKey, http.NoBody))
	if serveRec.Code != http.StatusNotFound {
		t.Fatalf("serve status = %d, want 404", serveRec.Code)
	}

	// DELETE removes the object from the backend as well as the row.
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/api/media/"+resp.ID, http.NoBody))
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", delRec.Code, delRec.Body.String())
	}
	if len(blobs.keys()) != 0 {
		t.Fatalf("object still present after delete: %v", blobs.keys())
	}
	if _, ok, err := store.Get(context.Background(), resp.ID); err != nil || ok {
		t.Fatalf("row still present after delete (ok=%v err=%v)", ok, err)
	}
}

func TestUploadEndpointBackendFailureReturns502AndRecordsNothing(t *testing.T) {
	blobs := newFakeBlobs()
	blobs.putErr = errors.New("InvalidAccessKeyId")
	store := newFakeStore()
	srv := &Service{Store: store, Blobs: blobs, Cfg: Config{MaxBytes: 1 << 20, BaseURL: "https://cdn.example.com"}}
	h := srv.Handler()

	rec := postImage(t, h, "hello.png", encodePNG(t, 2, 2))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s, want 502", rec.Code, rec.Body.String())
	}
	// A half-published upload is worse than a failed one: no row, no object.
	if items, total, err := store.List(context.Background(), ListFilter{}, 10, 0); err != nil || total != 0 {
		t.Fatalf("store has %d rows (%v), want 0 (err=%v)", total, items, err)
	}
	if len(blobs.keys()) != 0 {
		t.Fatalf("backend has objects %v, want none", blobs.keys())
	}
}

func TestAllocateAndStoreRetriesWhenBackendKeyExists(t *testing.T) {
	blobs := newFakeBlobs()
	// Ids are random, so a specific key can't be pre-seeded: instead make the
	// first two Exists calls report a hit, as a key left behind by an
	// interrupted upload would.
	blobs.existsHits = 2
	srv := &Service{Store: newFakeStore(), Blobs: blobs, Cfg: Config{MaxBytes: 1 << 20}}

	m, key, err := srv.allocateAndStore(context.Background(), "hash", optimizedImage{
		Data: []byte("new"), Mime: "image/png", Ext: ".png", Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := blobs.existsCalls; got != 3 {
		t.Fatalf("Exists called %d times, want 3 (two hits then a free id)", got)
	}
	if m.ID+".png" != key {
		t.Fatalf("key %q does not match id %q", key, m.ID)
	}
	if !bytes.Equal(blobs.objects[key], []byte("new")) {
		t.Fatalf("object missing at %q; stored %v", key, blobs.keys())
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
