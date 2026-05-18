package bluesky

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lepinkainen/lurker/datasource"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

// Config is one Bluesky account/source configuration.
type Config struct {
	Network     string // network row name (also unique key)
	Identifier  string // handle or DID for createSession
	AppPassword string
	PDS         string // optional; defaults to DefaultPDS
	Channels    []ChannelConfig
}

// Source is one configured Bluesky account. Implements datasource.Source.
//
// networkID and statusBufferID are written in Start before any goroutine
// is launched via wg.Go, and never written again. The goroutine-start
// happens-before edge makes them safe to read from poll goroutines without
// synchronization.
type Source struct {
	cfg    Config
	client *Client

	networkID      uuid.UUID
	statusBufferID uuid.UUID

	wg sync.WaitGroup
}

// runtimeChannel is the resolved runtime state for one ChannelConfig.
type runtimeChannel struct {
	cfg      ChannelConfig
	bufferID uuid.UUID
}

// NewSource constructs a Source from a parsed Config. The source is inert
// until Start is called.
func NewSource(cfg Config) *Source {
	return &Source{
		cfg:    cfg,
		client: NewClient(cfg.PDS, cfg.Identifier, cfg.AppPassword),
	}
}

// Name implements datasource.Source.
func (s *Source) Name() string { return s.cfg.Network }

// Kind implements datasource.Source.
func (s *Source) Kind() string { return ircdb.NetworkKindBluesky }

// NetworkID implements datasource.Source.
func (s *Source) NetworkID() uuid.UUID { return s.networkID }

// Wait implements datasource.Source.
func (s *Source) Wait() { s.wg.Wait() }

// Start logs in, upserts the network row, ensures channel buffers, and
// launches the polling goroutines. The Start error path leaves the source
// in a clean, never-started state.
func (s *Source) Start(parent context.Context, deps datasource.Deps) error {
	if s.cfg.Network == "" {
		return errors.New("bluesky: empty Network")
	}
	if s.cfg.Identifier == "" || s.cfg.AppPassword == "" {
		return errors.New("bluesky: missing credentials")
	}
	if len(s.cfg.Channels) == 0 {
		return errors.New("bluesky: no channels configured")
	}

	loginCtx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := s.client.Login(loginCtx); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	handle := s.client.Handle()

	host := pdsHost(s.client.PDS())
	nrow, err := deps.Stores.UpsertNetwork(parent, ircdb.Network{
		Name: s.cfg.Network,
		Kind: ircdb.NetworkKindBluesky,
		Host: host,
		Port: 0,
		TLS:  false,
		Nick: handle,
	})
	if err != nil {
		return fmt.Errorf("upsert network: %w", err)
	}
	s.networkID = nrow.ID

	// Resolve configured channels to buffer IDs. Channel names default to
	// "<handle>-feed" for the timeline kind so the UI labels the buffer with
	// the authenticated user.
	resolved := make([]runtimeChannel, 0, len(s.cfg.Channels))
	for _, ch := range s.cfg.Channels {
		if ch.Name == "" && ch.Kind == ChannelTimeline {
			ch.Name = sanitiseHandle(handle) + "-feed"
		}
		if ch.Name == "" {
			return fmt.Errorf("bluesky: channel kind %q requires a name", ch.Kind)
		}
		if ch.Interval == 0 {
			ch.Interval = defaultInterval(ch.Kind)
		}
		bufID, created, buf, berr := deps.Stores.EnsureBuffer(parent, nrow.ID, ch.Name, ircdb.BufferChannel)
		if berr != nil {
			return fmt.Errorf("ensure buffer %q: %w", ch.Name, berr)
		}
		if created && deps.Hub != nil {
			deps.Hub.Publish(&irc.BufferCreatedEvent{
				Type:      "buffer_created",
				ID:        buf.ID,
				NetworkID: nrow.ID,
				Name:      buf.Name,
				Kind:      buf.Kind,
				CreatedAt: buf.CreatedAt,
			})
		}
		resolved = append(resolved, runtimeChannel{cfg: ch, bufferID: bufID})
	}

	statusID, err := ircdb.EnsureStatusBuffer(parent, deps.Stores, nrow.ID)
	if err != nil {
		return fmt.Errorf("ensure status buffer: %w", err)
	}
	s.statusBufferID = statusID

	if deps.Hub != nil {
		deps.Hub.Publish(&irc.NetworkStateEvent{
			Type:      "network_state",
			NetworkID: nrow.ID,
			State:     irc.StateConnected.String(),
		})
	}
	slog.Info("data source connected", "source", "bluesky", "network", s.cfg.Network, "handle", handle)

	for _, rc := range resolved {
		s.wg.Go(func() {
			s.runChannel(parent, deps, rc)
		})
	}
	return nil
}

func (s *Source) runChannel(ctx context.Context, deps datasource.Deps, rc runtimeChannel) {
	lru := newURILRU(500)

	// Initial fetch is immediate; subsequent fetches run on the channel
	// interval. backoff doubles up to ~10 min on repeated failure.
	backoff := time.Second
	for {
		if err := s.pollOnce(ctx, deps, rc, lru); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("bluesky poll failed",
				"network", s.cfg.Network, "buffer", rc.cfg.Name, "err", err)
			s.emitStatus(ctx, deps, fmt.Sprintf("bluesky poll failed: %v", err))
			if !sleepCtx(ctx, backoff) {
				return
			}
			if backoff < 10*time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if !sleepCtx(ctx, rc.cfg.Interval) {
			return
		}
	}
}

