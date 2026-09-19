package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessageHandler(t *testing.T) {
	const limit = 32
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"ready", "GET", "/ready", "", http.StatusNoContent},
		{"message", "POST", "/message", "hello from bob", http.StatusAccepted},
		{"empty", "POST", "/message", "", http.StatusBadRequest},
		{"multiline", "POST", "/message", "hello\r\nQUIT", http.StatusBadRequest},
		{"nul", "POST", "/message", "hello\x00", http.StatusBadRequest},
		{"at limit", "POST", "/message", strings.Repeat("x", limit), http.StatusAccepted},
		{"too long", "POST", "/message", strings.Repeat("x", limit+1), http.StatusBadRequest},
		{"multibyte at limit", "POST", "/message", strings.Repeat("é", limit/2), http.StatusAccepted},
		{"multibyte too long", "POST", "/message", strings.Repeat("é", limit/2) + "x", http.StatusBadRequest},
		{"wrong method", "GET", "/message", "hello", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []string
			h := messageHandler(func() int { return limit }, func(message string) error { sent = append(sent, message); return nil })
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if tc.status == http.StatusAccepted {
				if len(sent) != 1 || sent[0] != tc.body {
					t.Fatalf("sent = %q", sent)
				}
			} else if len(sent) != 0 {
				t.Fatalf("unexpected messages: %q", sent)
			}
		})
	}
}
