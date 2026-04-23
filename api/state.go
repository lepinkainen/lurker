package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/preview"
)

// networkDTO is the wire shape for a network row. We don't ship sasl
// credentials to clients, ever.
type networkDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TLS       bool   `json:"tls"`
	Nick      string `json:"nick"`
	Realname  string `json:"realname,omitzero"`
	Status    string `json:"status,omitzero"`
	SortOrder int    `json:"sort_order"`
}

type bufferDTO struct {
	ID         int64  `json:"id"`
	NetworkID  int64  `json:"network_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Topic      string `json:"topic,omitzero"`
	Joined     bool   `json:"joined"`
	LastSeenID int64  `json:"last_seen_id,omitzero"`
	CreatedAt  string `json:"created_at"`
}

type messageDTO struct {
	ID        int64                     `json:"id"`
	NetworkID int64                     `json:"network_id"`
	BufferID  int64                     `json:"buffer_id"`
	MsgID     string                    `json:"msgid,omitzero"`
	TS        string                    `json:"ts"`
	Sender    string                    `json:"sender"`
	Account   string                    `json:"account,omitzero"`
	Kind      string                    `json:"kind"`
	Target    string                    `json:"target,omitzero"`
	Content   string                    `json:"content"`
	Previews  []preview.ResolvedPreview `json:"previews,omitzero"`
}

type channelMemberDTO struct {
	Nick   string `json:"nick"`
	Prefix string `json:"prefix,omitzero"`
	Away   bool   `json:"away"`
	Self   bool   `json:"self"`
}

type stateDTO struct {
	Networks        []networkDTO                  `json:"networks"`
	Buffers         []bufferDTO                   `json:"buffers"`
	InitialMessages map[string][]messageDTO       `json:"initial_messages"` // keyed by buffer id (string so JSON handles large ids cleanly)
	Members         map[string][]channelMemberDTO `json:"members,omitzero"`
}

// state serves the full snapshot a client needs to render from scratch:
// every network, every buffer, plus the last 100 messages per buffer.
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
		Members:         map[string][]channelMemberDTO{},
	}
	states := map[int64]string{}
	if s.Manager != nil {
		states = s.Manager.StateSnapshot()
	}
	for _, n := range nets {
		out.Networks = append(out.Networks, toNetworkDTO(n, states[n.ID]))
	}
	for _, b := range bufs {
		out.Buffers = append(out.Buffers, bufferDTO{
			ID: b.ID, NetworkID: b.NetworkID, Name: b.Name, Kind: b.Kind,
			Topic: b.Topic, Joined: b.Joined, LastSeenID: b.LastSeenID, CreatedAt: b.CreatedAt,
		})
		if b.Kind == ircdb.BufferChannel && s.Manager != nil {
			if members := s.Manager.ChannelMembers(b.NetworkID, b.Name); members != nil {
				out.Members[strconv.FormatInt(b.ID, 10)] = toChannelMemberDTOs(members)
			}
		}
		msgs, err := s.Stores.RecentMessages(ctx, b.ID, 100)
		if err != nil {
			slog.Error("recent messages", "err", err, "buffer_id", b.ID)
			continue
		}
		out.InitialMessages[strconv.FormatInt(b.ID, 10)] = s.toMessageDTOs(ctx, msgs)
	}

	writeJSON(w, http.StatusOK, out)
}

// history serves older-than-cursor messages for a buffer.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	bufferID, ok := parsePathInt64(w, r, "id", "bad buffer id")
	if !ok {
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
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

func (s *Server) loadHistory(ctx context.Context, bufferID, before int64, limit int) ([]messageDTO, error) {
	var (
		msgs []ircdb.StoredMessage
		err  error
	)
	if before > 0 {
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
	for _, m := range in {
		out = append(out, messageDTO{
			ID: m.ID, NetworkID: m.NetworkID, BufferID: m.BufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	s.attachPreviews(ctx, out)
	return out
}

// attachPreviews populates Previews on every DTO by joining per-network
// message_previews link rows against the shared url_previews cache. Failures
// are non-fatal: the message still renders without previews.
func (s *Server) attachPreviews(ctx context.Context, msgs []messageDTO) {
	if s.Stores == nil || s.Stores.Previews == nil || len(msgs) == 0 {
		return
	}

	// Group message IDs by network so we hit each log DB once.
	byNetwork := map[int64][]int64{}
	indexByMsg := map[int64]map[int64]int{} // network -> message id -> index in msgs
	for i, m := range msgs {
		byNetwork[m.NetworkID] = append(byNetwork[m.NetworkID], m.ID)
		if indexByMsg[m.NetworkID] == nil {
			indexByMsg[m.NetworkID] = map[int64]int{}
		}
		indexByMsg[m.NetworkID][m.ID] = i
	}

	urlSet := map[string]struct{}{}
	type linkRef struct {
		msgIdx   int
		url      string
		position int
	}
	var links []linkRef
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
			links = append(links, linkRef{msgIdx: idx, url: r.URL, position: r.Position})
		}
	}
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

func toChannelMemberDTOs(in []ircdb.ChannelMember) []channelMemberDTO {
	out := make([]channelMemberDTO, 0, len(in))
	for _, m := range in {
		out = append(out, channelMemberDTO{Nick: m.Nick, Prefix: m.Prefix, Away: m.Away, Self: m.Self})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
