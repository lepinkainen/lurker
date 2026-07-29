package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
	"github.com/lepinkainen/lurker/mirc"
	"github.com/lepinkainen/lurker/preview"
)

// networkDTO is the wire shape for a network row. We don't ship sasl
// credentials to clients, ever.
type networkDTO struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	TLS       bool      `json:"tls"`
	Nick      string    `json:"nick"`
	NickColor *int      `json:"nick_color,omitempty"`
	Realname  string    `json:"realname,omitzero"`
	Status    string    `json:"status,omitzero"`
	SortOrder int       `json:"sort_order"`
	Disabled  bool      `json:"disabled,omitzero"`
	// InConfig reports whether the network is defined in config.yaml.
	// Networks not in the config are ephemeral: they are marked disabled on
	// the next backend restart.
	InConfig bool `json:"in_config"`
}

type bufferDTO struct {
	ID                     uuid.UUID `json:"id"`
	NetworkID              uuid.UUID `json:"network_id"`
	Name                   string    `json:"name"`
	Kind                   string    `json:"kind"`
	Topic                  string    `json:"topic,omitzero"`
	TopicSetBy             string    `json:"topic_set_by,omitzero"`
	TopicSetAt             string    `json:"topic_set_at,omitzero"`
	Joined                 bool      `json:"joined"`
	LastSeenID             uuid.UUID `json:"last_seen_id,omitzero"`
	MarkerID               uuid.UUID `json:"marker_id,omitzero"`
	MarkerTS               string    `json:"marker_ts,omitzero"`
	CreatedAt              string    `json:"created_at"`
	SortOrder              int64     `json:"sort_order"`
	ShowEmbeds             bool      `json:"show_embeds"`
	ShowPresenceEvents     bool      `json:"show_presence_events"`
	CollapsePresenceEvents bool      `json:"collapse_presence_events"`
	Pinned                 bool      `json:"pinned"`
	Archived               bool      `json:"archived"`
	PinOrder               int64     `json:"pin_order"`
	Unread                 int       `json:"unread"`
	Mentions               int       `json:"mentions"`
}

// messageDTO is the REST history shape: the shared wire core plus resolved
// URL previews. JSON key set must match irc.MessageEvent minus "type" (see
// wire_shape_test.go).
type messageDTO struct {
	irc.MessageCore
	Previews []preview.ResolvedPreview `json:"previews,omitzero"`
}

type stateDTO struct {
	Networks        []networkDTO                 `json:"networks"`
	Buffers         []bufferDTO                  `json:"buffers"`
	InitialMessages map[string][]messageDTO      `json:"initial_messages"`
	Members         map[string][]irc.ChannelUser `json:"members,omitzero"`
}

// stateManager is the IRC runtime state surface needed by /api/state.
type stateManager interface {
	StateSnapshot() map[uuid.UUID]string
	IsJoined(networkID uuid.UUID, channel string) bool
	ChannelMembers(networkID uuid.UUID, channel string) []irc.ChannelUser
	Nick(networkID uuid.UUID) string
}

