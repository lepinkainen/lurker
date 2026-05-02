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
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lrstanley/girc"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

// debugWriter returns os.Stderr when IRC_DEBUG is set, io.Discard otherwise.
func debugWriter() io.Writer {
	if os.Getenv("IRC_DEBUG") != "" {
		return os.Stderr
	}
	return io.Discard
}

// ServerConfig describes one IRC server endpoint.
type ServerConfig struct {
	Host        string // hostname of the IRC server
	Port        int    // TCP port
	TLS         bool   // use TLS
	TLSInsecure bool   // skip TLS certificate verification
}

// NetworkConfig is the runtime configuration for a single logical IRC network.
type NetworkConfig struct {
	Name            string         // display/unique key, e.g. "libera"
	Servers         []ServerConfig // ordered failover / round-robin candidates
	Nick            string         // desired nickname
	User            string         // ident
	Realname        string         // realname / gecos
	Channels        []string       // channels to autojoin after connect
	ConnectCommands []string       // raw IRC lines sent after registration, before autojoin
	SASLUser        string         // empty disables SASL
	SASLPass        string
}

// PrimaryServer returns the first configured server, if any.
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

type connectorFunc func(ctx context.Context, client *girc.Client, server ServerConfig) error

// Manager owns IRC clients and connection lifecycle for all networks.
type Manager struct {
	stores         *ircdb.MultiStore
	hub            *hub.Hub
	previews       PreviewEnqueuer
	wg             sync.WaitGroup
	mu             sync.Mutex
	conn           map[uuid.UUID]*girc.Client
	state          map[uuid.UUID]string
	runtime        map[uuid.UUID]networkRuntime
	membersLoaded  map[uuid.UUID]map[string]bool
	joined         map[uuid.UUID]map[string]bool
	fixtureMembers map[uuid.UUID]map[string][]ircdb.ChannelMember
	connector      connectorFunc
}

// NewManager constructs a Manager.
func NewManager(stores *ircdb.MultiStore, h *hub.Hub) *Manager {
	return &Manager{
		stores:         stores,
		hub:            h,
		conn:           map[uuid.UUID]*girc.Client{},
		state:          map[uuid.UUID]string{},
		runtime:        map[uuid.UUID]networkRuntime{},
		membersLoaded:  map[uuid.UUID]map[string]bool{},
		joined:         map[uuid.UUID]map[string]bool{},
		fixtureMembers: map[uuid.UUID]map[string][]ircdb.ChannelMember{},
		connector:      defaultConnector,
	}
}

// SetPreviewEnqueuer installs the URL-preview hook. Must be called before
// Start so handlers pick it up when they're constructed.
func (m *Manager) SetPreviewEnqueuer(p PreviewEnqueuer) {
	m.previews = p
}

// Start upserts and starts all configured networks.
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
		if err := ircdb.SetNetworkConnectCommands(ctx, m.stores.Control, nrow.ID, nc.ConnectCommands); err != nil {
			return err
		}
		if err := m.StartNetwork(ctx, nrow.ID, nc); err != nil {
			return err
		}
	}
	return nil
}

// StartNetwork starts managing one network runtime.
func (m *Manager) StartNetwork(parent context.Context, networkID uuid.UUID, nc NetworkConfig) error {
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

// StopNetwork stops a running network runtime.
func (m *Manager) StopNetwork(networkID uuid.UUID) error {
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

// Wait blocks until all network runtimes exit.
func (m *Manager) Wait() { m.wg.Wait() }

// ErrNotConnected indicates that a network has no active IRC connection.
var ErrNotConnected = errors.New("irc: network not connected")

// Send sends a PRIVMSG to a target on a connected network.
func (m *Manager) Send(networkID uuid.UUID, target, content string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Message(target, content)
	return nil
}

// LogOutbound persists a user-sent message to the per-network log and
// publishes it on the hub. Necessary because the IRC servers we talk to
// don't honor the echo-message capability, so outbound PRIVMSGs would
// otherwise be missing from history and from other connected clients.
func (m *Manager) LogOutbound(ctx context.Context, networkID uuid.UUID, target, kind, content string) error {
	if m.stores == nil {
		return errors.New("irc: stores unavailable")
	}
	bufKind := ircdb.BufferQuery
	if girc.IsValidChannel(target) {
		bufKind = ircdb.BufferChannel
	}
	bufID, _, _, err := m.stores.EnsureBuffer(ctx, networkID, target, bufKind)
	if err != nil {
		return err
	}
	logStore, err := m.stores.LogStore(networkID)
	if err != nil {
		return err
	}
	nick := m.Nick(networkID)
	if nick == "" {
		return errors.New("irc: cannot log outbound message without known nick")
	}
	id, ts, inserted, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID:  bufID,
		Timestamp: time.Now(),
		Sender:    nick,
		Kind:      kind,
		Content:   content,
	})
	if err != nil {
		return err
	}
	if !inserted || m.hub == nil {
		return nil
	}
	m.hub.Publish(&MessageEvent{
		Type:      "message",
		ID:        id,
		NetworkID: networkID,
		BufferID:  bufID,
		TS:        ts,
		Sender:    nick,
		Kind:      kind,
		Content:   content,
	})
	if m.previews != nil && (kind == "privmsg" || kind == "notice" || kind == "action") {
		m.previews.Enqueue(networkID, bufID, id, content)
	}
	return nil
}

