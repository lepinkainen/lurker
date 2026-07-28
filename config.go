package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lepinkainen/lurker/datasource/bluesky"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

type Config struct {
	DataDir       string
	ControlDBPath string
	Addr          string
	ConfigPath    string
	ThemesDir     string
	Networks      []irc.NetworkConfig
	DataSources   DataSourcesConfig
	Previews      PreviewConfig
	Updates       UpdateConfig
	Uploads       UploadConfig
}

// DataSourcesConfig holds parsed datasource configurations. Each adapter
// gets its own slice; main wires them into datasource.Manager.
type DataSourcesConfig struct {
	Bluesky []bluesky.Config
}

// PreviewConfig mirrors the YAML `previews:` block.
type PreviewConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaxBytes      int  `yaml:"max_bytes"`
	TimeoutMs     int  `yaml:"timeout_ms"`
	CacheTTLHours int  `yaml:"cache_ttl_hours"`
	Workers       int  `yaml:"workers"`
}

type UpdateConfig struct {
	Enabled  bool
	Image    string
	Tag      string
	Interval time.Duration
	Username string
	Token    string
}

type UploadConfig struct {
	Dir      string
	MaxBytes int64
	BaseURL  string
}

type FileConfig struct {
	Nick        string                 `yaml:"nick,omitempty"`
	User        string                 `yaml:"user,omitempty"`
	Realname    string                 `yaml:"realname,omitempty"`
	Previews    *PreviewConfig         `yaml:"previews,omitempty"`
	Networks    []NetworkFileConfig    `yaml:"networks"`
	DataSources *DataSourcesFileConfig `yaml:"data_sources,omitempty"`
}

// DataSourcesFileConfig mirrors the YAML `data_sources:` block.
type DataSourcesFileConfig struct {
	Bluesky []BlueskyFileConfig `yaml:"bluesky,omitempty"`
}

// BlueskyFileConfig is one entry under `data_sources.bluesky[]`.
type BlueskyFileConfig struct {
	Network     string                     `yaml:"network"`
	Identifier  string                     `yaml:"identifier"`
	AppPassword string                     `yaml:"app_password"`
	PDS         string                     `yaml:"pds,omitempty"`
	Channels    []BlueskyChannelFileConfig `yaml:"channels,omitempty"`
}

// BlueskyChannelFileConfig is one channel under a Bluesky account. Today
// only `kind: timeline` is implemented; other kinds parse but are deferred.
type BlueskyChannelFileConfig struct {
	Kind  string `yaml:"kind"`
	Name  string `yaml:"name,omitempty"`
	URI   string `yaml:"uri,omitempty"`
	Query string `yaml:"query,omitempty"`
}

type NetworkFileConfig struct {
	Network         string   `yaml:"network"`
	Nick            string   `yaml:"nick,omitempty"`
	User            string   `yaml:"user,omitempty"`
	Realname        string   `yaml:"realname,omitempty"`
	Channels        []string `yaml:"channels,omitempty"`
	ConnectCommands []string `yaml:"connect_commands,omitempty"`
	SASLUser        string   `yaml:"sasl_user,omitempty"`
	SASLPass        string   `yaml:"sasl_pass,omitempty"`
	Password        string   `yaml:"server_password,omitempty"`
	// TLSVerify enables TLS certificate verification for this network's
	// servers. Defaults to false (verification OFF) to tolerate networks
	// with broken round-robin certs.
	TLSVerify bool               `yaml:"tls_verify,omitempty"`
	Servers   []ServerFileConfig `yaml:"servers"`
}

type ServerFileConfig struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port,omitempty"`
	TLS           *bool  `yaml:"tls,omitempty"`
	TLSMaxVersion string `yaml:"tls_max_version,omitempty"`
}

