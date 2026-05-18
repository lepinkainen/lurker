package datasource

import (
	"context"
	"fmt"
	"sync"
)

// Manager owns the lifecycle of all configured Sources. Mirrors irc.Manager.
type Manager struct {
	deps    Deps
	mu      sync.Mutex
	sources []Source
}

// NewManager constructs a Manager.
func NewManager(deps Deps) *Manager {
	return &Manager{deps: deps}
}

// Register adds a Source to be started by Start. Safe to call before Start.
func (m *Manager) Register(s Source) {
	if s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources = append(m.sources, s)
}

// Start launches every registered source. Returns the first start error;
// already-started sources keep running.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	srcs := append([]Source(nil), m.sources...)
	m.mu.Unlock()
	for _, s := range srcs {
		if err := s.Start(ctx, m.deps); err != nil {
			return fmt.Errorf("start source %s/%s: %w", s.Kind(), s.Name(), err)
		}
	}
	return nil
}

// Wait blocks until every registered source has stopped.
func (m *Manager) Wait() {
	m.mu.Lock()
	srcs := append([]Source(nil), m.sources...)
	m.mu.Unlock()
	for _, s := range srcs {
		s.Wait()
	}
}

// Names returns the network names of every registered source.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.sources))
	for _, s := range m.sources {
		out = append(out, s.Name())
	}
	return out
}
