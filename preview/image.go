package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotImage is returned by FetchImage when the response's Content-Type is
// not an image/* type.
var ErrNotImage = errors.New("preview: not an image")

// FetchImage fetches target through the same SSRF-guarded HTTP client used by
// Fetch (SSRFCheck on every hop, DNS-pinned dial, redirect cap) and returns
// the raw body plus its Content-Type. It is used by callers that need image
// bytes directly (e.g. the avatar proxy) rather than the OpenGraph/preview
// pipeline.
//
// The response must advertise an image/* Content-Type, and the body is
// capped at cfg.MaxBytes — both callers get the same bounds Fetch already
// enforces. ctx controls the request timeout as usual.
func (f *Fetcher) FetchImage(ctx context.Context, target string) (data []byte, mimeType string, err error) {
	if ssrfErr := f.cfg.SSRFCheck(ctx, target); ssrfErr != nil {
		return nil, "", ssrfErr
	}
	resp, err := f.doRequest(ctx, http.MethodGet, target)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	ct := contentType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", fmt.Errorf("%w: content-type %q", ErrNotImage, ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.cfg.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("preview: read image body: %w", err)
	}
	if int64(len(body)) > f.cfg.MaxBytes {
		return nil, "", fmt.Errorf("preview: image exceeds max bytes (%d)", f.cfg.MaxBytes)
	}
	return body, ct, nil
}
