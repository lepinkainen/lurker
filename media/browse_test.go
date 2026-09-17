package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type listResponse struct {
	Items  []mediaListItem `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

func uploadImage(t *testing.T, handler http.Handler, name string, w, height int) (id, url string) {
	t.Helper()
	body, contentType := multipartBody(t, "file", name, encodePNG(t, w, height))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.ID, resp.URL
}

func getMediaList(t *testing.T, h http.Handler, query string) listResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/media"+query, http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp listResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestListMediaEndpointReturnsUploadedItems(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	id1, url1 := uploadImage(t, h, "a.png", 4, 3)
	id2, url2 := uploadImage(t, h, "b.png", 8, 6)

	resp := getMediaList(t, h, "")
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(resp.Items))
	}

	byID := map[string]mediaListItem{}
	for _, it := range resp.Items {
		byID[it.ID] = it
	}
	item1, ok := byID[id1]
	if !ok {
		t.Fatalf("missing item %q in list", id1)
	}
	if item1.URL != url1 {
		t.Fatalf("item1 url = %q, want %q", item1.URL, url1)
	}
	if item1.Mime != "image/png" || item1.Kind != "image" {
		t.Fatalf("item1 mime/kind = %q/%q", item1.Mime, item1.Kind)
	}
	if item1.Width != 4 || item1.Height != 3 {
		t.Fatalf("item1 dims = %dx%d, want 4x3", item1.Width, item1.Height)
	}
	if item1.Bytes <= 0 {
		t.Fatalf("item1 bytes = %d, want > 0", item1.Bytes)
	}
	if item1.CreatedAt == "" {
		t.Fatal("item1 created_at is empty")
	}

	item2, ok := byID[id2]
	if !ok {
		t.Fatalf("missing item %q in list", id2)
	}
	if item2.URL != url2 {
		t.Fatalf("item2 url = %q, want %q", item2.URL, url2)
	}
}

func TestListMediaEndpointFiltersByKindAndQ(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	id, _ := uploadImage(t, h, "only.png", 2, 2)

	resp := getMediaList(t, h, "?kind=image")
	if resp.Total != 1 {
		t.Fatalf("kind=image total = %d, want 1", resp.Total)
	}

	resp = getMediaList(t, h, "?kind=video")
	if resp.Total != 0 {
		t.Fatalf("kind=video total = %d, want 0", resp.Total)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("kind=video items len = %d, want 0", len(resp.Items))
	}

	resp = getMediaList(t, h, "?q="+id)
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].ID != id {
		t.Fatalf("q=%s resp = %+v", id, resp)
	}

	resp = getMediaList(t, h, "?q=does-not-exist-anywhere")
	if resp.Total != 0 {
		t.Fatalf("q=nonexistent total = %d, want 0", resp.Total)
	}
}

func TestListMediaEndpointCapsLimit(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	uploadImage(t, h, "a.png", 2, 2)

	resp := getMediaList(t, h, "?limit="+strconv.Itoa(maxListLimit+500))
	if resp.Limit != maxListLimit {
		t.Fatalf("limit = %d, want capped at %d", resp.Limit, maxListLimit)
	}

	resp = getMediaList(t, h, "")
	if resp.Limit != defaultListLimit {
		t.Fatalf("default limit = %d, want %d", resp.Limit, defaultListLimit)
	}
}

func TestDeleteMediaEndpointRemovesRowAndFile(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	id, url := uploadImage(t, h, "gone.png", 3, 3)
	key := uploadKey(t, url)
	diskPath := filepath.Join(uploadDir, filepath.FromSlash(key))
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("expected uploaded file on disk: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/media/"+id, http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed from disk, stat err = %v", err)
	}

	resp := getMediaList(t, h, "")
	if resp.Total != 0 {
		t.Fatalf("total after delete = %d, want 0", resp.Total)
	}
}

func TestExistsMediaEndpoint(t *testing.T) {
	uploadDir := t.TempDir()
	srv := newDiskService(uploadDir, 1<<20, "")
	h := srv.Handler()

	// Upload known bytes, then look them up by their SHA-256 (as a client would
	// hash the exact bytes it POSTs).
	imgData := encodePNG(t, 5, 4)
	body, contentType := multipartBody(t, "file", "pic.png", imgData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	var up struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&up); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(imgData)
	hash := hex.EncodeToString(sum[:])

	t.Run("hit returns 200 with the stored url", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/media/exists?hash="+hash, http.NoBody)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("exists status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.ID != up.ID || resp.URL != up.URL {
			t.Fatalf("exists resp = %+v, want id=%q url=%q", resp, up.ID, up.URL)
		}
	})

	t.Run("uppercase hash is accepted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/media/exists?hash="+strings.ToUpper(hash), http.NoBody)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("uppercase hash status = %d", rec.Code)
		}
	})

	t.Run("miss returns 404", func(t *testing.T) {
		miss := strings.Repeat("0", 64)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/media/exists?hash="+miss, http.NoBody)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("miss status = %d, want 404", rec.Code)
		}
	})

	t.Run("malformed hash returns 400", func(t *testing.T) {
		for _, bad := range []string{"", "xyz", "deadbeef", strings.Repeat("g", 64)} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/media/exists?hash="+bad, http.NoBody)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("hash %q status = %d, want 400", bad, rec.Code)
			}
		}
	})
}

func TestDeleteMediaEndpointUnknownIDReturns404(t *testing.T) {
	srv := newDiskService(t.TempDir(), 1<<20, "")
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/media/does-not-exist", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete unknown status = %d body=%s", rec.Code, rec.Body.String())
	}
}
