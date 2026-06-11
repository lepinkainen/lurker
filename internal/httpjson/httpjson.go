// Package httpjson provides a shared HTTP+JSON request helper.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
)

const (
	defaultMaxBytes = 4 << 20 // 4 MiB
	maxErrorBytes   = 2 << 10 // 2 KiB — safe to log
)

// Client is a reusable HTTP+JSON client. Zero value is usable with defaults.
type Client struct {
	HTTP     *http.Client // nil → http.DefaultClient
	MaxBytes int64        // response body cap; 0 → 4 MiB
}

// Request describes a single HTTP call.
type Request struct {
	Method        string // default: GET
	URL           string
	Body          any         // marshaled as JSON; sets Content-Type when non-nil
	Header        http.Header // merged over defaults; overrides Accept, but Body's Content-Type and the Authorization field win
	Authorization string      // full value: "Bearer x" or "Basic y"
}

// Response carries a 2xx reply.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Error carries a non-2xx reply. Body is truncated to 2 KiB for safe logging.
type Error struct {
	Status int
	Header http.Header
	Body   []byte
}

func (e *Error) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) limit() int64 {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return defaultMaxBytes
}

// Do executes the request. Returns *Error for non-2xx status codes.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}

	var reqBody io.Reader
	if r.Body != nil {
		buf, err := json.Marshal(r.Body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.URL, reqBody)
	if err != nil {
		return nil, err
	}

	// Default Accept; caller-supplied header overrides.
	req.Header.Set("Accept", "application/json")
	for k, vs := range r.Header {
		req.Header[http.CanonicalHeaderKey(k)] = slices.Clone(vs)
	}
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Authorization != "" {
		req.Header.Set("Authorization", r.Authorization)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		return nil, &Error{Status: resp.StatusCode, Header: resp.Header, Body: errBody}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.limit()))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// DoJSON calls Do and unmarshals the 2xx body into out. Pass nil out to skip decode.
func (c *Client) DoJSON(ctx context.Context, r Request, out any) error {
	resp, err := c.Do(ctx, r)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		preview := resp.Body
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return fmt.Errorf("decode response: %w (body: %s)", err, preview)
	}
	return nil
}
