package irc

import (
	"database/sql"

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
	memberListHook      func(channel string)
	clearMemberListHook func(channel string)
	setJoinedHook       func(channel string, joined bool)
	drainJoinedHook     func() []string
	listEntries         []ChannelListEntry // accumulator for /LIST responses
}
