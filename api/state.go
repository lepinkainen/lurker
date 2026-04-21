package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	ircdb "github.com/lepinkainen/research/irc-service/db"
)

// networkDTO is the wire shape for a network row. We don't ship sasl
// credentials to clients, ever.
type networkDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      bool   `json:"tls"`
	Nick     string `json:"nick"`
	Realname string `json:"realname,omitzero"`
	Status   string `json:"status,omitzero"`
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
	ID        int64  `json:"id"`
	NetworkID int64  `json:"network_id"`
	BufferID  int64  `json:"buffer_id"`
	MsgID     string `json:"msgid,omitzero"`
	TS        string `json:"ts"`
	Sender    string `json:"sender"`
	Account   string `json:"account,omitzero"`
	Kind      string `json:"kind"`
	Target    string `json:"target,omitzero"`
	Content   string `json:"content"`
}

type channelMemberDTO struct {
	Nick   string `json:"nick"`
	Prefix string `json:"prefix,omitzero"`
	Away   bool   `json:"away"`
	Self   bool   `json:"self"`
}

type stateDTO struct {
	Networks        []networkDTO                    `json:"networks"`
	Buffers         []bufferDTO                     `json:"buffers"`
	InitialMessages map[string][]messageDTO         `json:"initial_messages"` // keyed by buffer id (string so JSON handles large ids cleanly)
	Members         map[string][]channelMemberDTO   `json:"members,omitzero"`
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
			members := s.Manager.ChannelMembers(b.NetworkID, b.Name)
			if len(members) > 0 {
				out.Members[strconv.FormatInt(b.ID, 10)] = toChannelMemberDTOs(members)
			}
		}
		msgs, err := s.Stores.RecentMessages(ctx, b.ID, 100)
		if err != nil {
			slog.Error("recent messages", "err", err, "buffer_id", b.ID)
			continue
		}
		out.InitialMessages[strconv.FormatInt(b.ID, 10)] = toMessageDTOs(msgs)
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
	limit := clampLimit(r.URL.Query().Get("limit"), 200, 500)
	var (
		msgs []ircdb.StoredMessage
		err  error
	)
	if before > 0 {
		msgs, err = s.Stores.MessagesBefore(r.Context(), bufferID, before, limit)
	} else {
		msgs, err = s.Stores.RecentMessages(r.Context(), bufferID, limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"buffer_id": bufferID,
		"messages":  toMessageDTOs(msgs),
	})
}

func toMessageDTOs(in []ircdb.StoredMessage) []messageDTO {
	out := make([]messageDTO, 0, len(in))
	for _, m := range in {
		out = append(out, messageDTO{
			ID: m.ID, NetworkID: m.NetworkID, BufferID: m.BufferID,
			MsgID: m.MsgID, TS: m.TS, Sender: m.Sender, Account: m.Account,
			Kind: m.Kind, Target: m.Target, Content: m.Content,
		})
	}
	return out
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
