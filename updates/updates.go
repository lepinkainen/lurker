package updates

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lepinkainen/lurker/internal/httpjson"
)

// Config controls periodic checks for a newer published backend image.
type Config struct {
	Enabled  bool
	Repo     string
	Interval time.Duration
	Current  BuildInfo
	Client   *http.Client
	Logger   *slog.Logger
}

// BuildInfo describes currently running server build.
type BuildInfo struct {
	Commit string
}

// Status is cached update-check result exposed by API.
type Status struct {
	Enabled         bool      `json:"enabled"`
	RemoteVersion   string    `json:"remote_version,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
	UpdateAvailable bool      `json:"update_available"`
	Error           string    `json:"error,omitempty"`
}

// Checker polls GitHub for the latest successful release run and keeps the
// last status in memory. The image is built from the head SHA of that run
// (release.yml bakes it into main.gitHash), so a differing SHA means a newer
// image is published.
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
	if cfg.Repo == "" {
		cfg.Repo = "lepinkainen/lurker"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.Interval < time.Hour {
		cfg.Interval = time.Hour
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Checker{
		cfg:    cfg,
		hjc:    &httpjson.Client{HTTP: cfg.Client},
		logger: cfg.Logger,
		status: Status{Enabled: cfg.Enabled},
	}
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
	remote, err := fetchLatestSHA(ctx, c.hjc, c.cfg.Repo)
	st.CheckedAt = time.Now().UTC()
	if err != nil {
		st.Error = err.Error()
		c.setStatus(st)
		c.logError(err)
		return
	}
	st.RemoteVersion = shortSHA(remote)
	st.UpdateAvailable = updateAvailable(c.cfg.Current.Commit, remote)
	st.Error = ""
	c.setStatus(st)
	c.logTransition(st)
}

func (c *Checker) setStatus(st Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = st
}

// fetchLatestSHA returns the head SHA of the newest successful release
// workflow run, i.e. the commit the currently published image was built from.
func fetchLatestSHA(ctx context.Context, hjc *httpjson.Client, repo string) (string, error) {
	var payload struct {
		WorkflowRuns []struct {
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_runs"`
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/release.yml/runs?status=success&per_page=1", repo)
	if err := hjc.DoJSON(ctx, httpjson.Request{URL: url}, &payload); err != nil {
		return "", fmt.Errorf("release runs request: %w", err)
	}
	if len(payload.WorkflowRuns) == 0 || payload.WorkflowRuns[0].HeadSHA == "" {
		return "", fmt.Errorf("no successful release runs for %s", repo)
	}
	return payload.WorkflowRuns[0].HeadSHA, nil
}

// updateAvailable compares the running build's commit with the released one.
// Dev builds ("unknown"/empty) never report an update. Prefix comparison
// tolerates a short hash on either side.
func updateAvailable(current, remote string) bool {
	if current == "" || current == "unknown" || remote == "" {
		return false
	}
	current = strings.ToLower(current)
	remote = strings.ToLower(remote)
	return !strings.HasPrefix(remote, current) && !strings.HasPrefix(current, remote)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (c *Checker) logTransition(st Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st.UpdateAvailable == c.lastLoggedUpdate {
		return
	}
	c.lastLoggedUpdate = st.UpdateAvailable
	if st.UpdateAvailable {
		c.logger.Info("update available", "current_commit", c.cfg.Current.Commit, "remote_commit", st.RemoteVersion)
		return
	}
	c.logger.Info("update status current", "current_commit", c.cfg.Current.Commit, "remote_commit", st.RemoteVersion)
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
	c.logger.Warn("update check failed", "repo", c.cfg.Repo, "err", err)
}
