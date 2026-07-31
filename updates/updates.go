package updates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lepinkainen/lurker/internal/httpjson"
)

// Config controls periodic remote image metadata checks.
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

// Platform selects image manifest variant for multi-arch images.
type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

// BuildInfo describes currently running server build.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

// Status is cached update-check result exposed by API.
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
	CheckedAt        time.Time `json:"checked_at"`
	UpdateAvailable  bool      `json:"update_available"`
	Error            string    `json:"error,omitempty"`
}

// Checker polls registry metadata and keeps last status in memory.
type Checker struct {
	cfg    Config
	hjc    *httpjson.Client
	logger *slog.Logger

	mu                sync.RWMutex
	status            Status
	lastLoggedUpdate  bool
	lastErrorLoggedAt time.Time
	lastErrorMessage  string
}

// New creates update checker with defaults applied.
func New(cfg Config) *Checker {
	if cfg.Image == "" {
		cfg.Image = "ghcr.io/lepinkainen/lurker"
	}
	if cfg.Tag == "" {
		cfg.Tag = "latest"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.Interval < time.Hour {
		cfg.Interval = time.Hour
	}
	if cfg.Platform.OS == "" {
		cfg.Platform.OS = "linux"
	}
	if cfg.Platform.Architecture == "" {
		cfg.Platform.Architecture = "amd64"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Checker{
		cfg:    cfg,
		hjc:    &httpjson.Client{HTTP: cfg.Client},
		logger: cfg.Logger,
	}
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

// Start runs periodic checks until context ends.
func (c *Checker) Start(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	go c.run(ctx)
}

// CheckNow runs one immediate check.
func (c *Checker) CheckNow(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	c.check(ctx)
}

// Status returns latest cached check result.
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
	remote, err := fetchRemoteStatus(ctx, c.hjc, c.cfg)
	st.CheckedAt = time.Now().UTC()
	if err != nil {
		st.Error = err.Error()
		c.setStatus(st)
		c.logError(err)
		return
	}
	st.RemoteVersion = remote.Version
	st.RemoteCommit = remote.Commit
	st.RemoteBuildTime = remote.BuildTime
	st.RemoteDigest = remote.Digest
	st.UpdateAvailable = compareRemote(c.cfg.Current, remote)
	st.Error = ""
	c.setStatus(st)
	c.logTransition(st)
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

func fetchRemoteStatus(ctx context.Context, hjc *httpjson.Client, cfg Config) (remoteStatus, error) {
	repo, err := normalizeImage(cfg.Image)
	if err != nil {
		return remoteStatus{}, err
	}
	token, err := registryToken(ctx, hjc, cfg, repo)
	if err != nil {
		return remoteStatus{}, err
	}
	manifestURL := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, cfg.Tag)
	body, digest, mediaType, err := getRegistryJSON(ctx, hjc, manifestURL, token)
	if err != nil {
		return remoteStatus{}, err
	}
	manifestDigest := digest
	manifestBody := body
	manifestMediaType := mediaType
	if manifestMediaType == mediaTypeOCIImageIndex || manifestMediaType == mediaTypeDockerManifestList {
		var idx manifestList
		unmarshalErr := json.Unmarshal(body, &idx)
		if unmarshalErr != nil {
			return remoteStatus{}, fmt.Errorf("decode manifest list: %w", unmarshalErr)
		}
		entry, ok := chooseManifest(idx.Manifests, cfg.Platform)
		if !ok {
			return remoteStatus{}, fmt.Errorf("no manifest for %s/%s", cfg.Platform.OS, cfg.Platform.Architecture)
		}
		manifestURL = fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, entry.Digest)
		manifestBody, manifestDigest, manifestMediaType, err = getRegistryJSON(ctx, hjc, manifestURL, token)
		if err != nil {
			return remoteStatus{}, err
		}
	}
	if manifestMediaType != mediaTypeOCIImageManifest && manifestMediaType != mediaTypeDockerManifestV2 {
		return remoteStatus{}, fmt.Errorf("unsupported manifest media type %q", manifestMediaType)
	}
	var manifest imageManifest
	unmarshalErr := json.Unmarshal(manifestBody, &manifest)
	if unmarshalErr != nil {
		return remoteStatus{}, fmt.Errorf("decode manifest: %w", unmarshalErr)
	}
	configURL := fmt.Sprintf("https://ghcr.io/v2/%s/blobs/%s", repo, manifest.Config.Digest)
	configBody, _, _, err := getRegistryJSON(ctx, hjc, configURL, token)
	if err != nil {
		return remoteStatus{}, err
	}
	var cfgBlob imageConfigBlob
	unmarshalErr = json.Unmarshal(configBody, &cfgBlob)
	if unmarshalErr != nil {
		return remoteStatus{}, fmt.Errorf("decode config blob: %w", unmarshalErr)
	}
	labels := cfgBlob.Config.Labels
	return remoteStatus{
		Version:   labels["org.opencontainers.image.version"],
		Commit:    labels["org.opencontainers.image.revision"],
		BuildTime: labels["org.opencontainers.image.created"],
		Digest:    manifestDigest,
	}, nil
}

func registryToken(ctx context.Context, hjc *httpjson.Client, cfg Config, repo string) (string, error) {
	var auth string
	if cfg.Username != "" && cfg.Token != "" {
		auth = basicAuth(cfg.Username, cfg.Token)
	}
	_, err := hjc.Do(ctx, httpjson.Request{
		URL:           fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, cfg.Tag),
		Header:        registryAcceptHeader(),
		Authorization: auth,
	})
	if err == nil {
		return "", nil
	}
	var herr *httpjson.Error
	if !errors.As(err, &herr) {
		return "", fmt.Errorf("registry probe: %w", err)
	}
	if herr.Status != http.StatusUnauthorized {
		return "", fmt.Errorf("registry probe: unexpected status %d", herr.Status)
	}
	challenge, err := parseChallenge(herr.Header.Get("Www-Authenticate"))
	if err != nil {
		return "", err
	}
	if challenge.Scope == "" {
		challenge.Scope = "repository:" + repo + ":pull"
	}
	tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", challenge.Realm, challenge.Service, challenge.Scope)
	var payload struct {
		Token string `json:"token"`
	}
	if err := hjc.DoJSON(ctx, httpjson.Request{
		URL:           tokenURL,
		Authorization: auth,
	}, &payload); err != nil {
		return "", fmt.Errorf("registry token request: %w", err)
	}
	return payload.Token, nil
}