func loadConfig() Config {
	dataDir := envOr("DATA_DIR", "./data")
	cfg := Config{
		DataDir:       dataDir,
		ControlDBPath: dataDir + "/control.db",
		Addr:          envOr("ADDR", ":8080"),
		ConfigPath:    envOr("CONFIG_PATH", "./config.yaml"),
		ThemesDir:     envOr("THEMES_DIR", "./themes"),
		Previews: PreviewConfig{
			Enabled:       true,
			MaxBytes:      512 * 1024,
			TimeoutMs:     5000,
			CacheTTLHours: 24 * 7,
			Workers:       4,
		},
		Updates: UpdateConfig{
			Enabled:  envBoolOr("UPDATE_CHECK_ENABLED", true),
			Image:    envOr("UPDATE_CHECK_IMAGE", "ghcr.io/lepinkainen/lurker"),
			Tag:      envOr("UPDATE_CHECK_TAG", "latest"),
			Interval: clampUpdateInterval(envDurationOr("UPDATE_CHECK_INTERVAL", 24*time.Hour)),
			Username: os.Getenv("GHCR_USERNAME"),
			Token:    os.Getenv("GHCR_TOKEN"),
		},
		Uploads: UploadConfig{
			Dir:      envOr("UPLOAD_DIR", dataDir+"/uploads"),
			MaxBytes: envInt64Or("UPLOAD_MAX_BYTES", 20*1024*1024),
			BaseURL:  strings.TrimSpace(os.Getenv("UPLOAD_BASE_URL")),
		},
	}
	nets, pv, ds, err := parseYAMLConfig(cfg.ConfigPath)
	if err != nil {
		// A long-running bouncer must never boot on a partially-valid
		// config. Any failure — missing file included — is fatal: never
		// reason with a broken config and run a half-configured backend.
		slog.Error("invalid config", "path", cfg.ConfigPath, "err", err)
		os.Exit(1)
	}
	cfg.Networks = nets
	cfg.DataSources = ds
	if pv != nil {
		if pv.MaxBytes > 0 {
			cfg.Previews.MaxBytes = pv.MaxBytes
		}
		if pv.TimeoutMs > 0 {
			cfg.Previews.TimeoutMs = pv.TimeoutMs
		}
		if pv.CacheTTLHours > 0 {
			cfg.Previews.CacheTTLHours = pv.CacheTTLHours
		}
		if pv.Workers > 0 {
			cfg.Previews.Workers = pv.Workers
		}
		cfg.Previews.Enabled = pv.Enabled
	}
	return cfg
}

func parseYAMLConfig(path string) ([]irc.NetworkConfig, *PreviewConfig, DataSourcesConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		// A missing config file is as fatal as a broken one: booting with an
		// implicit empty config would mark every network disabled and run a
		// half-configured backend. Fail fast so a misplaced file is noticed.
		return nil, nil, DataSourcesConfig{}, err
	}
	var fc FileConfig
	if yerr := yaml.Unmarshal(b, &fc); yerr != nil {
		return nil, nil, DataSourcesConfig{}, fmt.Errorf("parse yaml %s: %w", path, yerr)
	}
	nets, err := buildNetworks(fc)
	if err != nil {
		return nil, nil, DataSourcesConfig{}, err
	}
	ds, err := buildDataSources(fc)
	if err != nil {
		return nil, nil, DataSourcesConfig{}, err
	}
	return nets, fc.Previews, ds, nil
}

// envVarRE matches `${VAR}` exactly — no shell features, no defaults.
var envVarRE = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// expandEnvSecret returns os.Getenv(VAR) when v looks exactly like ${VAR},
// otherwise returns v unchanged. The strict form avoids accidentally
// expanding values that happen to contain "$" characters.
func expandEnvSecret(v string) string {
	m := envVarRE.FindStringSubmatch(v)
	if len(m) != 2 {
		return v
	}
	return os.Getenv(m[1])
}

func buildDataSources(fc FileConfig) (DataSourcesConfig, error) {
	var out DataSourcesConfig
	if fc.DataSources == nil {
		return out, nil
	}
	for _, bs := range fc.DataSources.Bluesky {
		cfg, err := buildBlueskyConfig(bs)
		if err != nil {
			return DataSourcesConfig{}, err
		}
		out.Bluesky = append(out.Bluesky, cfg)
	}
	return out, nil
}

