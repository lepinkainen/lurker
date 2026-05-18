package bluesky

import (
	"context"
	"fmt"
	"time"
)

// ChannelKind enumerates the supported Bluesky-side feed kinds. Only
// "timeline" is wired up today; "search", "list", "feed", and "notifications"
// are reserved so the switch in fetchPage can grow without touching the
// source/manager layer.
type ChannelKind string

// Supported (and reserved-for-future) channel kinds. Only ChannelTimeline
// is wired up in fetchPage today.
const (
	ChannelTimeline      ChannelKind = "timeline"
	ChannelSearch        ChannelKind = "search"        // future
	ChannelList          ChannelKind = "list"          // future
	ChannelFeed          ChannelKind = "feed"          // future
	ChannelNotifications ChannelKind = "notifications" // future
)

// ChannelConfig describes one ingestion channel within a Bluesky source.
// Name is the buffer name as it appears in the UI. Kind selects the XRPC
// endpoint dispatched in fetchPage.
type ChannelConfig struct {
	Name     string
	Kind     ChannelKind
	Interval time.Duration

	// Per-kind parameters (unused today). Populated by future channel kinds:
	//   Query for ChannelSearch, URI for ChannelList/ChannelFeed.
	Query string
	URI   string
}

// defaultInterval returns the poll interval used when the config does not
// override it. Only ChannelTimeline is wired up today; future kinds add
// their own cases here.
func defaultInterval(_ ChannelKind) time.Duration {
	return 60 * time.Second
}

// fetchPage returns the newest page of items for a channel. Timeline
// polling always asks for the newest page and relies on per-buffer dedupe;
// future cursor-aware kinds will grow new arms here.
func (s *Source) fetchPage(ctx context.Context, ch ChannelConfig) (*FeedResponse, error) {
	switch ch.Kind {
	case ChannelTimeline:
		return s.client.GetTimeline(ctx, 50, "")
	case ChannelSearch, ChannelList, ChannelFeed, ChannelNotifications:
		return nil, fmt.Errorf("bluesky: channel kind %q not implemented yet", ch.Kind)
	default:
		return nil, fmt.Errorf("bluesky: unknown channel kind %q", ch.Kind)
	}
}
