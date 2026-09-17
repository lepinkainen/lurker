package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a config.yaml with the given body and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalNetworks = `
networks:
  - network: Libera
    servers:
      - host: irc.libera.chat
        port: 6697
        tls: true
`

func TestParseYAMLConfigMediaS3(t *testing.T) {
	t.Setenv("LURKER_TEST_S3_KEY", "AKIATEST")
	t.Setenv("LURKER_TEST_S3_SECRET", "s3cret")

	path := writeConfig(t, minimalNetworks+`
media:
  backend: s3
  max_bytes: 1048576
  s3:
    endpoint: s3.eu-north-1.amazonaws.com
    region: eu-north-1
    bucket: lurker-media
    access_key_id: ${LURKER_TEST_S3_KEY}
    secret_access_key: ${LURKER_TEST_S3_SECRET}
    public_base_url: https://cdn.example.com/
    prefix: /media/
    path_style: false
`)
	parsed, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Media
	if got.Backend != MediaBackendS3 {
		t.Fatalf("backend = %q, want %q", got.Backend, MediaBackendS3)
	}
	if got.MaxBytes != 1048576 {
		t.Fatalf("max_bytes = %d, want 1048576", got.MaxBytes)
	}
	// Secrets come from the environment, never the file.
	if got.S3.AccessKeyID != "AKIATEST" || got.S3.SecretAccessKey != "s3cret" {
		t.Fatalf("credentials not expanded: %+v", got.S3)
	}
	// Trailing/leading slashes are normalized so URL joining stays predictable.
	if got.S3.PublicBaseURL != "https://cdn.example.com" {
		t.Fatalf("public_base_url = %q", got.S3.PublicBaseURL)
	}
	if got.S3.Prefix != "media" {
		t.Fatalf("prefix = %q, want %q", got.S3.Prefix, "media")
	}
	if want := "https://cdn.example.com/media"; joinPublicURL(got.S3.PublicBaseURL, got.S3.Prefix) != want {
		t.Fatalf("joined base URL = %q, want %q", joinPublicURL(got.S3.PublicBaseURL, got.S3.Prefix), want)
	}
}

func TestParseYAMLConfigMediaS3DefaultsMaxBytes(t *testing.T) {
	path := writeConfig(t, minimalNetworks+`
media:
  backend: s3
  s3:
    endpoint: s3.eu-north-1.amazonaws.com
    bucket: lurker-media
    access_key_id: key
    secret_access_key: secret
    public_base_url: https://cdn.example.com
`)
	parsed, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Media.MaxBytes != defaultMediaMaxBytes {
		t.Fatalf("max_bytes = %d, want %d", parsed.Media.MaxBytes, defaultMediaMaxBytes)
	}
}

func TestParseYAMLConfigMediaDisk(t *testing.T) {
	path := writeConfig(t, minimalNetworks+`
media:
  backend: disk
  disk:
    dir: ./data/uploads
    base_url: http://lurker.tailnet.ts.net/
`)
	parsed, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Media
	if got.Backend != MediaBackendDisk {
		t.Fatalf("backend = %q, want %q", got.Backend, MediaBackendDisk)
	}
	if got.Disk.Dir != "./data/uploads" {
		t.Fatalf("dir = %q", got.Disk.Dir)
	}
	if got.Disk.BaseURL != "http://lurker.tailnet.ts.net" {
		t.Fatalf("base_url = %q", got.Disk.BaseURL)
	}
}

// No media block is not an error: the bouncer runs, uploads are simply off.
// It must never resolve to an implicit disk backend.
func TestParseYAMLConfigWithoutMediaBlockDisablesUploads(t *testing.T) {
	parsed, err := parseYAMLConfig(writeConfig(t, minimalNetworks))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Media.Backend != "" {
		t.Fatalf("backend = %q, want empty (uploads disabled)", parsed.Media.Backend)
	}
	if parsed.Media.Disk.Dir != "" {
		t.Fatalf("disk dir = %q, want empty", parsed.Media.Disk.Dir)
	}
}

// A present-but-incomplete media block is fatal. Booting a storage backend
// that half works is how uploads end up somewhere nobody intended.
func TestParseYAMLConfigMediaValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantText string
	}{
		{
			name:     "no backend named",
			body:     "media:\n  max_bytes: 1024\n",
			wantText: "backend is required",
		},
		{
			name:     "unknown backend",
			body:     "media:\n  backend: gcs\n",
			wantText: `unknown backend "gcs"`,
		},
		{
			name:     "s3 without block",
			body:     "media:\n  backend: s3\n",
			wantText: "requires a media.s3 block",
		},
		{
			name: "s3 missing public_base_url",
			body: `media:
  backend: s3
  s3:
    endpoint: s3.eu-north-1.amazonaws.com
    bucket: lurker-media
    access_key_id: key
    secret_access_key: secret
`,
			wantText: "media.s3.public_base_url",
		},
		{
			name: "s3 missing credentials",
			body: `media:
  backend: s3
  s3:
    endpoint: s3.eu-north-1.amazonaws.com
    bucket: lurker-media
    public_base_url: https://cdn.example.com
`,
			wantText: "media.s3.access_key_id",
		},
		{
			name: "s3 unexpanded env secret",
			body: `media:
  backend: s3
  s3:
    endpoint: s3.eu-north-1.amazonaws.com
    bucket: lurker-media
    access_key_id: key
    secret_access_key: ${LURKER_TEST_UNSET_SECRET}
    public_base_url: https://cdn.example.com
`,
			wantText: "media.s3.secret_access_key",
		},
		{
			name: "s3 endpoint with scheme",
			body: `media:
  backend: s3
  s3:
    endpoint: https://s3.eu-north-1.amazonaws.com
    bucket: lurker-media
    access_key_id: key
    secret_access_key: secret
    public_base_url: https://cdn.example.com
`,
			wantText: "must not include a scheme",
		},
		{
			name:     "disk without block",
			body:     "media:\n  backend: disk\n",
			wantText: "requires a media.disk block",
		},
		{
			name:     "disk without dir",
			body:     "media:\n  backend: disk\n  disk:\n    base_url: http://example\n",
			wantText: "media.disk.dir",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseYAMLConfig(writeConfig(t, minimalNetworks+tc.body))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantText)
			}
		})
	}
}