func buildBlueskyConfig(b BlueskyFileConfig) (bluesky.Config, error) {
	name := strings.TrimSpace(b.Network)
	if name == "" {
		return bluesky.Config{}, fmt.Errorf("bluesky data source: network is required")
	}
	if strings.TrimSpace(b.Identifier) == "" {
		return bluesky.Config{}, fmt.Errorf("bluesky data source %q: identifier is required", name)
	}
	password := expandEnvSecret(strings.TrimSpace(b.AppPassword))
	if password == "" {
		return bluesky.Config{}, fmt.Errorf("bluesky data source %q: app_password is required (got empty after env expansion)", name)
	}

	channels := b.Channels
	if len(channels) == 0 {
		channels = []BlueskyChannelFileConfig{{Kind: string(bluesky.ChannelTimeline)}}
	}

	parsed := make([]bluesky.ChannelConfig, 0, len(channels))
	for _, ch := range channels {
		kind := bluesky.ChannelKind(strings.ToLower(strings.TrimSpace(ch.Kind)))
		if kind == "" {
			kind = bluesky.ChannelTimeline
		}
		switch kind {
		case bluesky.ChannelTimeline:
			parsed = append(parsed, bluesky.ChannelConfig{Kind: kind, Name: strings.TrimSpace(ch.Name)})
		case bluesky.ChannelSearch, bluesky.ChannelList, bluesky.ChannelFeed, bluesky.ChannelNotifications:
			// Reserved kinds parse but are not yet wired in fetchPage. Refuse
			// here so a misconfigured YAML fails loud instead of silently
			// idling.
			return bluesky.Config{}, fmt.Errorf("bluesky data source %q: channel kind %q is reserved for future use", name, kind)
		default:
			return bluesky.Config{}, fmt.Errorf("bluesky data source %q: unknown channel kind %q", name, kind)
		}
	}

	return bluesky.Config{
		Network:     name,
		Identifier:  strings.TrimSpace(b.Identifier),
		AppPassword: password,
		PDS:         strings.TrimSpace(b.PDS),
		Channels:    parsed,
	}, nil
}

func parseTLSMaxVersion(v string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return 0, nil
	case "1.2", "tls1.2", "tlsv1.2", "tls12", "tlsv12":
		return tls.VersionTLS12, nil
	case "1.3", "tls1.3", "tlsv1.3", "tls13", "tlsv13":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported tls_max_version %q", v)
	}
}

func buildNetworks(fc FileConfig) ([]irc.NetworkConfig, error) {
	var out []irc.NetworkConfig
	for _, n := range fc.Networks {
		if strings.TrimSpace(n.Network) == "" {
			return nil, fmt.Errorf("network name is required")
		}
		if len(n.Servers) == 0 {
			return nil, fmt.Errorf("network %q must define at least one server", n.Network)
		}
		nick, user, realname := resolveIdentity(fc, n)
		servers, err := buildServers(n)
		if err != nil {
			return nil, err
		}
		out = append(out, irc.NetworkConfig{
			Name:            n.Network,
			Servers:         servers,
			Nick:            nick,
			User:            user,
			Realname:        realname,
			Channels:        n.Channels,
			ConnectCommands: n.ConnectCommands,
			SASLUser:        n.SASLUser,
			SASLPass:        n.SASLPass,
			ServerPass:      n.Password,
		})
	}
	return out, nil
}

