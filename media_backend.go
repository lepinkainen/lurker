package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/lepinkainen/lurker/internal/objstore"
	"github.com/lepinkainen/lurker/media"
)

// mediaProbeTimeout bounds the boot-time reachability check against the
// object store. It must stay short: a slow bucket may not delay IRC coming up.
const mediaProbeTimeout = 10 * time.Second

// newMediaService builds the media upload service for the configured backend.
// The backend is always explicit:
//
//   - "s3"  — publish to the bucket. If the bucket is unreachable at boot we
//     log loudly and keep the s3 backend anyway, so uploads fail with 502
//     rather than silently landing on local disk.
//   - "disk" — store under the configured directory.
//   - unset — no backend, uploads disabled. The bouncer still runs.
//
// There is deliberately no fallback path between backends.
func newMediaService(ctx context.Context, cfg MediaConfig, store media.Store) *media.Service {
	switch cfg.Backend {
	case MediaBackendS3:
		client, err := objstore.New(objstore.Config{
			Endpoint:        cfg.S3.Endpoint,
			Region:          cfg.S3.Region,
			Bucket:          cfg.S3.Bucket,
			AccessKeyID:     cfg.S3.AccessKeyID,
			SecretAccessKey: cfg.S3.SecretAccessKey,
			Prefix:          cfg.S3.Prefix,
			PathStyle:       cfg.S3.PathStyle,
		})
		if err != nil {
			// Config-level failure: same class as a broken config.yaml.
			slog.Error("media: invalid s3 configuration", "err", err)
			os.Exit(1)
		}

		probeCtx, cancel := context.WithTimeout(ctx, mediaProbeTimeout)
		defer cancel()
		if probeErr := client.Probe(probeCtx); probeErr != nil {
			// Runtime failure (bad credentials, bucket gone, network down).
			// Loud, but not fatal: IRC must not go offline because a cloud
			// dependency is having a bad day. Uploads 502 until it recovers.
			slog.Error("media: s3 backend unusable; uploads will fail until this is fixed",
				"bucket", cfg.S3.Bucket, "endpoint", cfg.S3.Endpoint, "err", probeErr)
		}

		baseURL := joinPublicURL(cfg.S3.PublicBaseURL, cfg.S3.Prefix)
		slog.Info("media storage", "backend", MediaBackendS3, "bucket", cfg.S3.Bucket,
			"endpoint", cfg.S3.Endpoint, "public_base_url", baseURL, "max_bytes", cfg.MaxBytes)
		return &media.Service{
			Store: store,
			Blobs: client,
			Cfg:   media.Config{MaxBytes: cfg.MaxBytes, BaseURL: baseURL},
		}

	case MediaBackendDisk:
		if err := os.MkdirAll(cfg.Disk.Dir, 0o755); err != nil {
			slog.Error("media: create upload dir", "dir", cfg.Disk.Dir, "err", err)
			os.Exit(1)
		}
		slog.Info("media storage", "backend", MediaBackendDisk, "dir", cfg.Disk.Dir,
			"base_url", cfg.Disk.BaseURL, "max_bytes", cfg.MaxBytes)
		return &media.Service{
			Store: store,
			Blobs: &media.DiskBlobs{Dir: cfg.Disk.Dir},
			Cfg: media.Config{
				MaxBytes: cfg.MaxBytes,
				BaseURL:  cfg.Disk.BaseURL,
				ServeDir: cfg.Disk.Dir,
			},
		}

	default:
		// No `media:` block. Uploads are off; nothing is written anywhere.
		slog.Warn("media: no backend configured; uploads disabled (see S3_SETUP.md)")
		return &media.Service{Store: store}
	}
}

// joinPublicURL builds the URL prefix returned to clients from the CDN base
// and the bucket key prefix, which the objstore client applies to keys.
func joinPublicURL(base, prefix string) string {
	if prefix == "" {
		return base
	}
	return base + "/" + prefix
}
