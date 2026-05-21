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
	post := mapFeedItem(item)
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
	post := mapFeedItem(item)
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
	post := mapFeedItem(item)
	if !strings.Contains(post.Content, "https://example.com/x") {
		t.Fatalf("embed url missing: %q", post.Content)
	}
}

func TestMapFeedItemReplyAppendsParent(t *testing.T) {
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
	post := mapFeedItem(item)
	if !strings.Contains(post.Content, "re: at://did:plc:xyz/app.bsky.feed.post/p") {
		t.Fatalf("reply parent missing: %q", post.Content)
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
