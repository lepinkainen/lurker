package bluesky

import (
	"strings"
	"testing"
)

func TestMapFeedItemBasic(t *testing.T) {
	item := FeedItem{
		Post: PostView{
			URI:       "at://did:plc:abc/app.bsky.feed.post/1",
			Author:    Actor{DID: "did:plc:abc", Handle: "alice.bsky.social"},
			Record:    Record{Text: "hello world"},
			IndexedAt: "2026-05-15T10:00:00Z",
		},
	}
	post := mapFeedItem(item, nil)
	if post.MsgID != item.Post.URI {
		t.Fatalf("msgid = %q", post.MsgID)
	}
	if post.Sender != "alice.bsky.social" {
		t.Fatalf("sender = %q", post.Sender)
	}
	if post.Account != "did:plc:abc" {
		t.Fatalf("account = %q", post.Account)
	}
	if post.Content != "hello world" {
		t.Fatalf("content = %q", post.Content)
	}
	if post.Kind != "privmsg" {
		t.Fatalf("kind = %q", post.Kind)
	}
}

func TestMapFeedItemRepost(t *testing.T) {
	item := FeedItem{
		Post: PostView{
			URI:    "at://did:plc:abc/app.bsky.feed.post/1",
			Author: Actor{Handle: "alice.bsky.social"},
			Record: Record{Text: "original"},
		},
		Reason: &FeedReason{
			Type: "app.bsky.feed.defs#reasonRepost",
			By:   Actor{Handle: "bob.bsky.social"},
		},
	}
	post := mapFeedItem(item, nil)
	if !strings.HasPrefix(post.Content, "[RT by bob.bsky.social]") {
		t.Fatalf("repost prefix missing: %q", post.Content)
	}
	if post.Sender != "alice.bsky.social" {
		t.Fatalf("sender should be original author, got %q", post.Sender)
	}
}

func TestMapFeedItemExternalEmbed(t *testing.T) {
	item := FeedItem{
		Post: PostView{
			URI:    "at://did:plc:abc/app.bsky.feed.post/2",
			Author: Actor{Handle: "alice.bsky.social"},
			Record: Record{Text: "check this"},
			Embed: &Embed{
				Type:     "app.bsky.embed.external#view",
				External: &EmbedExternal{URI: "https://example.com/x"},
			},
		},
	}
	post := mapFeedItem(item, nil)
	if !strings.Contains(post.Content, "https://example.com/x") {
		t.Fatalf("embed url missing: %q", post.Content)
	}
}

func TestMapFeedItemReplyFallsBackToURI(t *testing.T) {
	item := FeedItem{
		Post: PostView{
			URI:    "at://did:plc:abc/app.bsky.feed.post/3",
			Author: Actor{Handle: "alice.bsky.social"},
			Record: Record{
				Text:  "yeah",
				Reply: &ReplyRef{Parent: &StrongRef{URI: "at://did:plc:xyz/app.bsky.feed.post/p"}},
			},
		},
	}
	// nil resolver and an unknown parent both fall back to the raw URI so
	// context is never silently dropped.
	post := mapFeedItem(item, nil)
	if !strings.Contains(post.Content, "(re: at://did:plc:xyz/app.bsky.feed.post/p)") {
		t.Fatalf("reply parent missing: %q", post.Content)
	}
}

func TestMapFeedItemReplyRendersResolvedSnippet(t *testing.T) {
	parentURI := "at://did:plc:xyz/app.bsky.feed.post/p"
	item := FeedItem{
		Post: PostView{
			URI:    "at://did:plc:abc/app.bsky.feed.post/3",
			Author: Actor{Handle: "alice.bsky.social"},
			Record: Record{
				Text:  "yeah agreed",
				Reply: &ReplyRef{Parent: &StrongRef{URI: parentURI}},
			},
		},
	}
	resolve := func(uri string) (parentRef, bool) {
		if uri != parentURI {
			return parentRef{}, false
		}
		return parentRef{handle: "bob.bsky.social", text: "the original hot take"}, true
	}
	post := mapFeedItem(item, resolve)
	want := "yeah agreed (re: @bob.bsky.social: \"the original hot take\")"
	if post.Content != want {
		t.Fatalf("content = %q, want %q", post.Content, want)
	}
}

