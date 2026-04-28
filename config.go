package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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
	Previews      PreviewConfig
	Updates       UpdateConfig
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

type FileConfig struct {
	Nick     string              `yaml:"nick,omitempty"`
	User     string              `yaml:"user,omitempty"`
	Realname string              `yaml:"realname,omitempty"`
	Previews *PreviewConfig      `yaml:"previews,omitempty"`
	Networks []NetworkFileConfig `yaml:"networks"`
}

type NetworkFileConfig struct {
	Network  string             `yaml:"network"`
	Nick     string             `yaml:"nick,omitempty"`
	User     string             `yaml:"user,omitempty"`
	Realname string             `yaml:"realname,omitempty"`
	Channels []string           `yaml:"channels,omitempty"`
	SASLUser string             `yaml:"sasl_user,omitempty"`
	SASLPass string             `yaml:"sasl_pass,omitempty"`
	Servers  []ServerFileConfig `yaml:"servers"`
}

type ServerFileConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port,omitempty"`
	TLS  *bool  `yaml:"tls,omitempty"`
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
	}
	if nets, pv, err := parseYAMLConfig(cfg.ConfigPath); err == nil {
		cfg.Networks = nets
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
	}
	return cfg
}

func parseYAMLConfig(path string) ([]irc.NetworkConfig, *PreviewConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var fc FileConfig
	if yerr := yaml.Unmarshal(b, &fc); yerr != nil {
		return nil, nil, fmt.Errorf("parse yaml %s: %w", path, yerr)
	}
	nets, err := buildNetworks(fc)
	if err != nil {
		return nil, nil, err
	}
	return nets, fc.Previews, nil
}

func loadNetworksFromYAML(path string) ([]irc.NetworkConfig, error) {
	nets, _, err := parseYAMLConfig(path)
	return nets, err
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
		nick := n.Nick
		if nick == "" {
			nick = fc.Nick
		}
		if nick == "" {
			nick = "ircsvc"
		}
		user := n.User
		if user == "" {
			user = fc.User
		}
		if user == "" {
			user = nick
		}
		realname := n.Realname
		if realname == "" {
			realname = fc.Realname
		}
		if realname == "" {
			realname = nick
		}

		servers := make([]irc.ServerConfig, 0, len(n.Servers))
		for _, s := range n.Servers {
			useTLS := true
			if s.TLS != nil {
				useTLS = *s.TLS
			}
			port := s.Port
			if port == 0 {
				if useTLS {
					port = 6697
				} else {
					port = 6667
				}
			}
			servers = append(servers, irc.ServerConfig{
				Host:        s.Host,
				Port:        port,
				TLS:         useTLS,
				TLSInsecure: true,
			})
		}
		out = append(out, irc.NetworkConfig{
			Name:     n.Network,
			Servers:  servers,
			Nick:     nick,
			User:     user,
			Realname: realname,
			Channels: n.Channels,
			SASLUser: n.SASLUser,
			SASLPass: n.SASLPass,
		})
	}
	return out, nil
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

	// Compute effective global defaults (same logic as buildNetworks).
	globalNick := fc.Nick
	if globalNick == "" {
		globalNick = "ircsvc"
	}
	globalRealname := fc.Realname
	if globalRealname == "" {
		globalRealname = globalNick
	}

	// Index existing YAML networks by normalised name.
	existingByName := make(map[string]NetworkFileConfig, len(fc.Networks))
	for _, n := range fc.Networks {
		existingByName[strings.ToLower(strings.TrimSpace(n.Network))] = n
	}

	// Build new list in DB sort order. Disabled networks are excluded from the
	// export (ListNetworksWithSASL already filters them). Networks in DB but
	// not in the existing YAML get a minimal new entry; networks in YAML but
	// not in DB are dropped.
	merged := make([]NetworkFileConfig, 0, len(networks))
	for _, n := range networks {
		key := strings.ToLower(strings.TrimSpace(n.Name))
		entry, found := existingByName[key]
		if !found {
			entry = NetworkFileConfig{Network: n.Name}
		}
		entry.Network = n.Name
		// Only write per-network nick/realname when they differ from the global
		// default; otherwise leave empty so the global applies.
		if n.Nick == globalNick {
			entry.Nick = ""
		} else {
			entry.Nick = n.Nick
		}
		if n.Realname == globalRealname {
			entry.Realname = ""
		} else {
			entry.Realname = n.Realname
		}
		// user (ident) is not stored in DB — preserve from original YAML entry.
		entry.SASLUser = n.SASLUser
		entry.SASLPass = n.SASLPass
		tls := n.TLS
		if len(entry.Servers) > 0 {
			entry.Servers[0] = ServerFileConfig{Host: n.Host, Port: n.Port, TLS: &tls}
		} else {
			entry.Servers = []ServerFileConfig{{Host: n.Host, Port: n.Port, TLS: &tls}}
		}
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

func clampUpdateInterval(v time.Duration) time.Duration {
	if v <= 0 {
		return 24 * time.Hour
	}
	if v < time.Hour {
		return time.Hour
	}
	return v
}
