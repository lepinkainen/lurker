package preview

import (
	"context"
	"log/slog"
	"sync"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

// Config is the runtime configuration for the preview service.
type Config struct {
	Enabled       bool
	MaxBytes      int64
	Timeout       time.Duration
	CacheTTL      time.Duration
	Workers       int
	UserAgent     string
	QueueCapacity int
}

// DefaultConfig returns the sensible defaults baked into config.go.
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		MaxBytes:      512 * 1024,
		Timeout:       5 * time.Second,
		CacheTTL:      7 * 24 * time.Hour,
		Workers:       4,
		QueueCapacity: 256,
	}
}

// job is a single enqueue request: a message's URLs need previews.
type job struct {
	NetworkID int64
	BufferID  int64
	MessageID int64
	Content   string
}

// Event is published over the hub once previews are ready for a
// message. Only displayable previews are included.
type Event struct {
	Type      string            `json:"type"`
	MessageID int64             `json:"message_id"`
	NetworkID int64             `json:"network_id"`
	BufferID  int64             `json:"buffer_id"`
	Previews  []ResolvedPreview `json:"previews"`
}

// ResolvedPreview is the wire form of a single preview.
type ResolvedPreview struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	Title       string `json:"title,omitzero"`
	Description string `json:"description,omitzero"`
	ImageURL    string `json:"image_url,omitzero"`
	SiteName    string `json:"site_name,omitzero"`
	Width       int    `json:"width,omitzero"`
	Height      int    `json:"height,omitzero"`
	Mime        string `json:"mime,omitzero"`
}

// ToResolvedPreview converts a cached URLPreview into wire form.
func ToResolvedPreview(p ircdb.URLPreview) ResolvedPreview {
	return ResolvedPreview{
		URL:         p.URL,
		Kind:        p.Kind,
		Title:       p.Title,
		Description: p.Description,
		ImageURL:    p.ImageURL,
		SiteName:    p.SiteName,
		Width:       p.Width,
		Height:      p.Height,
		Mime:        p.Mime,
	}
}

// Service runs URL-preview fetches asynchronously and writes results into
// the shared preview store plus per-network link tables.
type Service struct {
	cfg     Config
	stores  *ircdb.MultiStore
	hub     *hub.Hub
	fetcher *Fetcher
	queue   chan job

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewService constructs a preview Service. The caller must call Start before
// enqueueing and Close on shutdown. If cfg.Enabled is false, Enqueue is a
// no-op and no workers run.
func NewService(cfg Config, stores *ircdb.MultiStore, h *hub.Hub) *Service {
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 256
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	svc := &Service{
		cfg:    cfg,
		stores: stores,
		hub:    h,
		queue:  make(chan job, cfg.QueueCapacity),
	}
	svc.fetcher = NewFetcher(FetcherConfig{
		UserAgent: cfg.UserAgent,
		Timeout:   cfg.Timeout,
		MaxBytes:  cfg.MaxBytes,
	})
	return svc
}

// Start launches the worker pool. Safe to call once.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if !s.cfg.Enabled {
		slog.Info("preview disabled by config")
		return
	}
	if s.stores == nil || s.stores.Previews == nil {
		slog.Warn("preview disabled: no preview store available")
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	for range s.cfg.Workers {
		s.wg.Go(func() { s.run(runCtx) })
	}
	slog.Info("preview workers started", "workers", s.cfg.Workers, "ttl", s.cfg.CacheTTL, "max_bytes", s.cfg.MaxBytes)
}

// Close stops workers and waits for them to drain.
func (s *Service) Close() error {
	if s == nil || s.cancel == nil {
		return nil
	}
	s.cancel()
	s.wg.Wait()
	return nil
}

// Enqueue submits a message for URL-preview resolution. Non-blocking: if the
// queue is full, the request is dropped with a warning. Matches the
// irc.PreviewEnqueuer interface.
func (s *Service) Enqueue(networkID, bufferID, messageID int64, content string) {
	if s == nil || !s.cfg.Enabled || content == "" {
		return
	}
	j := job{NetworkID: networkID, BufferID: bufferID, MessageID: messageID, Content: content}
	select {
	case s.queue <- j:
	default:
		slog.Warn("preview queue full; dropping", "message_id", messageID, "network_id", networkID)
	}
}

func (s *Service) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-s.queue:
			if !ok {
				return
			}
			s.process(ctx, ev)
		}
	}
}

func (s *Service) process(ctx context.Context, ev job) {
	urls := ExtractURLs(ev.Content)
	if len(urls) == 0 {
		return
	}
	slog.Debug("preview process", "message_id", ev.MessageID, "urls", urls)

	// Per-URL fetch + cache.
	resolved := make([]ircdb.URLPreview, 0, len(urls))
	for _, u := range urls {
		cached, ok, err := s.stores.Previews.Get(ctx, u, s.cfg.CacheTTL)
		if err != nil {
			slog.Warn("preview cache get", "err", err, "url", u)
		}
		if ok {
			resolved = append(resolved, cached)
			continue
		}
		p := s.fetcher.Fetch(ctx, u)
		if err := s.stores.Previews.Put(ctx, p); err != nil {
			slog.Warn("preview cache put", "err", err, "url", u)
		}
		resolved = append(resolved, p)
	}

	// Record (message_id, url) associations in the per-network log DB.
	logStore, err := s.stores.LogStore(ev.NetworkID)
	if err != nil {
		slog.Warn("preview log store", "err", err, "network_id", ev.NetworkID)
		return
	}
	links := make([]ircdb.MessagePreviewLink, 0, len(urls))
	for i, u := range urls {
		links = append(links, ircdb.MessagePreviewLink{MessageID: ev.MessageID, URL: u, Position: i})
	}
	if err := ircdb.InsertMessagePreviewLinks(ctx, logStore.DB, links); err != nil {
		slog.Warn("preview link insert", "err", err, "message_id", ev.MessageID)
	}

	// Publish only displayable previews.
	out := make([]ResolvedPreview, 0, len(resolved))
	for _, p := range resolved {
		if p.Displayable() {
			out = append(out, ToResolvedPreview(p))
		}
	}
	if len(out) == 0 || s.hub == nil {
		return
	}
	s.hub.Publish(&Event{
		Type:      "preview",
		MessageID: ev.MessageID,
		NetworkID: ev.NetworkID,
		BufferID:  ev.BufferID,
		Previews:  out,
	})
}