// state serves the full snapshot a client needs to render from scratch:
// every network, every buffer, plus the last 100 messages per buffer.
// All per-network log DB access is batched: O(networks) queries for both
// recent messages and unread candidates.
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nets, err := ircdb.ListNetworks(ctx, s.Stores.Control)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bufs, err := s.Stores.ListAllBuffers(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := stateDTO{
		Networks:        make([]networkDTO, 0, len(nets)),
		Buffers:         make([]bufferDTO, 0, len(bufs)),
		InitialMessages: map[string][]messageDTO{},
		Members:         map[string][]irc.ChannelUser{},
	}
	states := map[uuid.UUID]string{}
	if s.Manager != nil {
		states = s.Manager.StateSnapshot()
	}
	kinds := make(map[uuid.UUID]string, len(nets))
	for _, n := range nets {
		kinds[n.ID] = n.Kind
		out.Networks = append(out.Networks, s.toNetworkDTO(n, states[n.ID]))
	}

	// Group buffers by network so we can issue one log-DB query per network.
	byNetwork := make(map[uuid.UUID][]ircdb.Buffer, len(nets))
	for _, b := range bufs {
		byNetwork[b.NetworkID] = append(byNetwork[b.NetworkID], b)
	}

	// Pre-fetch recent messages and unread candidates per network.
	recentByBuf, unreadByBuf := s.prefetchNetworkState(ctx, byNetwork, len(bufs))

	for _, b := range bufs {
		s.appendBufferToState(ctx, &out, b, kinds, recentByBuf, unreadByBuf)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) appendBufferToState(ctx context.Context, out *stateDTO, b ircdb.Buffer, kinds map[uuid.UUID]string, recentByBuf map[uuid.UUID][]ircdb.StoredMessage, unreadByBuf map[uuid.UUID]unreadTally) {
	joined := s.bufferJoined(b, kinds)
	counts := unreadByBuf[b.ID]
	out.Buffers = append(out.Buffers, bufferDTO{
		ID: b.ID, NetworkID: b.NetworkID, Name: b.Name, Kind: b.Kind,
		Topic: mirc.Strip(b.Topic), TopicSetBy: b.TopicSetBy, TopicSetAt: b.TopicSetAt,
		Joined: joined, LastSeenID: b.LastSeenID, CreatedAt: b.CreatedAt,
		SortOrder: b.SortOrder,
		MarkerID:  counts.MarkerID, MarkerTS: markerTS(counts.MarkerID),
		ShowEmbeds: b.ShowEmbeds, ShowPresenceEvents: b.ShowPresenceEvents,
		CollapsePresenceEvents: b.CollapsePresenceEvents, Pinned: b.Pinned,
		Archived: b.Archived, PinOrder: b.PinOrder,
		Unread: counts.Unread, Mentions: counts.Mentions,
	})
	if b.Kind == ircdb.BufferChannel && s.Manager != nil {
		if members := s.Manager.ChannelMembers(b.NetworkID, b.Name); members != nil {
			out.Members[b.ID.String()] = members
		}
	}
	out.InitialMessages[b.ID.String()] = s.toMessageDTOs(ctx, recentByBuf[b.ID])
}

// prefetchNetworkState issues one batch recent-messages query and one batch
// unread-candidates query per network, returning maps keyed by buffer ID.
func (s *Server) prefetchNetworkState(ctx context.Context, byNetwork map[uuid.UUID][]ircdb.Buffer, totalBufs int) (recentByBuf map[uuid.UUID][]ircdb.StoredMessage, unreadByBuf map[uuid.UUID]unreadTally) {
	recentByBuf = make(map[uuid.UUID][]ircdb.StoredMessage, totalBufs)
	unreadByBuf = make(map[uuid.UUID]unreadTally, totalBufs)
	for netID, netBufs := range byNetwork {
		nick := ""
		if s.Manager != nil {
			nick = s.Manager.Nick(netID)
		}
		bufIDs := make([]uuid.UUID, len(netBufs))
		for i, b := range netBufs {
			bufIDs[i] = b.ID
		}
		s.prefetchRecentMessages(ctx, netID, bufIDs, recentByBuf)
		s.prefetchUnreadCounts(ctx, netID, netBufs, nick, unreadByBuf)
	}
	return recentByBuf, unreadByBuf
}

func (s *Server) prefetchRecentMessages(ctx context.Context, netID uuid.UUID, bufIDs []uuid.UUID, out map[uuid.UUID][]ircdb.StoredMessage) {
	batchMsgs, err := s.Stores.BatchRecentMessages(ctx, netID, bufIDs, 100)
	if err != nil {
		slog.Error("batch recent messages", "err", err, "network_id", netID)
		return
	}
	maps.Copy(out, batchMsgs)
}

func (s *Server) prefetchUnreadCounts(ctx context.Context, netID uuid.UUID, netBufs []ircdb.Buffer, nick string, out map[uuid.UUID]unreadTally) {
	cutoffs := make(map[uuid.UUID]uuid.UUID, len(netBufs))
	for _, b := range netBufs {
		cutoffs[b.ID] = b.LastSeenID
	}
	batchUnread, err := s.Stores.BatchUnreadCandidates(ctx, netID, cutoffs, unreadCountsCap)
	if err != nil {
		slog.Error("batch unread candidates", "err", err, "network_id", netID)
		return
	}
	for bufID, cands := range batchUnread {
		out[bufID] = tallyUnread(cands, nick)
	}
}

// unreadTally is the server-derived read state of one buffer: unread and
// mention counts plus the "New messages" marker position (the id of the
// oldest unread message that counts; uuid.Nil when the buffer is caught up).
type unreadTally struct {
	Unread   int
	Mentions int
	MarkerID uuid.UUID
}

// tallyUnread folds unread candidates through ComputeMessageSemantics.
// Self-authored messages never count and never anchor the marker; with an
// unknown nick self-detection degrades to counting everything, same as
// mention detection.
func tallyUnread(cands []ircdb.UnreadCandidate, nick string) unreadTally {
	var t unreadTally
	for _, c := range cands {
		sem := irc.ComputeMessageSemantics(c.Kind, c.Sender, c.Content, "", nick)
		if !sem.CountsAsUnread || sem.IsSelf {
			continue
		}
		t.Unread++
		if sem.MentionsMe || sem.Highlight {
			t.Mentions++
		}
		if t.MarkerID == uuid.Nil || bytes.Compare(c.ID[:], t.MarkerID[:]) < 0 {
			t.MarkerID = c.ID
		}
	}
	return t
}

// markerTS renders the timestamp embedded in a UUIDv7 marker id as RFC3339,
// so clients can show "new since 14:02" without having the message loaded.
// Empty for uuid.Nil or non-v7 ids.
func markerTS(id uuid.UUID) string {
	if id == uuid.Nil || id.Version() != 7 {
		return ""
	}
	sec, nsec := id.Time().UnixTime()
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
}

func (s *Server) bufferJoined(b ircdb.Buffer, kinds map[uuid.UUID]string) bool {
	if b.Kind != ircdb.BufferChannel {
		return false
	}
	if isNonIRCNetwork(kinds[b.NetworkID]) {
		return true
	}
	if s.Manager == nil {
		return false
	}
	return s.Manager.IsJoined(b.NetworkID, b.Name)
}

// history serves older-than-cursor messages for a buffer.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	bufferID, ok := parsePathUUID(w, r, "id", "bad buffer id")
	if !ok {
		return
	}
	var before uuid.UUID
	if raw := r.URL.Query().Get("before"); raw != "" {
		if parsed, err := uuid.Parse(raw); err == nil {
			before = parsed
		}
	}
	msgs, err := s.loadHistory(r.Context(), bufferID, before, clampLimit(r.URL.Query().Get("limit"), 200, 500))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"buffer_id": bufferID,
		"messages":  msgs,
	})
}

