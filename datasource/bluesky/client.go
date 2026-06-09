// Package bluesky implements a read-only Bluesky data source for Lurker.
// The XRPC client here is hand-rolled to avoid the heavy indigo SDK; only
// the endpoints Lurker actually consumes are wired up.
package bluesky

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultPDS is bsky.social, the canonical entry PDS. Per-account PDS hosts
// resolve transparently because createSession is allowed on the entry host.
const DefaultPDS = "https://bsky.social"

// HTTPTimeout caps every XRPC request.
const HTTPTimeout = 20 * time.Second

// Client is a thin XRPC client. Goroutine-safe.
type Client struct {
	pds        string
	identifier string
	password   string

	http *http.Client

	mu         sync.Mutex
	accessJwt  string
	refreshJwt string
	did        string
	handle     string
}

// NewClient constructs an unauthenticated client. Call Login before any
// authenticated request.
func NewClient(pds, identifier, password string) *Client {
	if pds == "" {
		pds = DefaultPDS
	}
	pds = strings.TrimRight(pds, "/")
	return &Client{
		pds:        pds,
		identifier: identifier,
		password:   password,
		http:       &http.Client{Timeout: HTTPTimeout},
	}
}

// Handle returns the resolved account handle. Valid after Login.
func (c *Client) Handle() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handle
}

// DID returns the resolved account DID. Valid after Login.
func (c *Client) DID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.did
}

// PDS returns the configured PDS hostname (with scheme).
func (c *Client) PDS() string { return c.pds }

// Login authenticates via com.atproto.server.createSession.
func (c *Client) Login(ctx context.Context) error {
	body := CreateSessionRequest{Identifier: c.identifier, Password: c.password}
	var resp SessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "com.atproto.server.createSession", nil, "", body, &resp); err != nil {
		return fmt.Errorf("createSession: %w", err)
	}
	if resp.AccessJwt == "" || resp.RefreshJwt == "" {
		return errors.New("createSession: empty session tokens")
	}
	c.mu.Lock()
	c.accessJwt = resp.AccessJwt
	c.refreshJwt = resp.RefreshJwt
	c.did = resp.DID
	c.handle = resp.Handle
	c.mu.Unlock()
	return nil
}

// Refresh rotates the access JWT via com.atproto.server.refreshSession.
// The refresh JWT is sent as the Bearer credential.
func (c *Client) Refresh(ctx context.Context) error {
	c.mu.Lock()
	refresh := c.refreshJwt
	c.mu.Unlock()
	if refresh == "" {
		return errors.New("refreshSession: no refresh token")
	}
	var resp SessionResponse
	if err := c.doJSON(ctx, http.MethodPost, "com.atproto.server.refreshSession", nil, refresh, nil, &resp); err != nil {
		return fmt.Errorf("refreshSession: %w", err)
	}
	if resp.AccessJwt == "" || resp.RefreshJwt == "" {
		return errors.New("refreshSession: empty session tokens")
	}
	c.mu.Lock()
	c.accessJwt = resp.AccessJwt
	c.refreshJwt = resp.RefreshJwt
	if resp.DID != "" {
		c.did = resp.DID
	}
	if resp.Handle != "" {
		c.handle = resp.Handle
	}
	c.mu.Unlock()
	return nil
}

// GetTimeline fetches the authenticated user's home timeline. Pass empty
// cursor for the newest page; cursor pages OLDER per ATProto semantics.
func (c *Client) GetTimeline(ctx context.Context, limit int, cursor string) (*FeedResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out FeedResponse
	if err := c.callAuthed(ctx, http.MethodGet, "app.bsky.feed.getTimeline", q, nil, &out); err != nil {
		return nil, fmt.Errorf("getTimeline: %w", err)
	}
	return &out, nil
}

// getPostsMax is the per-call URI cap of app.bsky.feed.getPosts. Callers
// must chunk longer lists.
const getPostsMax = 25

// GetPosts hydrates posts by their at:// URIs via app.bsky.feed.getPosts.
// At most getPostsMax URIs are honoured per call; extras are dropped by the
// API, so callers chunk. Missing/blocked posts are simply absent from the
// response rather than erroring.
func (c *Client) GetPosts(ctx context.Context, uris []string) ([]PostView, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	if len(uris) > getPostsMax {
		uris = uris[:getPostsMax]
	}
	q := url.Values{}
	for _, u := range uris {
		q.Add("uris", u)
	}
	var out struct {
		Posts []PostView `json:"posts"`
	}
	if err := c.callAuthed(ctx, http.MethodGet, "app.bsky.feed.getPosts", q, nil, &out); err != nil {
		return nil, fmt.Errorf("getPosts: %w", err)
	}
	return out.Posts, nil
}

// callAuthed runs an authenticated XRPC call. On expired access JWT it
// refreshes and retries once; if the refresh JWT itself is expired it
// re-runs createSession and retries once.
func (c *Client) callAuthed(ctx context.Context, method, nsid string, q url.Values, body, out any) error {
	c.mu.Lock()
	access := c.accessJwt
	c.mu.Unlock()
	err := c.doJSON(ctx, method, nsid, q, access, body, out)
	if err == nil {
		return nil
	}
	if !isExpiredAuthErr(err) {
		return err
	}
	if rerr := c.Refresh(ctx); rerr != nil {
		if !isExpiredAuthErr(rerr) {
			return fmt.Errorf("refresh after expired access: %w", rerr)
		}
		if lerr := c.Login(ctx); lerr != nil {
			return fmt.Errorf("re-login after expired refresh: %w", lerr)
		}
	}
	c.mu.Lock()
	access = c.accessJwt
	c.mu.Unlock()
	return c.doJSON(ctx, method, nsid, q, access, body, out)
}

// isExpiredAuthErr reports whether err is an XRPC auth failure that warrants
// a token refresh or re-login. ATProto returns 400 + error:"ExpiredToken" on
// access-JWT expiry, and 400 + error:"ExpiredToken"/"InvalidToken" on
// refresh-JWT expiry; 401 also covers older/edge cases.
func isExpiredAuthErr(err error) bool {
	var herr *httpError
	if !errors.As(err, &herr) {
		return false
	}
	if herr.Status == http.StatusUnauthorized {
		return true
	}
	if herr.Status != http.StatusBadRequest {
		return false
	}
	switch herr.Code {
	case "ExpiredToken", "InvalidToken", "AuthenticationRequired":
		return true
	}
	return false
}

// parseXRPCErrorCode extracts the `error` field from an XRPC error body.
// Returns "" if the body is not JSON or has no error field.
func parseXRPCErrorCode(raw []byte) string {
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return ""
	}
	return env.Error
}

// httpError carries non-2xx XRPC responses.
type httpError struct {
	Status int
	Code   string
	Body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("xrpc http %d: %s", e.Status, e.Body)
}

// doJSON is the unifying request helper. Pass bearer="" for unauthenticated
// calls; body=nil for GET-style calls (query-string only).
func (c *Client) doJSON(ctx context.Context, method, nsid string, q url.Values, bearer string, body, out any) error {
	u := c.pds + "/xrpc/" + nsid
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpError{Status: resp.StatusCode, Code: parseXRPCErrorCode(raw), Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