func TestMapFeedItemQuotePost(t *testing.T) {
	// Plain quote: app.bsky.embed.record#view carries the quoted post directly.
	item := FeedItem{
		Post: PostView{
			URI:    "at://did:plc:abc/app.bsky.feed.post/4",
			Author: Actor{Handle: "alice.bsky.social"},
			Record: Record{Text: "look at this"},
			Embed: &Embed{
				Type: "app.bsky.embed.record#view",
				Record: &EmbedRecord{
					Type:   "app.bsky.embed.record#viewRecord",
					URI:    "at://did:plc:xyz/app.bsky.feed.post/q",
					Author: Actor{Handle: "carol.bsky.social"},
					Value:  &Record{Text: "the quoted hot take"},
				},
			},
		},
	}
	post := mapFeedItem(item, nil)
	want := "look at this (quoting @carol.bsky.social: \"the quoted hot take\")"
	if post.Content != want {
		t.Fatalf("content = %q, want %q", post.Content, want)
	}
}

func TestQuotedPost(t *testing.T) {
	// recordWithMedia#view nests the quoted viewRecord one level deeper, and
	// the media (images/external) is collected separately via Media.
	rwm := &Embed{
		Type: "app.bsky.embed.recordWithMedia#view",
		Record: &EmbedRecord{
			Type: "app.bsky.embed.record#view",
			Record: &EmbedRecord{
				Type:   "app.bsky.embed.record#viewRecord",
				Author: Actor{Handle: "dave.bsky.social"},
				Value:  &Record{Text: "nested quote"},
			},
		},
		Media: &Embed{External: &EmbedExternal{URI: "https://example.com/y"}},
	}
	q, ok := quotedPost(rwm)
	if !ok || q.handle != "dave.bsky.social" || q.text != "nested quote" {
		t.Fatalf("quoted = %+v ok=%v", q, ok)
	}
	if urls := collectEmbedURLs(rwm); len(urls) != 1 || urls[0] != "https://example.com/y" {
		t.Fatalf("media urls = %v", urls)
	}

	// notFound/blocked/feed-generator stubs leave Author and Value empty.
	stub := &Embed{Record: &EmbedRecord{Type: "app.bsky.embed.record#viewNotFound", URI: "at://x"}}
	if _, ok := quotedPost(stub); ok {
		t.Fatal("expected unhydrated quote stub to be skipped")
	}

	// Non-quote embeds yield nothing.
	if _, ok := quotedPost(&Embed{External: &EmbedExternal{URI: "https://example.com/z"}}); ok {
		t.Fatal("external embed is not a quote")
	}
	if _, ok := quotedPost(nil); ok {
		t.Fatal("nil embed is not a quote")
	}
}

func TestInlineParent(t *testing.T) {
	uri := "at://did:plc:xyz/app.bsky.feed.post/p"
	base := func() FeedItem {
		return FeedItem{
			Post: PostView{
				Record: Record{Reply: &ReplyRef{Parent: &StrongRef{URI: uri}}},
			},
		}
	}

	// Hydrated inline parent is used directly.
	item := base()
	item.Reply = &FeedReplyRef{Parent: &PostView{
		URI:    uri,
		Author: Actor{Handle: "bob.bsky.social"},
		Record: Record{Text: "hello"},
	}}
	if ref, ok := inlineParent(item, uri); !ok || ref.handle != "bob.bsky.social" || ref.text != "hello" {
		t.Fatalf("inline parent = %+v ok=%v", ref, ok)
	}

	// notFound/blocked stub (only URI set) is rejected so the caller fetches.
	stub := base()
	stub.Reply = &FeedReplyRef{Parent: &PostView{URI: uri}}
	if _, ok := inlineParent(stub, uri); ok {
		t.Fatal("expected stub parent to be rejected")
	}

	// URI mismatch is rejected.
	mism := base()
	mism.Reply = &FeedReplyRef{Parent: &PostView{
		URI:    "at://did:plc:other/app.bsky.feed.post/q",
		Record: Record{Text: "nope"},
	}}
	if _, ok := inlineParent(mism, uri); ok {
		t.Fatal("expected URI mismatch to be rejected")
	}
}