func (s *Source) pollOnce(ctx context.Context, deps datasource.Deps, rc runtimeChannel, lru *uriLRU) error {
	page, err := s.fetchPage(ctx, rc.cfg)
	if err != nil {
		return err
	}
	if page == nil || len(page.Feed) == 0 {
		return nil
	}
	// Sort by indexedAt ASC so older items insert first and rendering order
	// is stable across polls. page.Feed is a freshly decoded slice, so it is
	// safe to sort in place.
	sort.Slice(page.Feed, func(i, j int) bool {
		return page.Feed[i].Post.IndexedAt < page.Feed[j].Post.IndexedAt
	})
	for _, item := range page.Feed {
		uri := item.Post.URI
		if uri == "" {
			continue
		}
		if lru.seen(uri) {
			continue
		}
		if _, _, err := datasource.IngestPost(ctx, deps, s.networkID, rc.bufferID, mapFeedItem(item)); err != nil {
			slog.Warn("bluesky ingest failed", "uri", uri, "err", err)
			continue
		}
		lru.add(uri)
	}
	return nil
}

func (s *Source) emitStatus(ctx context.Context, deps datasource.Deps, msg string) {
	if s.networkID == uuid.Nil || s.statusBufferID == uuid.Nil {
		return
	}
	_, _, _ = datasource.IngestPost(ctx, deps, s.networkID, s.statusBufferID, datasource.Post{
		Timestamp: time.Now(),
		Sender:    "*",
		Kind:      "notice",
		Content:   msg,
	})
}

// mapFeedItem converts a Bluesky feed item to a datasource.Post.
func mapFeedItem(item FeedItem) datasource.Post {
	p := item.Post
	content := p.Record.Text

	embedURLs := collectEmbedURLs(p.Embed)
	if len(embedURLs) > 0 {
		if content != "" {
			content += " "
		}
		content += strings.Join(embedURLs, " ")
	}

	sender := p.Author.Handle
	account := p.Author.DID

	if item.Reason != nil && strings.HasSuffix(item.Reason.Type, "#reasonRepost") && item.Reason.By.Handle != "" {
		content = "[RT by " + item.Reason.By.Handle + "] " + content
	}

	if p.Record.Reply != nil && p.Record.Reply.Parent != nil && p.Record.Reply.Parent.URI != "" {
		// Best-effort parent reference; thread tree rendering is out of scope.
		content += " (re: " + p.Record.Reply.Parent.URI + ")"
	}

	ts := parseATTimestamp(p.IndexedAt)
	return datasource.Post{
		MsgID:     p.URI,
		Timestamp: ts,
		Sender:    sender,
		Account:   account,
		Kind:      "privmsg",
		Content:   content,
	}
}

func collectEmbedURLs(e *Embed) []string {
	if e == nil {
		return nil
	}
	var out []string
	if e.External != nil && e.External.URI != "" {
		out = append(out, e.External.URI)
	}
	for _, img := range e.Images {
		if img.Fullsize != "" {
			out = append(out, img.Fullsize)
		} else if img.Thumb != "" {
			out = append(out, img.Thumb)
		}
	}
	if e.Media != nil {
		out = append(out, collectEmbedURLs(e.Media)...)
	}
	return out
}

func parseATTimestamp(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	// time.RFC3339 accepts the nanosecond form ATProto emits.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now()
}

func pdsHost(pds string) string {
	if u, err := url.Parse(pds); err == nil && u.Host != "" {
		return u.Host
	}
	return "bsky.social"
}

// sanitiseHandle returns a buffer-safe slug for a Bluesky handle. Buffer
// names must be unique per network; handles already are. We only trim
// whitespace and lower-case to keep the UI label predictable.
func sanitiseHandle(handle string) string {
	h := strings.TrimSpace(strings.ToLower(handle))
	if h == "" {
		return "user"
	}
	return h
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// uriLRU is a tiny FIFO-eviction set keyed by URI. Owned by a single
// channel goroutine — not safe for concurrent use.
type uriLRU struct {
	cap   int
	order *list.List
	index map[string]*list.Element
}

func newURILRU(capacity int) *uriLRU {
	if capacity <= 0 {
		capacity = 500
	}
	return &uriLRU{cap: capacity, order: list.New(), index: map[string]*list.Element{}}
}

func (l *uriLRU) seen(uri string) bool {
	_, ok := l.index[uri]
	return ok
}

func (l *uriLRU) add(uri string) {
	if _, ok := l.index[uri]; ok {
		return
	}
	el := l.order.PushBack(uri)
	l.index[uri] = el
	for l.order.Len() > l.cap {
		oldest := l.order.Front()
		if oldest == nil {
			break
		}
		l.order.Remove(oldest)
		delete(l.index, oldest.Value.(string))
	}
}
