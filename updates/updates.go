package updates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled  bool
	Image    string
	Tag      string
	Interval time.Duration
	Username string
	Token    string
	Platform Platform
	Current  BuildInfo
	Client   *http.Client
	Logger   *slog.Logger
}

type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

type Status struct {
	Enabled          bool      `json:"enabled"`
	Image            string    `json:"image"`
	Tag              string    `json:"tag"`
	CurrentVersion   string    `json:"current_version"`
	CurrentCommit    string    `json:"current_commit"`
	CurrentBuildTime string    `json:"current_build_time"`
	RemoteVersion    string    `json:"remote_version,omitempty"`
	RemoteCommit     string    `json:"remote_commit,omitempty"`
	RemoteBuildTime  string    `json:"remote_build_time,omitempty"`
	RemoteDigest     string    `json:"remote_digest,omitempty"`
	CheckedAt        time.Time `json:"checked_at,omitempty"`
	UpdateAvailable  bool      `json:"update_available"`
	Error            string    `json:"error,omitempty"`
}

type Checker struct {
	cfg    Config
	client *http.Client
	logger *slog.Logger

	mu     sync.RWMutex
	status Status
}

func New(cfg Config) *Checker {
	if cfg.Image == "" {
		cfg.Image = "ghcr.io/lepinkainen/lurker"
	}
	if cfg.Tag == "" {
		cfg.Tag = "latest"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.Platform.OS == "" {
		cfg.Platform.OS = runtime.GOOS
	}
	if cfg.Platform.Architecture == "" {
		cfg.Platform.Architecture = runtime.GOARCH
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Checker{cfg: cfg, client: cfg.Client, logger: cfg.Logger}
	c.status = Status{
		Enabled:          cfg.Enabled,
		Image:            cfg.Image,
		Tag:              cfg.Tag,
		CurrentVersion:   cfg.Current.Version,
		CurrentCommit:    cfg.Current.Commit,
		CurrentBuildTime: cfg.Current.BuildTime,
	}
	return c
}

func (c *Checker) Start(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	go c.run(ctx)
}

func (c *Checker) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Checker) run(ctx context.Context) {
	c.check(ctx)
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(ctx)
		}
	}
}

func (c *Checker) check(ctx context.Context) {
	st := c.Status()
	remote, err := fetchRemoteStatus(ctx, c.client, c.cfg)
	st.CheckedAt = time.Now().UTC()
	if err != nil {
		st.Error = err.Error()
		c.setStatus(st)
		c.logger.Warn("update check failed", "image", c.cfg.Image, "tag", c.cfg.Tag, "err", err)
		return
	}
	st.RemoteVersion = remote.Version
	st.RemoteCommit = remote.Commit
	st.RemoteBuildTime = remote.BuildTime
	st.RemoteDigest = remote.Digest
	st.UpdateAvailable = compareRemote(c.cfg.Current, remote)
	st.Error = ""
	c.setStatus(st)
}

func (c *Checker) setStatus(st Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = st
}

type remoteStatus struct {
	Version   string
	Commit    string
	BuildTime string
	Digest    string
}

type authChallenge struct {
	Realm   string
	Service string
	Scope   string
}

type manifestList struct {
	SchemaVersion int                 `json:"schemaVersion"`
	MediaType     string              `json:"mediaType"`
	Manifests     []manifestListEntry `json:"manifests"`
}

type manifestListEntry struct {
	MediaType string           `json:"mediaType"`
	Digest    string           `json:"digest"`
	Platform  manifestPlatform `json:"platform"`
}

type manifestPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

type imageManifest struct {
	SchemaVersion int `json:"schemaVersion"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"config"`
}

