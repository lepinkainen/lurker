// Package datasource provides the shared abstraction for non-IRC content
// sources (Bluesky, Mastodon, etc.) that surface posts as IRC-style buffers.
// Adapters live in subpackages (e.g. datasource/bluesky) and call IngestPost
// to land messages on the same hub/store path the IRC handler uses.
package datasource

import (
	"context"
	"time"

	"github.com/google/uuid"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/irc"
)

// Post is the source-agnostic shape an adapter produces for a single
// ingestable item. MsgID is the dedupe key (e.g. a Bluesky post URI).
type Post struct {
	MsgID     string
	Timestamp time.Time
	Sender    string
	Account   string
	Kind      string // "privmsg" | "notice" | "action"
	Target    string
	Content   string
}

// Deps is the runtime wiring a Source needs to land posts.
type Deps struct {
	Stores   *ircdb.MultiStore
	Hub      *hub.Hub
	Previews irc.PreviewEnqueuer
}

// Source is one configured non-IRC account/integration. Implementations own
// their own auth state, polling goroutines, and shutdown.
type Source interface {
	// Name is the network row name. Stable across restarts.
	Name() string
	// Kind is the network kind (e.g. ircdb.NetworkKindBluesky).
	Kind() string
	// Start performs authentication, ensures the network/buffer rows exist,
	// and launches background goroutines. Returns once startup is far enough
	// that future work runs detached.
	Start(ctx context.Context, deps Deps) error
	// NetworkID returns the upserted network row ID. Valid after Start.
	NetworkID() uuid.UUID
	// Wait blocks until all background goroutines exit.
	Wait()
}
