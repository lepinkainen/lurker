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

// callAuthed runs an authenticated XRPC call, refreshing once on 401.
func (c *Client) callAuthed(ctx context.Context, method, nsid string, q url.Values, body, out any) error {
	c.mu.Lock()
	access := c.accessJwt
	c.mu.Unlock()
	err := c.doJSON(ctx, method, nsid, q, access, body, out)
	if err == nil {
		return nil
	}
	var herr *httpError
	if !errors.As(err, &herr) || herr.Status != http.StatusUnauthorized {
		return err
	}
	if rerr := c.Refresh(ctx); rerr != nil {
		return fmt.Errorf("refresh after 401: %w", rerr)
	}
	c.mu.Lock()
	access = c.accessJwt
	c.mu.Unlock()
	return c.doJSON(ctx, method, nsid, q, access, body, out)
}

// httpError carries non-2xx XRPC responses.
type httpError struct {
	Status int
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
		return &httpError{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
