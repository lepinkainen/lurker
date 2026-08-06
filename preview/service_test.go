package preview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

type serviceTestEnv struct {
	stores    *ircdb.MultiStore
	networkID uuid.UUID
	bufferID  uuid.UUID
	messageID uuid.UUID
}

func newServiceTestEnv(t *testing.T) serviceTestEnv {
	t.Helper()
	ctx := context.Background()
	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatalf("open multistore: %v", err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	netRow, err := stores.UpsertNetwork(ctx, ircdb.Network{
		Name: "testnet",
		Host: "irc.test",
		Port: 6697,
		TLS:  true,
		Nick: "tester",
	})
	if err != nil {
		t.Fatalf("upsert network: %v", err)
	}
	bufferID, _, _, err := stores.EnsureBuffer(ctx, netRow.ID, "#test", ircdb.BufferChannel)
	if err != nil {
		t.Fatalf("ensure buffer: %v", err)
	}
	logStore, err := stores.LogStore(netRow.ID)
	if err != nil {
		t.Fatalf("log store: %v", err)
	}
	messageID, _, _, err := ircdb.InsertLogMessage(ctx, logStore.DB, ircdb.LogMessageInput{
		BufferID:  bufferID,
		Timestamp: time.Now(),
		Sender:    "alice",
		Kind:      "privmsg",
		Target:    "#test",
		Content:   "message with preview",
		Raw:       ":alice PRIVMSG #test :message with preview",
	})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return serviceTestEnv{stores: stores, networkID: netRow.ID, bufferID: bufferID, messageID: messageID}
}

func recvPreviewEvent(t *testing.T, ch <-chan any) *Event {
	t.Helper()
	select {
	case v := <-ch:
		ev, ok := v.(*Event)
		if !ok {
			t.Fatalf("got event type %T, want *preview.Event", v)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for preview event")
		return nil
	}
}

func assertNoEvent(t *testing.T, ch <-chan any) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected event: %#v", v)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNewServiceNormalizesConfig(t *testing.T) {
	svc := NewService(Config{Enabled: true, Workers: -1}, nil, nil)
	if svc.cfg.Workers != 4 {
		t.Fatalf("Workers = %d, want 4", svc.cfg.Workers)
	}
	if cap(svc.queue) != 256 {
		t.Fatalf("queue capacity = %d, want 256", cap(svc.queue))
	}
}

func TestServiceEnqueueNoOps(t *testing.T) {
	var nilSvc *Service
	nilSvc.Enqueue(uuid.New(), uuid.New(), uuid.New(), "https://example.test")

	disabled := NewService(Config{Enabled: false, Workers: 1}, nil, nil)
	disabled.Enqueue(uuid.New(), uuid.New(), uuid.New(), "https://example.test")
	if got := len(disabled.queue); got != 0 {
		t.Fatalf("disabled service queue length = %d, want 0", got)
	}

	enabled := NewService(Config{Enabled: true, Workers: 1}, nil, nil)
	enabled.Enqueue(uuid.New(), uuid.New(), uuid.New(), "")
	if got := len(enabled.queue); got != 0 {
		t.Fatalf("empty content queue length = %d, want 0", got)
	}
}

func TestServiceEnqueueDropsWhenQueueFull(t *testing.T) {
	svc := NewService(Config{Enabled: true, Workers: 1}, nil, nil)
	svc.queue = make(chan job, 1)
	svc.Enqueue(uuid.New(), uuid.New(), uuid.New(), "https://one.example.test")
	if got := len(svc.queue); got != 1 {
		t.Fatalf("queue length after first enqueue = %d, want 1", got)
	}

	done := make(chan struct{})
	go func() {
		svc.Enqueue(uuid.New(), uuid.New(), uuid.New(), "https://two.example.test")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Enqueue blocked when queue was full")
	}
	if got := len(svc.queue); got != 1 {
		t.Fatalf("queue length after dropped enqueue = %d, want 1", got)
	}
}

func TestServiceWorkerProcessesCacheMissEndToEnd(t *testing.T) {
	env := newServiceTestEnv(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><meta property="og:title" content="Test Title"><meta property="og:description" content="Test Description"><meta property="og:site_name" content="Test Site"></head><body></body></html>`))
	}))
	defer server.Close()

	h := hub.New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()
	svc := NewService(Config{Enabled: true, Workers: 1, CacheTTL: time.Hour, Timeout: time.Second}, env.stores, h)
	svc.fetcher = NewFetcher(FetcherConfig{Timeout: time.Second, SSRFCheck: func(context.Context, string) error { return nil }})
	svc.Start(context.Background())
	defer func() { _ = svc.Close() }()

	svc.Enqueue(env.networkID, env.bufferID, env.messageID, "look "+server.URL+" now")
	ev := recvPreviewEvent(t, ch)
	if ev.Type != "preview" || ev.MessageID != env.messageID || ev.NetworkID != env.networkID || ev.BufferID != env.bufferID {
		t.Fatalf("unexpected event metadata: %+v", ev)
	}
	if len(ev.Previews) != 1 {
		t.Fatalf("event previews length = %d, want 1: %+v", len(ev.Previews), ev.Previews)
	}
	if got := ev.Previews[0]; got.URL != server.URL || got.Kind != ircdb.PreviewKindOpenGraph || got.Title != "Test Title" {
		t.Fatalf("unexpected preview: %+v", got)
	}
	if hits.Load() == 0 {
		t.Fatal("expected test server to be fetched")
	}

	cached, ok, err := env.stores.Previews.Get(context.Background(), server.URL, time.Hour)
	if err != nil || !ok {
		t.Fatalf("preview cache miss after process: ok=%v err=%v", ok, err)
	}
	if cached.Title != "Test Title" {
		t.Fatalf("cached title = %q, want Test Title", cached.Title)
	}
	assertPreviewLinks(t, env, []string{server.URL})
}

func TestServiceProcessUsesCacheHit(t *testing.T) {
	env := newServiceTestEnv(t)
	ctx := context.Background()
	url := "http://cached.example.test/page"
	if err := env.stores.Previews.Put(ctx, ircdb.URLPreview{
		URL:       url,
		Kind:      ircdb.PreviewKindOpenGraph,
		Title:     "Cached Title",
		FetchedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("put cached preview: %v", err)
	}

	h := hub.New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()
	svc := NewService(Config{Enabled: true, CacheTTL: time.Hour}, env.stores, h)
	var fetcherCalled atomic.Bool
	svc.fetcher = NewFetcher(FetcherConfig{SSRFCheck: func(context.Context, string) error {
		fetcherCalled.Store(true)
		return nil
	}})

	svc.process(ctx, job{NetworkID: env.networkID, BufferID: env.bufferID, MessageID: env.messageID, Content: url})
	if fetcherCalled.Load() {
		t.Fatal("fetcher called for cache hit")
	}
	ev := recvPreviewEvent(t, ch)
	if len(ev.Previews) != 1 || ev.Previews[0].Title != "Cached Title" {
		t.Fatalf("unexpected cached event previews: %+v", ev.Previews)
	}
	assertPreviewLinks(t, env, []string{url})
}

func TestServiceProcessFiltersNonDisplayablePreviews(t *testing.T) {
	env := newServiceTestEnv(t)
	ctx := context.Background()
	urls := []string{"http://cached.example.test/none", "http://cached.example.test/error"}
	previews := []ircdb.URLPreview{
		{URL: urls[0], Kind: ircdb.PreviewKindNone, FetchedAt: time.Now().UTC()},
		{URL: urls[1], Kind: ircdb.PreviewKindError, Error: "boom", FetchedAt: time.Now().UTC()},
	}
	for _, p := range previews {
		if err := env.stores.Previews.Put(ctx, p); err != nil {
			t.Fatalf("put preview %s: %v", p.URL, err)
		}
	}

	h := hub.New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()
	svc := NewService(Config{Enabled: true, CacheTTL: time.Hour}, env.stores, h)
	svc.process(ctx, job{NetworkID: env.networkID, BufferID: env.bufferID, MessageID: env.messageID, Content: urls[0] + " " + urls[1]})

	assertNoEvent(t, ch)
	assertPreviewLinks(t, env, urls)
}

func TestServiceProcessPublishesOnlyDisplayablePreviews(t *testing.T) {
	env := newServiceTestEnv(t)
	ctx := context.Background()
	displayableURL := "http://cached.example.test/image.png"
	noneURL := "http://cached.example.test/none"
	for _, p := range []ircdb.URLPreview{
		{URL: displayableURL, Kind: ircdb.PreviewKindImage, Mime: "image/png", FetchedAt: time.Now().UTC()},
		{URL: noneURL, Kind: ircdb.PreviewKindNone, FetchedAt: time.Now().UTC()},
	} {
		if err := env.stores.Previews.Put(ctx, p); err != nil {
			t.Fatalf("put preview %s: %v", p.URL, err)
		}
	}

	h := hub.New()
	ch, _, unsub := h.Subscribe(1)
	defer unsub()
	svc := NewService(Config{Enabled: true, CacheTTL: time.Hour}, env.stores, h)
	svc.process(ctx, job{NetworkID: env.networkID, BufferID: env.bufferID, MessageID: env.messageID, Content: displayableURL + " " + noneURL})

	ev := recvPreviewEvent(t, ch)
	if len(ev.Previews) != 1 {
		t.Fatalf("event previews length = %d, want 1: %+v", len(ev.Previews), ev.Previews)
	}
	if ev.Previews[0].URL != displayableURL || ev.Previews[0].Kind != ircdb.PreviewKindImage {
		t.Fatalf("unexpected displayable preview: %+v", ev.Previews[0])
	}
	assertPreviewLinks(t, env, []string{displayableURL, noneURL})
}

func assertPreviewLinks(t *testing.T, env serviceTestEnv, want []string) {
	t.Helper()
	logStore, err := env.stores.LogStore(env.networkID)
	if err != nil {
		t.Fatalf("log store: %v", err)
	}
	links, err := ircdb.ListMessagePreviewLinks(context.Background(), logStore.DB, []uuid.UUID{env.messageID})
	if err != nil {
		t.Fatalf("list preview links: %v", err)
	}
	if len(links) != len(want) {
		t.Fatalf("link count = %d, want %d: %+v", len(links), len(want), links)
	}
	for i, url := range want {
		if links[i].MessageID != env.messageID || links[i].URL != url || links[i].Position != i {
			t.Fatalf("link[%d] = %+v, want message=%s url=%s position=%d", i, links[i], env.messageID, url, i)
		}
	}
}