func TestTruncateSnippet(t *testing.T) {
	if got := truncateSnippet("  hello\n\nworld  ", 100); got != "hello world" {
		t.Fatalf("collapse = %q", got)
	}
	if got := truncateSnippet("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncateSnippet("abc def ghi", 4); got != "abc…" {
		t.Fatalf("trailing space trim = %q", got)
	}
}

func TestParentCache(t *testing.T) {
	c := newParentCache(2)
	c.put("a", parentRef{handle: "ha"})
	c.put("b", parentRef{handle: "hb"})
	if ref, ok := c.get("a"); !ok || ref.handle != "ha" {
		t.Fatalf("get a = %+v ok=%v", ref, ok)
	}
	c.put("c", parentRef{handle: "hc"}) // evicts "a"
	if _, ok := c.get("a"); ok {
		t.Fatal("a should have been evicted")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("c should be present")
	}
	// put on existing key updates in place without eviction churn.
	c.put("b", parentRef{handle: "hb2"})
	if ref, _ := c.get("b"); ref.handle != "hb2" {
		t.Fatalf("update b = %q", ref.handle)
	}
}

func TestURILRU(t *testing.T) {
	l := newURILRU(3)
	l.add("a")
	l.add("b")
	l.add("c")
	if !l.seen("a") || !l.seen("b") || !l.seen("c") {
		t.Fatal("expected a,b,c present")
	}
	l.add("d")
	if l.seen("a") {
		t.Fatal("a should have been evicted")
	}
	if !l.seen("d") {
		t.Fatal("d should be present")
	}
}

func TestSanitiseHandle(t *testing.T) {
	if got := sanitiseHandle("Alice.Bsky.Social"); got != "alice.bsky.social" {
		t.Fatalf("sanitised = %q", got)
	}
	if got := sanitiseHandle(""); got != "user" {
		t.Fatalf("empty = %q", got)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantSub string // substring expected in err; "" means no error
	}{
		{
			name: "ok",
			cfg: Config{
				Network:     "bsky",
				Identifier:  "alice",
				AppPassword: "p",
				Channels:    []ChannelConfig{{Kind: ChannelTimeline}},
			},
		},
		{
			name:    "missing network",
			cfg:     Config{Identifier: "alice", AppPassword: "p", Channels: []ChannelConfig{{Kind: ChannelTimeline}}},
			wantSub: "empty Network",
		},
		{
			name:    "missing identifier",
			cfg:     Config{Network: "bsky", AppPassword: "p", Channels: []ChannelConfig{{Kind: ChannelTimeline}}},
			wantSub: "credentials",
		},
		{
			name:    "missing password",
			cfg:     Config{Network: "bsky", Identifier: "alice", Channels: []ChannelConfig{{Kind: ChannelTimeline}}},
			wantSub: "credentials",
		},
		{
			name:    "no channels",
			cfg:     Config{Network: "bsky", Identifier: "alice", AppPassword: "p"},
			wantSub: "no channels",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Source{cfg: tc.cfg}
			err := s.validateConfig()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestPDSHost(t *testing.T) {
	if got := pdsHost("https://bsky.social/"); got != "bsky.social" {
		t.Fatalf("got %q", got)
	}
	if got := pdsHost(""); got != "bsky.social" {
		t.Fatalf("empty got %q", got)
	}
}