func resolveIdentity(fc FileConfig, n NetworkFileConfig) (nick, user, realname string) {
	nick = firstNonEmpty(n.Nick, fc.Nick, "ircsvc")
	user = firstNonEmpty(n.User, fc.User, nick)
	realname = firstNonEmpty(n.Realname, fc.Realname, nick)
	return
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func buildServers(n NetworkFileConfig) ([]irc.ServerConfig, error) {
	servers := make([]irc.ServerConfig, 0, len(n.Servers))
	for _, s := range n.Servers {
		tlsMaxVersion, err := parseTLSMaxVersion(s.TLSMaxVersion)
		if err != nil {
			return nil, fmt.Errorf("network %q server %q: %w", n.Network, s.Host, err)
		}
		useTLS := true
		if s.TLS != nil {
			useTLS = *s.TLS
		}
		servers = append(servers, irc.ServerConfig{
			Host:          s.Host,
			Port:          defaultServerPort(s.Port, useTLS),
			TLS:           useTLS,
			TLSInsecure:   !n.TLSVerify,
			TLSMaxVersion: tlsMaxVersion,
		})
	}
	return servers, nil
}

func defaultServerPort(port int, useTLS bool) int {
	if port != 0 {
		return port
	}
	if useTLS {
		return 6697
	}
	return 6667
}

// previewConfigYAML returns the current raw config file content and a proposed
// YAML that merges the DB network list into the existing file. For each DB
// network, the connection fields (host/port/tls/nick/realname/sasl) are
// updated while channels and extra servers are preserved from the current file.
// Networks present in the file but absent from the DB are removed.
func previewConfigYAML(configPath string, networks []ircdb.Network) (current, proposed string, err error) {
	b, readErr := os.ReadFile(configPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", "", readErr
	}
	current = string(b)

	var fc FileConfig
	if len(b) > 0 {
		if yerr := yaml.Unmarshal(b, &fc); yerr != nil {
			return "", "", fmt.Errorf("parse existing yaml: %w", yerr)
		}
	}

	globalNick := firstNonEmpty(fc.Nick, "ircsvc")
	globalRealname := firstNonEmpty(fc.Realname, globalNick)

	existingByName := make(map[string]NetworkFileConfig, len(fc.Networks))
	for _, n := range fc.Networks {
		existingByName[strings.ToLower(strings.TrimSpace(n.Network))] = n
	}

	merged := make([]NetworkFileConfig, 0, len(networks))
	for _, n := range networks {
		entry := mergeNetworkEntry(existingByName, n, globalNick, globalRealname)
		merged = append(merged, entry)
	}
	fc.Networks = merged

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if yerr := enc.Encode(fc); yerr != nil {
		return "", "", fmt.Errorf("marshal yaml: %w", yerr)
	}
	return current, buf.String(), nil
}

// mergeNetworkEntry returns the YAML entry for a DB network n, preserving
// channels/extra servers from the existing YAML index when present.
func mergeNetworkEntry(existing map[string]NetworkFileConfig, n ircdb.Network, globalNick, globalRealname string) NetworkFileConfig {
	key := strings.ToLower(strings.TrimSpace(n.Name))
	entry, found := existing[key]
	if !found {
		entry = NetworkFileConfig{Network: n.Name}
	}
	entry.Network = n.Name
	entry.Nick = overrideIfDifferent(n.Nick, globalNick)
	entry.Realname = overrideIfDifferent(n.Realname, globalRealname)
	entry.ConnectCommands = n.ConnectCommands
	entry.SASLUser = n.SASLUser
	entry.SASLPass = n.SASLPass
	entry.Password = n.ServerPass
	useTLS := n.TLS
	if len(entry.Servers) > 0 {
		tlsMaxVersion := entry.Servers[0].TLSMaxVersion
		entry.Servers[0] = ServerFileConfig{Host: n.Host, Port: n.Port, TLS: &useTLS, TLSMaxVersion: tlsMaxVersion}
	} else {
		entry.Servers = []ServerFileConfig{{Host: n.Host, Port: n.Port, TLS: &useTLS}}
	}
	return entry
}

func overrideIfDifferent(value, globalDefault string) string {
	if value == globalDefault {
		return ""
	}
	return value
}

// saveConfigYAML atomically writes content to configPath by writing to a temp
// file then renaming.
func saveConfigYAML(configPath, content string) error {
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBoolOr(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return parsed
}

func envDurationOr(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return parsed
}

func envInt64Or(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

func clampUpdateInterval(v time.Duration) time.Duration {
	if v <= 0 {
		return 24 * time.Hour
	}
	if v < time.Hour {
		return time.Hour
	}
	return v
}
