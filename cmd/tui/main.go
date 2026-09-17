package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	configPath := flag.String("config", "", "path to tui config YAML (default: ./tui-config.yaml or ~/.config/lurker/tui.yaml)")
	backendURL := flag.String("url", "", "backend URL (overrides config file)")
	stateDirFlag := flag.String("state-dir", "", "directory for tui-state.json (default: <user config dir>/lurker)")
	flag.Parse()

	stateDir = *stateDirFlag

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if *backendURL != "" {
		cfg.BackendURL = *backendURL
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