func (s *Server) loadHistory(ctx context.Context, bufferID, before uuid.UUID, limit int) ([]messageDTO, error) {
	var (
		msgs []ircdb.StoredMessage
		err  error
	)
	if before != uuid.Nil {
		msgs, err = s.Stores.MessagesBefore(ctx, bufferID, before, limit)
	} else {
		msgs, err = s.Stores.RecentMessages(ctx, bufferID, limit)
	}
	if err != nil {
		return nil, err
	}
	return s.toMessageDTOs(ctx, msgs), nil
}

func (s *Server) toMessageDTOs(ctx context.Context, in []ircdb.StoredMessage) []messageDTO {
	out := make([]messageDTO, 0, len(in))
	nicks := map[uuid.UUID]string{}
	for _, m := range in {
		nick, ok := nicks[m.NetworkID]
		if !ok {
			if s.Manager != nil {
				nick = s.Manager.Nick(m.NetworkID)
			}
			nicks[m.NetworkID] = nick
		}
		core := irc.CoreFromStored(m)
		core.ApplySemantics(nick)
		out = append(out, messageDTO{MessageCore: core})
	}
	s.attachPreviews(ctx, out)
	annotateNetsplits(out)
	return out
}

// annotateNetsplits runs the server-side netsplit clustering over a batch of
// message DTOs and stamps members with their group annotation, mirroring the
// live tracker so clients render history and live events identically.
// Batches are per-buffer at both call sites, but group by buffer anyway so a
// mixed batch can't cross-cluster.
func annotateNetsplits(msgs []messageDTO) {
	byBuffer := map[uuid.UUID][]irc.PresenceEntry{}
	dtoByID := map[string]*messageDTO{}
	for i := range msgs {
		m := &msgs[i]
		if m.Kind != "quit" && m.Kind != "join" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, m.TS)
		if err != nil {
			continue
		}
		byBuffer[m.BufferID] = append(byBuffer[m.BufferID], irc.PresenceEntry{
			ID: m.ID.String(), Kind: m.Kind, Sender: m.Sender, Content: m.Content, TS: ts,
		})
		dtoByID[m.ID.String()] = m
	}
	for _, entries := range byBuffer {
		for _, g := range irc.GroupPresence(entries) {
			if g.Netsplit == nil {
				continue
			}
			meta := irc.MetaForNetsplit(g.Netsplit)
			for _, e := range append(append([]irc.PresenceEntry{}, g.Netsplit.Quits...), g.Netsplit.Rejoins...) {
				if dto := dtoByID[e.ID]; dto != nil {
					m := meta
					dto.Netsplit = &m
				}
			}
		}
	}
}