type imageConfigBlob struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"config"`
}

const (
	mediaTypeOCIImageIndex      = "application/vnd.oci.image.index.v1+json"
	mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaTypeOCIImageManifest   = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeDockerManifestV2   = "application/vnd.docker.distribution.manifest.v2+json"
)

func fetchRemoteStatus(ctx context.Context, client *http.Client, cfg Config) (remoteStatus, error) {
	repo, err := normalizeImage(cfg.Image)
	if err != nil {
		return remoteStatus{}, err
	}
	token, err := registryToken(ctx, client, cfg, repo)
	if err != nil {
		return remoteStatus{}, err
	}
	manifestURL := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, cfg.Tag)
	body, digest, mediaType, err := getRegistryJSON(ctx, client, manifestURL, token)
	if err != nil {
		return remoteStatus{}, err
	}
	manifestDigest := digest
	manifestBody := body
	manifestMediaType := mediaType
	if manifestMediaType == mediaTypeOCIImageIndex || manifestMediaType == mediaTypeDockerManifestList {
		var idx manifestList
		if err := json.Unmarshal(body, &idx); err != nil {
			return remoteStatus{}, fmt.Errorf("decode manifest list: %w", err)
		}
		entry, ok := chooseManifest(idx.Manifests, cfg.Platform)
		if !ok {
			return remoteStatus{}, fmt.Errorf("no manifest for %s/%s", cfg.Platform.OS, cfg.Platform.Architecture)
		}
		manifestURL = fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, entry.Digest)
		manifestBody, manifestDigest, manifestMediaType, err = getRegistryJSON(ctx, client, manifestURL, token)
		if err != nil {
			return remoteStatus{}, err
		}
	}
	if manifestMediaType != mediaTypeOCIImageManifest && manifestMediaType != mediaTypeDockerManifestV2 {
		return remoteStatus{}, fmt.Errorf("unsupported manifest media type %q", manifestMediaType)
	}
	var manifest imageManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return remoteStatus{}, fmt.Errorf("decode manifest: %w", err)
	}
	configURL := fmt.Sprintf("https://ghcr.io/v2/%s/blobs/%s", repo, manifest.Config.Digest)
	configBody, _, _, err := getRegistryJSON(ctx, client, configURL, token)
	if err != nil {
		return remoteStatus{}, err
	}
	var cfgBlob imageConfigBlob
	if err := json.Unmarshal(configBody, &cfgBlob); err != nil {
		return remoteStatus{}, fmt.Errorf("decode config blob: %w", err)
	}
	labels := cfgBlob.Config.Labels
	return remoteStatus{
		Version:   labels["org.opencontainers.image.version"],
		Commit:    labels["org.opencontainers.image.revision"],
		BuildTime: labels["org.opencontainers.image.created"],
		Digest:    manifestDigest,
	}, nil
}

func registryToken(ctx context.Context, client *http.Client, cfg Config, repo string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, cfg.Tag), nil)
	if err != nil {
		return "", err
	}
	addAccept(req)
	if cfg.Username != "" && cfg.Token != "" {
		req.Header.Set("Authorization", basicAuth(cfg.Username, cfg.Token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "", nil
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return "", fmt.Errorf("registry probe: unexpected status %s", resp.Status)
	}
	challenge, err := parseChallenge(resp.Header.Get("Www-Authenticate"))
	if err != nil {
		return "", err
	}
	if challenge.Scope == "" {
		challenge.Scope = "repository:" + repo + ":pull"
	}
	tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", challenge.Realm, challenge.Service, challenge.Scope)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	if cfg.Username != "" && cfg.Token != "" {
		tokenReq.Header.Set("Authorization", basicAuth(cfg.Username, cfg.Token))
	}
	resp, err = client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("registry token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry token request: unexpected status %s", resp.Status)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode registry token: %w", err)
	}
	return payload.Token, nil
}

func getRegistryJSON(ctx context.Context, client *http.Client, url, token string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", err
	}
	addAccept(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("registry request %s: unexpected status %s", url, resp.Status)
	}
	var body any
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&body); err != nil {
		return nil, "", "", fmt.Errorf("decode registry response: %w", err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", "", err
	}
	return encoded, resp.Header.Get("Docker-Content-Digest"), resp.Header.Get("Content-Type"), nil
}

func addAccept(req *http.Request) {
	req.Header.Set("Accept", strings.Join([]string{
		mediaTypeOCIImageIndex,
		mediaTypeDockerManifestList,
		mediaTypeOCIImageManifest,
		mediaTypeDockerManifestV2,
		"application/json",
	}, ", "))
}

func parseChallenge(header string) (authChallenge, error) {
	if header == "" {
		return authChallenge{}, fmt.Errorf("missing registry auth challenge")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return authChallenge{}, fmt.Errorf("bad registry auth challenge %q", header)
	}
	var out authChallenge
	for _, part := range strings.Split(parts[1], ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := kv[0]
		value := strings.Trim(kv[1], `"`)
		switch key {
		case "realm":
			out.Realm = value
		case "service":
			out.Service = value
		case "scope":
			out.Scope = value
		}
	}
	if out.Realm == "" || out.Service == "" {
		return authChallenge{}, fmt.Errorf("incomplete registry auth challenge %q", header)
	}
	return out, nil
}

func chooseManifest(entries []manifestListEntry, platform Platform) (manifestListEntry, bool) {
	for _, entry := range entries {
		if entry.Platform.OS != platform.OS || entry.Platform.Architecture != platform.Architecture {
			continue
		}
		if platform.Variant == "" || entry.Platform.Variant == "" || entry.Platform.Variant == platform.Variant {
			return entry, true
		}
	}
	return manifestListEntry{}, false
}

func compareRemote(current BuildInfo, remote remoteStatus) bool {
	if current.Commit != "" && current.Commit != "unknown" && remote.Commit != "" {
		return !strings.EqualFold(current.Commit, remote.Commit)
	}
	if current.Version != "" && current.Version != "dev" && remote.Version != "" {
		return current.Version != remote.Version
	}
	return false
}

func normalizeImage(image string) (string, error) {
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(image, prefix) {
		return "", fmt.Errorf("unsupported image %q: only ghcr.io images supported", image)
	}
	repo := strings.TrimPrefix(image, prefix)
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return "", fmt.Errorf("bad image %q", image)
	}
	return repo, nil
}

func basicAuth(username, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+token))
}
