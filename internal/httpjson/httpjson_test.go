package httpjson_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lepinkainen/lurker/internal/httpjson"
)

func TestDo_2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	resp, err := c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("body = %q", resp.Body)
	}
	if resp.Header.Get("X-Custom") != "yes" {
		t.Error("response header not propagated")
	}
}

func TestDo_NonOK_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Err", "detail")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, err := c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *httpjson.Error
	if !isErrorAs(err, &herr) {
		t.Fatalf("err type = %T, want *httpjson.Error", err)
	}
	if herr.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", herr.Status)
	}
	if herr.Header.Get("X-Err") != "detail" {
		t.Error("error header not propagated")
	}
	if !strings.Contains(string(herr.Body), "Unauthorized") {
		t.Errorf("body = %q", herr.Body)
	}
}

func TestDo_ErrorBodyTruncatedAt2KiB(t *testing.T) {
	bigBody := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, err := c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	var herr *httpjson.Error
	if !isErrorAs(err, &herr) {
		t.Fatalf("err type = %T", err)
	}
	if len(herr.Body) > 2048 {
		t.Errorf("error body len = %d, want ≤ 2048", len(herr.Body))
	}
}

func TestDo_BodyCapApplied(t *testing.T) {
	payload := strings.Repeat("a", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client(), MaxBytes: 50}
	resp, err := c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Body) != 50 {
		t.Errorf("body len = %d, want 50", len(resp.Body))
	}
}

func TestDo_DefaultAcceptHeader(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, _ = c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestDo_CallerAcceptOverridesDefault(t *testing.T) {
	const want = "application/activity+json"
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, _ = c.Do(context.Background(), httpjson.Request{
		URL:    srv.URL,
		Header: http.Header{"Accept": []string{want}},
	})
	if gotAccept != want {
		t.Errorf("Accept = %q, want %q", gotAccept, want)
	}
}

func TestDo_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, _ = c.Do(context.Background(), httpjson.Request{
		URL:           srv.URL,
		Authorization: "Bearer testtoken",
	})
	if gotAuth != "Bearer testtoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestDo_BodySetsContentType(t *testing.T) {
	var gotCT, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotMethod = r.Method
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, _ = c.Do(context.Background(), httpjson.Request{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   map[string]string{"key": "val"},
	})
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestDoJSON_DecodesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"lurker","version":2}`))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	var out struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	if err := c.DoJSON(context.Background(), httpjson.Request{URL: srv.URL}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "lurker" || out.Version != 2 {
		t.Errorf("decoded = %+v", out)
	}
}

func TestDoJSON_NilOut_SkipsDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not valid json at all !!!`))
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	if err := c.DoJSON(context.Background(), httpjson.Request{URL: srv.URL}, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDo_DefaultMethod_IsGET(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method
	}))
	defer srv.Close()

	c := &httpjson.Client{HTTP: srv.Client()}
	_, _ = c.Do(context.Background(), httpjson.Request{URL: srv.URL})
	if got != http.MethodGet {
		t.Errorf("method = %q, want GET", got)
	}
}

func isErrorAs(err error, target **httpjson.Error) bool {
	return errors.As(err, target)
}
