package objstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// recordedRequest is one request the S3 stub saw.
type recordedRequest struct {
	Method       string
	Path         string
	ContentType  string
	CacheControl string
	Body         string
	Authz        string
}

// s3Stub is a minimal stand-in for an S3 endpoint: it records requests and
// answers from a canned status map keyed by "METHOD /path".
type s3Stub struct {
	mu       sync.Mutex
	requests []recordedRequest
	// status overrides the response for a given "METHOD /path"; anything not
	// listed gets a 200.
	status map[string]int
}

func (s *s3Stub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{
			Method:       r.Method,
			Path:         r.URL.Path,
			ContentType:  r.Header.Get("Content-Type"),
			CacheControl: r.Header.Get("Cache-Control"),
			Body:         string(body),
			Authz:        r.Header.Get("Authorization"),
		})
		code, ok := s.status[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if ok && code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		// ETag and Last-Modified are what minio-go parses out of PUT and HEAD
		// responses; without them it rejects the reply as malformed.
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.Header().Set("Last-Modified", "Mon, 2 Jan 2006 15:04:05 GMT")
		w.WriteHeader(http.StatusOK)
	})
}

func (s *s3Stub) seen() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

// newTestClient starts a TLS stub and points a path-style Client at it.
func newTestClient(t *testing.T, stub *s3Stub, prefix string) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(stub.handler())
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{
		Endpoint:        u.Host,
		Region:          "eu-north-1",
		Bucket:          "lurker-media",
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Prefix:          prefix,
		PathStyle:       true,
		// srv.Client()'s transport already trusts the stub's self-signed
		// certificate, so TLS verification stays on.
		transport: srv.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	full := Config{
		Endpoint:        "s3.eu-north-1.amazonaws.com",
		Bucket:          "lurker-media",
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
	}
	if _, err := New(full); err != nil {
		t.Fatalf("complete config rejected: %v", err)
	}

	tests := map[string]func(*Config){
		"endpoint":          func(c *Config) { c.Endpoint = "" },
		"bucket":            func(c *Config) { c.Bucket = "" },
		"access_key_id":     func(c *Config) { c.AccessKeyID = "" },
		"secret_access_key": func(c *Config) { c.SecretAccessKey = " " },
	}
	for field, blank := range tests {
		t.Run("missing "+field, func(t *testing.T) {
			cfg := full
			blank(&cfg)
			_, err := New(cfg)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("err = %v, want it to name %q", err, field)
			}
		})
	}

	t.Run("endpoint with scheme", func(t *testing.T) {
		cfg := full
		cfg.Endpoint = "https://s3.eu-north-1.amazonaws.com"
		if _, err := New(cfg); err == nil {
			t.Fatal("want error for endpoint with scheme, got nil")
		}
	})
}

func TestPutSendsKeyContentTypeAndImmutableCacheControl(t *testing.T) {
	stub := &s3Stub{status: map[string]int{}}
	c := newTestClient(t, stub, "")

	if err := c.Put(context.Background(), "abc123.jpg", []byte("jpegbytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}

	reqs := stub.seen()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1: %+v", len(reqs), reqs)
	}
	got := reqs[0]
	if got.Method != http.MethodPut {
		t.Fatalf("method = %s, want PUT", got.Method)
	}
	if got.Path != "/lurker-media/abc123.jpg" {
		t.Fatalf("path = %q, want /lurker-media/abc123.jpg", got.Path)
	}
	if got.ContentType != "image/jpeg" {
		t.Fatalf("content-type = %q, want image/jpeg", got.ContentType)
	}
	if got.CacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q", got.CacheControl)
	}
	if got.Body != "jpegbytes" {
		t.Fatalf("body = %q", got.Body)
	}
	if !strings.HasPrefix(got.Authz, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("authorization = %q, want a SigV4 header", got.Authz)
	}
}

func TestPutAppliesPrefix(t *testing.T) {
	stub := &s3Stub{status: map[string]int{}}
	c := newTestClient(t, stub, "media/")

	if err := c.Put(context.Background(), "abc123.jpg", []byte("x"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if got := stub.seen()[0].Path; got != "/lurker-media/media/abc123.jpg" {
		t.Fatalf("path = %q, want /lurker-media/media/abc123.jpg", got)
	}
}

func TestPutSurfacesBackendError(t *testing.T) {
	stub := &s3Stub{status: map[string]int{
		"PUT /lurker-media/abc123.jpg": http.StatusForbidden,
	}}
	c := newTestClient(t, stub, "")

	err := c.Put(context.Background(), "abc123.jpg", []byte("x"), "image/jpeg")
	if err == nil {
		t.Fatal("want error on 403, got nil")
	}
	if !strings.Contains(err.Error(), "abc123.jpg") {
		t.Fatalf("err = %v, want it to name the key", err)
	}
}

func TestExists(t *testing.T) {
	stub := &s3Stub{status: map[string]int{
		"HEAD /lurker-media/missing.jpg": http.StatusNotFound,
	}}
	c := newTestClient(t, stub, "")
	ctx := context.Background()

	ok, err := c.Exists(ctx, "present.jpg")
	if err != nil || !ok {
		t.Fatalf("Exists(present) = %v, %v; want true, nil", ok, err)
	}
	ok, err = c.Exists(ctx, "missing.jpg")
	if err != nil {
		t.Fatalf("Exists(missing) err = %v, want nil (404 means absent, not broken)", err)
	}
	if ok {
		t.Fatal("Exists(missing) = true, want false")
	}
}

func TestExistsPropagatesRealErrors(t *testing.T) {
	stub := &s3Stub{status: map[string]int{
		"HEAD /lurker-media/denied.jpg": http.StatusForbidden,
	}}
	c := newTestClient(t, stub, "")

	if _, err := c.Exists(context.Background(), "denied.jpg"); err == nil {
		t.Fatal("want error on 403, got nil")
	}
}

func TestDeleteToleratesMissingObject(t *testing.T) {
	stub := &s3Stub{status: map[string]int{
		"DELETE /lurker-media/gone.jpg": http.StatusNotFound,
	}}
	c := newTestClient(t, stub, "")

	if err := c.Delete(context.Background(), "gone.jpg"); err != nil {
		t.Fatalf("Delete missing = %v, want nil", err)
	}
}

func TestProbe(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		stub := &s3Stub{status: map[string]int{}}
		c := newTestClient(t, stub, "")
		if err := c.Probe(context.Background()); err != nil {
			t.Fatalf("Probe = %v, want nil", err)
		}
	})

	t.Run("bad credentials", func(t *testing.T) {
		stub := &s3Stub{status: map[string]int{
			"HEAD /lurker-media/": http.StatusForbidden,
		}}
		c := newTestClient(t, stub, "")
		err := c.Probe(context.Background())
		if err == nil {
			t.Fatal("want error on 403, got nil")
		}
		if !strings.Contains(err.Error(), "lurker-media") {
			t.Fatalf("err = %v, want it to name the bucket", err)
		}
	})

	t.Run("missing bucket", func(t *testing.T) {
		stub := &s3Stub{status: map[string]int{
			"HEAD /lurker-media/": http.StatusNotFound,
		}}
		c := newTestClient(t, stub, "")
		if err := c.Probe(context.Background()); err == nil {
			t.Fatal("want error for a nonexistent bucket, got nil")
		}
	})
}