// attachPreviews populates Previews on every DTO by joining per-network
// message_previews link rows against the shared url_previews cache. Failures
// are non-fatal: the message still renders without previews.
func (s *Server) attachPreviews(ctx context.Context, msgs []messageDTO) {
	if s.Stores == nil || s.Stores.Previews == nil || len(msgs) == 0 {
		return
	}

	byNetwork, indexByMsg := indexMessagesByNetwork(msgs)
	urlSet, links := s.collectPreviewLinks(ctx, byNetwork, indexByMsg)
	if len(links) == 0 {
		return
	}
	urls := make([]string, 0, len(urlSet))
	for u := range urlSet {
		urls = append(urls, u)
	}
	cache, err := s.Stores.Previews.GetMany(ctx, urls)
	if err != nil {
		slog.Warn("preview cache get-many", "err", err)
		return
	}
	for _, l := range links {
		p, ok := cache[l.url]
		if !ok || !p.Displayable() {
			continue
		}
		msgs[l.msgIdx].Previews = append(msgs[l.msgIdx].Previews, preview.ToResolvedPreview(p))
	}
}

type previewLinkRef struct {
	msgIdx   int
	url      string
	position int
}

// indexMessagesByNetwork groups message IDs by network so each log DB is
// queried once, and builds a reverse index from (network, message) to the
// original slice position.
func indexMessagesByNetwork(msgs []messageDTO) (byNetwork map[uuid.UUID][]uuid.UUID, indexByMsg map[uuid.UUID]map[uuid.UUID]int) {
	byNetwork = map[uuid.UUID][]uuid.UUID{}
	indexByMsg = map[uuid.UUID]map[uuid.UUID]int{}
	for i, m := range msgs {
		byNetwork[m.NetworkID] = append(byNetwork[m.NetworkID], m.ID)
		if indexByMsg[m.NetworkID] == nil {
			indexByMsg[m.NetworkID] = map[uuid.UUID]int{}
		}
		indexByMsg[m.NetworkID][m.ID] = i
	}
	return byNetwork, indexByMsg
}

func (s *Server) collectPreviewLinks(ctx context.Context, byNetwork map[uuid.UUID][]uuid.UUID, indexByMsg map[uuid.UUID]map[uuid.UUID]int) (map[string]struct{}, []previewLinkRef) {
	urlSet := map[string]struct{}{}
	var links []previewLinkRef
	for networkID, ids := range byNetwork {
		logStore, err := s.Stores.LogStore(networkID)
		if err != nil {
			slog.Warn("preview log store", "err", err, "network_id", networkID)
			continue
		}
		rows, err := ircdb.ListMessagePreviewLinks(ctx, logStore.DB, ids)
		if err != nil {
			slog.Warn("preview links", "err", err, "network_id", networkID)
			continue
		}
		for _, r := range rows {
			idx, ok := indexByMsg[networkID][r.MessageID]
			if !ok {
				continue
			}
			urlSet[r.URL] = struct{}{}
			links = append(links, previewLinkRef{msgIdx: idx, url: r.URL, position: r.Position})
		}
	}
	return urlSet, links
}

// unreadCountsCap caps how many candidate rows we scan per buffer when
// computing unread/mention counts. The UI typically renders "999+" beyond
// this anyway, so we keep the query bounded.
const unreadCountsCap = 1000

// computeUnreadCounts queries the per-network log DB for messages newer than
// lastSeenID, then folds each through ComputeMessageSemantics to derive unread
// + mention counts and the marker position. Used for single-buffer updates
// (e.g. mark-last-seen). Failures degrade silently to a zero tally.
func (s *Server) computeUnreadCounts(ctx context.Context, networkID, bufferID, lastSeenID uuid.UUID, nick string) unreadTally {
	if s.Stores == nil {
		return unreadTally{}
	}
	logStore, err := s.Stores.LogStore(networkID)
	if err != nil {
		slog.Warn("unread log store", "err", err, "network_id", networkID)
		return unreadTally{}
	}
	cands, err := ircdb.UnreadCandidates(ctx, logStore.DB, bufferID, lastSeenID, unreadCountsCap)
	if err != nil {
		slog.Warn("unread candidates", "err", err, "buffer_id", bufferID)
		return unreadTally{}
	}
	return tallyUnread(cands, nick)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}
