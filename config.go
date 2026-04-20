package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lepinkainen/research/irc-service/irc"
)

type Config struct {
	DataDir       string
	ControlDBPath string
	Addr          string
	ConfigPath    string
	Networks      []irc.NetworkConfig
}

type FileConfig struct {
	Nick     string              `yaml:"nick"`
	User     string              `yaml:"user"`
	Realname string              `yaml:"realname"`
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
	}
	if nets, err := loadNetworksFromYAML(cfg.ConfigPath); err == nil {
		cfg.Networks = nets
	}
	return cfg
}

func loadNetworksFromYAML(path string) ([]irc.NetworkConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var fc FileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return nil, fmt.Errorf("parse yaml %s: %w", path, err)
	}
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
