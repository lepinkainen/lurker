package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BackendURL string `yaml:"backend_url"`
}

// loadConfig reads config from the explicit path, then falls back to
// ./tui-config.yaml, then ~/.config/lurker/tui.yaml.
// If no file is found the returned config uses defaults.
func loadConfig(explicitPath string) (*Config, error) {
	cfg := &Config{
		BackendURL: "http://localhost:8080",
	}

	candidates := []string{}
	if explicitPath != "" {
		candidates = []string{explicitPath}
	} else {
		candidates = append(candidates, "tui-config.yaml")
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "lurker", "tui.yaml"))
		}
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
		break
	}

	return cfg, nil
}
