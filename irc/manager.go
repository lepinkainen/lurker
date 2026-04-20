// Package irc manages persistent IRC connections and writes inbound events
// into the SQLite store.
package irc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/lrstanley/girc"

	ircdb "github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/hub"
)

// debugWriter returns os.Stderr when IRC_DEBUG is set, io.Discard otherwise.
func debugWriter() io.Writer {
	if os.Getenv("IRC_DEBUG") != "" {
		return os.Stderr
	}
	return io.Discard
}

type ServerConfig struct {
	Host        string // hostname of the IRC server
	Port        int    // TCP port
	TLS         bool   // use TLS
	TLSInsecure bool   // skip TLS certificate verification
}

// NetworkConfig is the runtime configuration for a single logical IRC network.
type NetworkConfig struct {
	Name     string         // display/unique key, e.g. "libera"
	Servers  []ServerConfig // ordered failover / round-robin candidates
	Nick     string         // desired nickname
	User     string         // ident
	Realname string         // realname / gecos
	Channels []string       // channels to autojoin after connect
	SASLUser string         // empty disables SASL
	SASLPass string
}

func (n NetworkConfig) PrimaryServer() ServerConfig {
	if len(n.Servers) == 0 {
		return ServerConfig{}
	}
	return n.Servers[0]
}

func (n NetworkConfig) serverAt(i int) ServerConfig {
	if len(n.Servers) == 0 {
		return ServerConfig{}
	}
	return n.Servers[i%len(n.Servers)]
}

// Manager owns one IRC client per network.
type networkRuntime struct {
	cfg    NetworkConfig
	cancel context.CancelFunc
}

type Manager struct {
	stores  *ircdb.MultiStore
	hub     *hub.Hub
	wg      sync.WaitGroup
	mu      sync.Mutex
	conn    map[int64]*girc.Client
	state   map[int64]string
	runtime map[int64]networkRuntime
}

func NewManager(stores *ircdb.MultiStore, h *hub.Hub) *Manager {
	return &Manager{stores: stores, hub: h, conn: map[int64]*girc.Client{}, state: map[int64]string{}, runtime: map[int64]networkRuntime{}}
}

func (m *Manager) Start(ctx context.Context, nets []NetworkConfig) error {
	for _, nc := range nets {
		server := nc.PrimaryServer()
		nrow, err := m.stores.UpsertNetwork(ctx, ircdb.Network{
			Name:     nc.Name,
			Host:     server.Host,
			Port:     server.Port,
			TLS:      server.TLS,
			Nick:     nc.Nick,
			Realname: nc.Realname,
			SASLUser: nc.SASLUser,
			SASLPass: nc.SASLPass,
		})
		if err != nil {
			return err
		}
		if err := m.StartNetwork(ctx, nrow.ID, nc); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) StartNetwork(parent context.Context, networkID int64, nc NetworkConfig) error {
	m.mu.Lock()
	if _, ok := m.runtime[networkID]; ok {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	m.runtime[networkID] = networkRuntime{cfg: nc, cancel: cancel}
	m.state[networkID] = StateDisconnected.String()
	m.mu.Unlock()

	m.wg.Go(func() {
		m.runNetwork(ctx, networkID, nc)
	})
	return nil
}

func (m *Manager) StopNetwork(networkID int64) error {
	m.mu.Lock()
	rt, ok := m.runtime[networkID]
	if !ok {
		m.state[networkID] = StateDisconnected.String()
		m.mu.Unlock()
		return nil
	}
	delete(m.runtime, networkID)
	m.state[networkID] = StateDisconnected.String()
	c := m.conn[networkID]
	m.mu.Unlock()

	rt.cancel()
	if c != nil {
		c.Close()
	}
	if m.hub != nil {
		m.hub.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: networkID, State: StateDisconnected.String()})
	}
	return nil
}

func (m *Manager) Wait() { m.wg.Wait() }

var ErrNotConnected = errors.New("irc: network not connected")

func (m *Manager) Send(networkID int64, target, content string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Message(target, content)
	return nil
}

func (m *Manager) Join(networkID int64, channel string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Join(channel)
	return nil
}

func (m *Manager) Part(networkID int64, channel, reason string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Part(channel, reason)
	return nil
}

func (m *Manager) StateSnapshot() map[int64]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]string, len(m.state))
	for id, state := range m.state {
		out[id] = state
	}
	return out
}

func (m *Manager) runNetwork(ctx context.Context, networkID int64, nc NetworkConfig) {
	log := slog.With("network", nc.Name, "network_id", networkID)
	backoff := time.Second
	const maxBackoff = 5 * time.Minute
	serverIndex := 0

	for {
		if ctx.Err() != nil {
			return
		}
		server := nc.serverAt(serverIndex)
		client := m.buildClient(ctx, networkID, nc, server)

		m.mu.Lock()
		m.conn[networkID] = client
		m.state[networkID] = StateConnecting.String()
		m.mu.Unlock()
		if m.hub != nil {
			m.hub.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: networkID, State: StateConnecting.String()})
		}

		log.Info("connecting", "host", server.Host, "port", server.Port, "tls", server.TLS)
		err := client.Connect()

		m.mu.Lock()
		delete(m.conn, networkID)
		if _, ok := m.runtime[networkID]; ok {
			m.state[networkID] = StateDisconnected.String()
		}
		m.mu.Unlock()
		if m.hub != nil {
			m.hub.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: networkID, State: StateDisconnected.String()})
		}

		if ctx.Err() != nil {
			log.Info("connection closed on shutdown")
			return
		}
		if err != nil {
			log.Warn("connection failed", "err", err, "backoff", backoff)
		} else {
			log.Info("connection ended, reconnecting", "backoff", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if len(nc.Servers) > 0 {
			serverIndex = (serverIndex + 1) % len(nc.Servers)
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (m *Manager) buildClient(ctx context.Context, networkID int64, nc NetworkConfig, server ServerConfig) *girc.Client {
	user := nc.User
	if user == "" {
		user = nc.Nick
	}
	cfg := girc.Config{
		Server:      server.Host,
		Port:        server.Port,
		SSL:         server.TLS,
		Nick:        nc.Nick,
		User:        user,
		Name:        nc.Realname,
		Version:     "irc-service",
		PingDelay:   60 * time.Second,
		PingTimeout: 30 * time.Second,
		RecoverFunc: girc.DefaultRecoverHandler,
		Debug:       debugWriter(),
		SupportedCaps: map[string][]string{
			"echo-message":     nil,
			"labeled-response": nil,
		},
	}
	if server.TLS {
		cfg.TLSConfig = &tls.Config{
			ServerName:         server.Host,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: server.TLSInsecure,
		}
	}
	if nc.SASLUser != "" {
		cfg.SASL = &girc.SASLPlain{User: nc.SASLUser, Pass: nc.SASLPass}
	}

	client := girc.New(cfg)
	logStore, err := m.stores.LogStore(networkID)
	if err != nil {
		slog.Error("log store", "err", err, "network_id", networkID)
		return client
	}
	h := &handler{stores: m.stores, db: logStore.DB, hub: m.hub, networkID: networkID, networkName: nc.Name, autojoin: nc.Channels, connectedHook: func() {
		m.mu.Lock()
		m.state[networkID] = StateConnected.String()
		m.mu.Unlock()
	}}
	h.register(client)

	go func() {
		<-ctx.Done()
		client.Close()
	}()

	return client
}
