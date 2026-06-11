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
	if err := s.validateConfig(); err != nil {
		return err
	}

	loginCtx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := s.client.Login(loginCtx); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	handle := s.client.Handle()

	nrow, err := deps.Stores.UpsertNetwork(parent, ircdb.Network{
		Name: s.cfg.Network,
		Kind: ircdb.NetworkKindBluesky,
		Host: pdsHost(s.client.PDS()),
		Port: 0,
		TLS:  false,
		Nick: handle,
	})
	if err != nil {
		return fmt.Errorf("upsert network: %w", err)
	}
	s.networkID = nrow.ID

	resolved, err := s.resolveChannels(parent, deps, nrow, handle)
	if err != nil {
		return err
	}

	statusID, err := ircdb.EnsureStatusBuffer(parent, deps.Stores, nrow.ID)
	if err != nil {
		return fmt.Errorf("ensure status buffer: %w", err)
	}
	s.statusBufferID = statusID

	irc.PublishNetworkState(deps.Hub, nrow.ID, irc.StateConnected)
	slog.Info("data source connected", "source", "bluesky", "network", s.cfg.Network, "handle", handle)

	for _, rc := range resolved {
		s.wg.Go(func() {
			s.runChannel(parent, deps, rc)
		})
	}
	return nil
}

func (s *Source) validateConfig() error {
	if s.cfg.Network == "" {
		return errors.New("bluesky: empty Network")
	}
	if s.cfg.Identifier == "" || s.cfg.AppPassword == "" {
		return errors.New("bluesky: missing credentials")
	}
	if len(s.cfg.Channels) == 0 {
		return errors.New("bluesky: no channels configured")
	}
	return nil
}

// resolveChannels assigns names and intervals to configured channels and
// ensures a buffer exists for each. Channel names default to "<handle>-feed"
// for the timeline kind so the UI labels the buffer with the authenticated
// user.
func (s *Source) resolveChannels(parent context.Context, deps datasource.Deps, nrow ircdb.Network, handle string) ([]runtimeChannel, error) {
	resolved := make([]runtimeChannel, 0, len(s.cfg.Channels))
	for _, ch := range s.cfg.Channels {
		if ch.Name == "" && ch.Kind == ChannelTimeline {
			ch.Name = sanitiseHandle(handle) + "-feed"
		}
		if ch.Name == "" {
			return nil, fmt.Errorf("bluesky: channel kind %q requires a name", ch.Kind)
		}
		if ch.Interval == 0 {
			ch.Interval = defaultInterval(ch.Kind)
		}
		bufID, created, buf, berr := deps.Stores.EnsureBuffer(parent, nrow.ID, ch.Name, ircdb.BufferChannel)
		if berr != nil {
			return nil, fmt.Errorf("ensure buffer %q: %w", ch.Name, berr)
		}
		if created {
			irc.PublishBufferCreated(deps.Hub, buf)
		}
		resolved = append(resolved, runtimeChannel{cfg: ch, bufferID: bufID})
	}
	return resolved, nil
}

