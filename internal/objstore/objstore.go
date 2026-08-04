// Package objstore is a thin S3-compatible object store client covering the
// three operations lurker's media pipeline needs: store a variant, check
// whether a key is taken, delete a variant. It wraps minio-go, which is pure
// Go, so CGO_ENABLED=0 builds keep working.
//
// It deliberately has no local-disk fallback: the storage backend is selected
// explicitly in config, and a broken bucket must surface as an error rather
// than quietly divert uploads somewhere else.
package objstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// cacheControl is set on every stored object. Keys embed a random media id and
// are never rewritten, so the bytes behind a URL are immutable forever and the
// CDN can cache them indefinitely.
const cacheControl = "public, max-age=31536000, immutable"

// Config describes the bucket to publish to. Endpoint carries no scheme
// (e.g. "s3.eu-north-1.amazonaws.com"); TLS is always used.
type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// Prefix is an optional key prefix inside the bucket. Callers pass bare
	// keys; prefixing happens here and in the public URL the caller builds.
	Prefix string
	// PathStyle forces path-style addressing (bucket in the path rather than
	// the host). AWS S3 wants virtual-hosted style; some S3-compatible
	// services need this on.
	PathStyle bool

	// transport overrides the HTTP transport. Unexported: only this package's
	// tests use it, to talk to an httptest TLS stub.
	transport http.RoundTripper
}

// Client publishes media variants to an S3-compatible bucket.
type Client struct {
	mc     *minio.Client
	bucket string
	prefix string
}

// New builds a Client. Every field except Prefix/PathStyle is required: an
// incomplete configuration is an error, never a silent downgrade.
func New(cfg Config) (*Client, error) {
	missing := make([]string, 0, 5)
	for _, f := range []struct {
		name, value string
	}{
		{"endpoint", cfg.Endpoint},
		{"bucket", cfg.Bucket},
		{"access_key_id", cfg.AccessKeyID},
		{"secret_access_key", cfg.SecretAccessKey},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("objstore: missing required config: %s", strings.Join(missing, ", "))
	}
	if strings.Contains(cfg.Endpoint, "://") {
		return nil, fmt.Errorf("objstore: endpoint %q must not include a scheme", cfg.Endpoint)
	}

	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       true,
		Region:       cfg.Region,
		BucketLookup: lookup,
		Transport:    cfg.transport,
	})
	if err != nil {
		return nil, fmt.Errorf("objstore: %w", err)
	}
	return &Client{
		mc:     mc,
		bucket: cfg.Bucket,
		prefix: strings.Trim(strings.TrimSpace(cfg.Prefix), "/"),
	}, nil
}

// Bucket returns the configured bucket name, for logging.
func (c *Client) Bucket() string { return c.bucket }

// prefixed joins the configured prefix onto a bare key.
func (c *Client) prefixed(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "/" + strings.TrimPrefix(key, "/")
}

// Probe reports whether the bucket is reachable with the configured
// credentials. Used as a boot-time health check so a misconfigured deployment
// is visible in the logs before the first upload rather than after it.
func (c *Client) Probe(ctx context.Context) error {
	ok, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("objstore: probe bucket %q: %w", c.bucket, err)
	}
	if !ok {
		return fmt.Errorf("objstore: bucket %q does not exist or is not visible to these credentials", c.bucket)
	}
	return nil
}

// Put stores data under key with the given content type and an immutable
// cache-control header. Content type and caching are baked in at PUT time
// because the CDN serves the bytes directly.
func (c *Client) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, c.prefixed(key), bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{
			ContentType:  contentType,
			CacheControl: cacheControl,
		})
	if err != nil {
		return fmt.Errorf("objstore: put %q: %w", c.prefixed(key), err)
	}
	return nil
}

// Exists reports whether key is already present.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, c.prefixed(key), minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("objstore: stat %q: %w", c.prefixed(key), err)
	}
	return true, nil
}

// Delete removes key. A missing key is not an error: cleanup callers may race
// a Put that never landed.
func (c *Client) Delete(ctx context.Context, key string) error {
	err := c.mc.RemoveObject(ctx, c.bucket, c.prefixed(key), minio.RemoveObjectOptions{})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("objstore: delete %q: %w", c.prefixed(key), err)
	}
	return nil
}

// isNotFound recognizes S3's several ways of saying "no such object". Some
// implementations answer HEAD with a bare 404 and no error code, so the
// status code is checked too.
func isNotFound(err error) bool {
	resp, ok := errors.AsType[minio.ErrorResponse](err)
	if !ok {
		return false
	}
	return resp.StatusCode == http.StatusNotFound ||
		resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket"
}