func (c *Checker) logTransition(st Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st.UpdateAvailable == c.lastLoggedUpdate {
		return
	}
	c.lastLoggedUpdate = st.UpdateAvailable
	if st.UpdateAvailable {
		c.logger.Info("update available", "image", st.Image, "tag", st.Tag, "current_commit", st.CurrentCommit, "remote_commit", st.RemoteCommit, "remote_digest", st.RemoteDigest)
		return
	}
	c.logger.Info("update status current", "image", st.Image, "tag", st.Tag, "current_commit", st.CurrentCommit, "remote_commit", st.RemoteCommit, "remote_digest", st.RemoteDigest)
}

func (c *Checker) logError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.lastErrorMessage == err.Error() && now.Sub(c.lastErrorLoggedAt) < time.Hour {
		return
	}
	c.lastErrorMessage = err.Error()
	c.lastErrorLoggedAt = now
	c.logger.Warn("update check failed", "image", c.cfg.Image, "tag", c.cfg.Tag, "err", err)
}

func getRegistryJSON(ctx context.Context, hjc *httpjson.Client, url, token string) (body []byte, digest string, mediaType string, err error) {
	var auth string
	if token != "" {
		auth = "Bearer " + token
	}
	resp, err := hjc.Do(ctx, httpjson.Request{
		URL:           url,
		Header:        registryAcceptHeader(),
		Authorization: auth,
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("registry request %s: %w", url, err)
	}
	mediaType = strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	return resp.Body, resp.Header.Get("Docker-Content-Digest"), mediaType, nil
}

func registryAcceptHeader() http.Header {
	return http.Header{"Accept": []string{strings.Join([]string{
		mediaTypeOCIImageIndex,
		mediaTypeDockerManifestList,
		mediaTypeOCIImageManifest,
		mediaTypeDockerManifestV2,
		"application/json",
	}, ", ")}}
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
	for part := range strings.SplitSeq(parts[1], ",") {
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