func (s *Source) runChannel(ctx context.Context, deps datasource.Deps, rc runtimeChannel) {
	lru := newURILRU(500)
	parents := newParentCache(500)

	// Initial fetch is immediate; subsequent fetches run on the channel
	// interval. backoff doubles up to ~10 min on repeated failure.
	backoff := time.Second
	for {
		if err := s.pollOnce(ctx, deps, rc, lru, parents); err != nil {
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

func (s *Source) pollOnce(ctx context.Context, deps datasource.Deps, rc runtimeChannel, lru *uriLRU, parents *parentCache) error {
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

	// Keep only items we will actually ingest, so we resolve reply parents
	// for fresh posts only.
	fresh := make([]FeedItem, 0, len(page.Feed))
	for _, item := range page.Feed {
		uri := item.Post.URI
		if uri == "" || lru.seen(uri) {
			continue
		}
		fresh = append(fresh, item)
	}
	if len(fresh) == 0 {
		return nil
	}

	s.resolveParents(ctx, fresh, parents)

	for _, item := range fresh {
		post := mapFeedItem(item, parents.get)
		if _, _, err := datasource.IngestPost(ctx, deps, s.networkID, rc.bufferID, post); err != nil {
			slog.Warn("bluesky ingest failed", "uri", item.Post.URI, "err", err)
			continue
		}
		lru.add(item.Post.URI)
	}
	return nil
}

// resolveParents fills the parent cache for every reply in items. It prefers
// the parent PostView the timeline already embeds (free), and batch-hydrates
// the rest via getPosts. Resolution is best-effort: on fetch failure the
// affected replies fall back to rendering the raw parent URI.
func (s *Source) resolveParents(ctx context.Context, items []FeedItem, parents *parentCache) {
	var need []string
	queued := map[string]bool{}
	for _, item := range items {
		uri := parentURI(item)
		if uri == "" {
			continue
		}
		if _, ok := parents.get(uri); ok {
			continue
		}
		if ref, ok := inlineParent(item, uri); ok {
			parents.put(uri, ref)
			continue
		}
		if !queued[uri] {
			queued[uri] = true
			need = append(need, uri)
		}
	}
	for start := 0; start < len(need); start += getPostsMax {
		end := min(start+getPostsMax, len(need))
		posts, err := s.client.GetPosts(ctx, need[start:end])
		if err != nil {
			slog.Warn("bluesky parent fetch failed", "network", s.cfg.Network, "err", err)
			return
		}
		for _, p := range posts {
			parents.put(p.URI, parentRef{name: personLabel(p.Author), text: p.Record.Text})
		}
	}
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

// parentSnippetLen caps the inlined parent-post text so a reply stays
// glanceable in the buffer.
const parentSnippetLen = 100

// parentRef is the minimal parent-post context rendered inline on a reply or
// quote. name is the display name, falling back to the handle.
type parentRef struct {
	name string
	text string
}

// personLabel prefers an actor's display name and falls back to the stable
// handle, so the timeline reads with human names rather than raw handles.
func personLabel(a Actor) string {
	if n := strings.TrimSpace(a.DisplayName); n != "" {
		return n
	}
	return a.Handle
}

// oneLine collapses runs of whitespace (including newlines) to single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// mapFeedItem converts a Bluesky feed item to a datasource.Post. resolveParent
// returns the cached parent context for a reply's parent URI; pass nil to fall
// back to rendering the raw URI.
func mapFeedItem(item FeedItem, resolveParent func(uri string) (parentRef, bool)) datasource.Post {
	p := item.Post
	content := p.Record.Text

	if parts := renderEmbed(p.Embed); len(parts) > 0 {
		if content != "" {
			content += " "
		}
		content += strings.Join(parts, " ")
	}

	sender := personLabel(p.Author)
	account := p.Author.DID

	if item.Reason != nil && strings.HasSuffix(item.Reason.Type, "#reasonRepost") {
		if by := personLabel(item.Reason.By); by != "" {
			content = "[RT by " + by + "] " + content
		}
	}

	if uri := parentURI(item); uri != "" {
		content += " " + renderParent(uri, resolveParent)
	}

	if q, ok := quotedPost(p.Embed); ok {
		if r := renderQuote(q); r != "" {
			content += " " + r
		}
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

// parentURI returns the at:// URI of the post this item replies to, or "".
func parentURI(item FeedItem) string {
	r := item.Post.Record.Reply
	if r == nil || r.Parent == nil {
		return ""
	}
	return r.Parent.URI
}

// inlineParent extracts the parent context from the timeline's embedded reply
// PostView when present and hydrated. notFound/blocked parents (only URI set)
// and URI mismatches return false so the caller falls back to getPosts.
func inlineParent(item FeedItem, uri string) (parentRef, bool) {
	if item.Reply == nil || item.Reply.Parent == nil {
		return parentRef{}, false
	}
	pv := item.Reply.Parent
	if pv.URI != uri {
		return parentRef{}, false
	}
	if pv.Author.Handle == "" && pv.Record.Text == "" {
		return parentRef{}, false
	}
	return parentRef{name: personLabel(pv.Author), text: pv.Record.Text}, true
}

// renderParent formats the inline reply reference. With a resolved parent it
// produces e.g. `(re: Alice: "the quoted text…")`; otherwise it falls back to
// the raw URI so context is never silently dropped.
func renderParent(uri string, resolve func(uri string) (parentRef, bool)) string {
	if resolve != nil {
		if ref, ok := resolve(uri); ok {
			snippet := truncateSnippet(ref.text, parentSnippetLen)
			switch {
			case ref.name != "" && snippet != "":
				return "(re: " + ref.name + ": \"" + snippet + "\")"
			case ref.name != "":
				return "(re: " + ref.name + ")"
			case snippet != "":
				return "(re: \"" + snippet + "\")"
			}
		}
	}
	return "(re: " + uri + ")"
}

// renderQuote formats the inline quote-post reference, e.g.
// `(quoting Alice: "the quoted text…")`. Returns "" when there is nothing to
// show.
func renderQuote(q parentRef) string {
	snippet := truncateSnippet(q.text, parentSnippetLen)
	switch {
	case q.name != "" && snippet != "":
		return "(quoting " + q.name + ": \"" + snippet + "\")"
	case q.name != "":
		return "(quoting " + q.name + ")"
	case snippet != "":
		return "(quoting \"" + snippet + "\")"
	default:
		return ""
	}
}

// quotedPost extracts the quoted post (handle + text) from an embed, or false
// when the embed is not a quote or the quoted record is unhydrated. The quoted
// post is always carried inline by the timeline, so no fetch is needed. Handles
// both app.bsky.embed.record#view (quote directly under Record) and the record
// half of recordWithMedia#view (quote nested one level deeper).
func quotedPost(e *Embed) (parentRef, bool) {
	if e == nil {
		return parentRef{}, false
	}
	// The loop bound guards against a malformed/cyclic nesting; in practice
	// recordWithMedia nests at most one level.
	for rec, i := e.Record, 0; rec != nil && i < 4; rec, i = rec.Record, i+1 {
		text := ""
		if rec.Value != nil {
			text = rec.Value.Text
		}
		if rec.Author.Handle != "" || text != "" {
			return parentRef{name: personLabel(rec.Author), text: text}, true
		}
	}
	return parentRef{}, false
}

// truncateSnippet collapses whitespace and trims s to at most limit runes,
// appending an ellipsis when it cuts.
func truncateSnippet(s string, limit int) string {
	s = oneLine(s)
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimRight(string(r[:limit]), " ") + "…"
}

// renderEmbed produces the tokens appended to a post's text for its embed:
// media URLs (so the preview pipeline can attach cards/images) interleaved
// with the human context ATProto already ships for free — external card
// titles, image alt text, and video markers. It recurses into Media for
// recordWithMedia embeds; the quoted record is handled separately by
// quotedPost.
func renderEmbed(e *Embed) []string {
	if e == nil {
		return nil
	}
	var out []string

	if e.External != nil && e.External.URI != "" {
		out = append(out, e.External.URI)
		if t := oneLine(e.External.Title); t != "" {
			out = append(out, "\""+t+"\"")
		}
	}

	for _, img := range e.Images {
		switch {
		case img.Fullsize != "":
			out = append(out, img.Fullsize)
		case img.Thumb != "":
			out = append(out, img.Thumb)
		}
		if alt := oneLine(img.Alt); alt != "" {
			out = append(out, "[image: "+alt+"]")
		}
	}

	if e.Thumbnail != "" || e.Playlist != "" {
		if e.Thumbnail != "" {
			out = append(out, e.Thumbnail)
		}
		if alt := oneLine(e.Alt); alt != "" {
			out = append(out, "[video: "+alt+"]")
		} else {
			out = append(out, "[video]")
		}
	}

	if e.Media != nil {
		out = append(out, renderEmbed(e.Media)...)
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

// parentCache is a bounded FIFO map from parent URI to its resolved context.
// It lets a reply's parent be hydrated once and reused across polls and across
// sibling replies in the same batch. Owned by a single channel goroutine — not
// safe for concurrent use.
type parentCache struct {
	cap   int
	order *list.List
	index map[string]*list.Element
}

type parentEntry struct {
	uri string
	ref parentRef
}

func newParentCache(capacity int) *parentCache {
	if capacity <= 0 {
		capacity = 500
	}
	return &parentCache{cap: capacity, order: list.New(), index: map[string]*list.Element{}}
}

func (c *parentCache) get(uri string) (parentRef, bool) {
	el, ok := c.index[uri]
	if !ok {
		return parentRef{}, false
	}
	return el.Value.(*parentEntry).ref, true
}

func (c *parentCache) put(uri string, ref parentRef) {
	if el, ok := c.index[uri]; ok {
		el.Value.(*parentEntry).ref = ref
		return
	}
	el := c.order.PushBack(&parentEntry{uri: uri, ref: ref})
	c.index[uri] = el
	for c.order.Len() > c.cap {
		oldest := c.order.Front()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.index, oldest.Value.(*parentEntry).uri)
	}
}