// Join sends a JOIN command on a connected network.
func (m *Manager) Join(networkID uuid.UUID, channel string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Join(channel)
	return nil
}

// Part sends a PART command on a connected network.
func (m *Manager) Part(networkID uuid.UUID, channel, reason string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Part(channel, reason)
	return nil
}

// ChangeNick sends a NICK command on a connected network.
func (m *Manager) ChangeNick(networkID uuid.UUID, nick string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Nick(nick)
	return nil
}

// Me sends a CTCP ACTION to a target on a connected network.
func (m *Manager) Me(networkID uuid.UUID, target, message string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Action(target, message)
	return nil
}

// Topic sets the topic for a channel on a connected network.
func (m *Manager) Topic(networkID uuid.UUID, channel, topic string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Topic(channel, topic)
	return nil
}

// Whois sends a WHOIS request on a connected network.
func (m *Manager) Whois(networkID uuid.UUID, nick string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Whois(nick)
	return nil
}

// Invite sends an INVITE command on a connected network.
func (m *Manager) Invite(networkID uuid.UUID, nick, channel string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Invite(channel, nick)
	return nil
}

// Kick sends a KICK command on a connected network.
func (m *Manager) Kick(networkID uuid.UUID, channel, nick, reason string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Kick(channel, nick, reason)
	return nil
}

// Mode sends a MODE command on a connected network.
func (m *Manager) Mode(networkID uuid.UUID, target, modes string, params ...string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Mode(target, modes, params...)
	return nil
}

// Away sets the away status on a connected network.
func (m *Manager) Away(networkID uuid.UUID, message string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Away(message)
	return nil
}

// Back clears the away status on a connected network.
func (m *Manager) Back(networkID uuid.UUID) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Back()
	return nil
}

// Quit sends QUIT with an optional message and stops the network runtime.
func (m *Manager) Quit(networkID uuid.UUID, message string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	if message != "" {
		_ = c.Cmd.SendRaw("QUIT :" + message)
	}
	return m.StopNetwork(networkID)
}

// Rejoin parts then rejoins a channel on a connected network.
func (m *Manager) Rejoin(networkID uuid.UUID, channel string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.PartMessage(channel, "")
	c.Cmd.Join(channel)
	return nil
}

// Notice sends a NOTICE to a target on a connected network.
func (m *Manager) Notice(networkID uuid.UUID, target, content string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.Notice(target, content)
	return nil
}

// CTCP sends a CTCP request to a nick on a connected network.
func (m *Manager) CTCP(networkID uuid.UUID, nick, command, args string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	c.Cmd.SendCTCP(nick, command, args)
	return nil
}

// ListChannels sends a LIST command, optionally filtered by a channel name prefix.
func (m *Manager) ListChannels(networkID uuid.UUID, filter string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	if filter != "" {
		c.Cmd.List(filter)
	} else {
		c.Cmd.List()
	}
	return nil
}

// Raw sends a raw IRC line on a connected network.
func (m *Manager) Raw(networkID uuid.UUID, line string) error {
	m.mu.Lock()
	c := m.conn[networkID]
	m.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return ErrNotConnected
	}
	return c.Cmd.SendRaw(line)
}

// Nick returns the current nickname for a network. Falls back to the
// configured nick when the connection isn't active.
func (m *Manager) Nick(networkID uuid.UUID) string {
	m.mu.Lock()
	c := m.conn[networkID]
	rt, ok := m.runtime[networkID]
	m.mu.Unlock()
	if c != nil && c.IsConnected() {
		if n := c.GetNick(); n != "" {
			return n
		}
	}
	if ok {
		return rt.cfg.Nick
	}
	return ""
}

// StateSnapshot returns a copy of current network states.
func (m *Manager) StateSnapshot() map[uuid.UUID]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[uuid.UUID]string, len(m.state))
	for id, state := range m.state {
		out[id] = state
	}
	return out
}

// ChannelMembers returns the currently tracked members for a joined channel.
func (m *Manager) ChannelMembers(networkID uuid.UUID, channel string) []ircdb.ChannelMember {
	m.mu.Lock()
	c := m.conn[networkID]
	loaded := m.membersLoaded[networkID][channel]
	fixtureMembers := slices.Clone(m.fixtureMembers[networkID][channel])
	m.mu.Unlock()
	if channel == "" {
		return nil
	}
	if c == nil || !c.IsConnected() {
		return fixtureMembers
	}
	members := buildChannelMembers(c, channel)
	if members == nil {
		if loaded {
			return []ircdb.ChannelMember{}
		}
		return nil
	}
	out := make([]ircdb.ChannelMember, 0, len(members))
	for _, member := range members {
		out = append(out, ircdb.ChannelMember{
			Nick:   member.Nick,
			Prefix: member.Prefix,
			Away:   member.Away,
			Self:   member.Self,
		})
	}
	return out
}

