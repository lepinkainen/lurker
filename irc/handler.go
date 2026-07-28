package irc

import (
	"database/sql"
	"sync"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

// handler is the glue between a girc.Client and the SQLite store. One
// instance per network connection.
type handler struct {
	stores              *ircdb.MultiStore
	db                  *sql.DB
	hub                 *hub.Hub
	previews            PreviewEnqueuer
	networkID           uuid.UUID
	networkName         string
	autojoin            []string
	connectCommands     []string
	connectedHook       func(currentNick string)
	nickFn              func() string
	memberListHook      func(channel string)
	clearMemberListHook func(channel string)
	setJoinedHook       func(channel string, joined bool)
	drainJoinedHook     func() []string
	listEntries         []ChannelListEntry // accumulator for /LIST responses
	// lastMemberListHash deduplicates member_list republishes per channel.
	// Manual /WHO or repeated state events would otherwise broadcast the
	// full roster to every WS client even when nothing changed.
	lastMemberListHash map[string]uint64
	// modeMemberRefresh holds channels whose MODE event may change member
	// prefixes. onAllEvent fills it before girc's MODE handlers run, and
	// onStateUpdate drains it after girc has updated User.Perms.
	modeMemberRefreshMu sync.Mutex
	modeMemberRefresh   map[string]struct{}
	// userChannels mirrors nick→channel membership so that on QUIT we can
	// fan out to every channel the user shared with us. girc's internal
	// QUIT handler runs before ours and purges the user from its own
	// state, so we have to track it ourselves.
	userChannels *userChannels
	// netsplits clusters live quit/join events so published messages carry
	// their netsplit annotation (clients group by it instead of re-deriving).
	netsplits *netsplitTracker
}
