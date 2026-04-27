package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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
	Nick     string              `yaml:"nick"`
	User     string              `yaml:"user"`
	Realname string              `yaml:"realname"`
	Previews *PreviewConfig      `yaml:"previews"`
	Networks []NetworkFileConfig `yaml:"networks"`
}

type NetworkFileConfig struct {
	Network  string             `yaml:"network"`
	Nick     string             `yaml:"nick"`
	User     string             `yaml:"user"`
	Realname string             `yaml:"realname"`
	Channels []string           `yaml:"channels"`
	SASLUser string             `yaml:"sasl_user"`
	SASLPass string             `yaml:"sasl_pass"`
	Servers  []ServerFileConfig `yaml:"servers"`
}

type ServerFileConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	TLS  *bool  `yaml:"tls"`
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