// IsJoined returns whether a JOIN has been seen for the given channel on
// this connection. Resets to false at process restart and on disconnect.
func (m *Manager) IsJoined(networkID uuid.UUID, channel string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	channels := m.joined[networkID]
	if channels == nil {
		return false
	}
	return channels[channel]
}

// setJoined records the joined state for a channel under the given network.
func (m *Manager) setJoined(networkID uuid.UUID, channel string, joined bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	channels := m.joined[networkID]
	if channels == nil {
		if !joined {
			return
		}
		channels = map[string]bool{}
		m.joined[networkID] = channels
	}
	if joined {
		channels[channel] = true
	} else {
		delete(channels, channel)
		if len(channels) == 0 {
			delete(m.joined, networkID)
		}
	}
}

// drainJoined removes and returns all channels currently flagged joined for
// a network. Used on disconnect to fan out parted events.
func (m *Manager) drainJoined(networkID uuid.UUID) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	channels := m.joined[networkID]
	if len(channels) == 0 {
		return nil
	}
	out := make([]string, 0, len(channels))
	for ch := range channels {
		out = append(out, ch)
	}
	delete(m.joined, networkID)
	return out
}

func (m *Manager) clearChannelMembersLoaded(networkID uuid.UUID, channel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	channels := m.membersLoaded[networkID]
	if channels == nil {
		return
	}
	delete(channels, channel)
	if len(channels) == 0 {
		delete(m.membersLoaded, networkID)
	}
}

func (m *Manager) runNetwork(ctx context.Context, networkID uuid.UUID, nc NetworkConfig) {
	log := slog.With("network", nc.Name, "network_id", networkID)
	backoff := time.Second
	const maxBackoff = 5 * time.Minute
	serverIndex := 0

	for {
		if ctx.Err() != nil {
			return
		}
		server := nc.serverAt(serverIndex)
		if commands, err := ircdb.ListNetworkConnectCommands(ctx, m.stores.Control, networkID); err != nil {
			log.Warn("load connect commands", "err", err)
		} else {
			nc.ConnectCommands = commands
		}
		client := m.buildClient(ctx, networkID, nc, server)

		m.mu.Lock()
		m.conn[networkID] = client
		m.state[networkID] = StateConnecting.String()
		m.mu.Unlock()
		if m.hub != nil {
			m.hub.Publish(&NetworkStateEvent{Type: "network_state", NetworkID: networkID, State: StateConnecting.String()})
		}

		log.Info("connecting", "host", server.Host, "port", server.Port, "tls", server.TLS)
		err := m.connector(ctx, client, server)

		m.mu.Lock()
		delete(m.conn, networkID)
		delete(m.membersLoaded, networkID)
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

func defaultConnector(_ context.Context, client *girc.Client, _ ServerConfig) error {
	return client.Connect()
}

func (m *Manager) buildClient(ctx context.Context, networkID uuid.UUID, nc NetworkConfig, server ServerConfig) *girc.Client {
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
		Version:     "lurker",
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
	h := &handler{stores: m.stores, db: logStore.DB, hub: m.hub, previews: m.previews, networkID: networkID, networkName: nc.Name, autojoin: nc.Channels, connectCommands: nc.ConnectCommands, connectedHook: func(currentNick string) {
		m.mu.Lock()
		m.state[networkID] = StateConnected.String()
		if _, ok := m.membersLoaded[networkID]; !ok {
			m.membersLoaded[networkID] = map[string]bool{}
		}
		if currentNick != "" {
			if rt, ok := m.runtime[networkID]; ok {
				rt.cfg.Nick = currentNick
				m.runtime[networkID] = rt
			}
		}
		m.mu.Unlock()
		if currentNick != "" {
			tCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := ircdb.UpdateNetwork(tCtx, m.stores.Control, networkID, ircdb.Network{Nick: currentNick}); err != nil {
				slog.Warn("persist current nick", "network_id", networkID, "nick", currentNick, "err", err)
			}
		}
	}, memberListHook: func(channel string) {
		m.mu.Lock()
		if _, ok := m.membersLoaded[networkID]; !ok {
			m.membersLoaded[networkID] = map[string]bool{}
		}
		m.membersLoaded[networkID][channel] = true
		m.mu.Unlock()
	}, clearMemberListHook: func(channel string) {
		m.clearChannelMembersLoaded(networkID, channel)
	}, setJoinedHook: func(channel string, joined bool) {
		m.setJoined(networkID, channel, joined)
	}, drainJoinedHook: func() []string {
		return m.drainJoined(networkID)
	}}
	h.register(client)

	go func() {
		<-ctx.Done()
		client.Close()
	}()

	return client
}
