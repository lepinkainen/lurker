package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

type networkRequest struct {
	Name            string   `json:"name"`
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	TLS             *bool    `json:"tls,omitzero"`
	Nick            string   `json:"nick"`
	Realname        string   `json:"realname,omitzero"`
	SASLUser        string   `json:"sasl_user,omitzero"`
	SASLPass        string   `json:"sasl_pass,omitzero"`
	ConnectCommands []string `json:"connect_commands,omitzero"`
	Disabled        *bool    `json:"disabled,omitzero"`
}

type connectCommandsRequest struct {
	Commands []string `json:"commands"`
}

type reorderNetworksRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

// networkManager is the IRC lifecycle/state surface needed by network API
// handlers.
type networkManager interface {
	StateSnapshot() map[uuid.UUID]string
	StartNetwork(networkID uuid.UUID, nc irc.NetworkConfig) error
	StopNetwork(networkID uuid.UUID) error
}

func (r networkRequest) toDBNetwork() ircdb.Network {
	useTLS := true
	if r.TLS != nil {
		useTLS = *r.TLS
	}
	return ircdb.Network{
		Name:     strings.TrimSpace(r.Name),
		Host:     strings.TrimSpace(r.Host),
		Port:     r.Port,
		TLS:      useTLS,
		Nick:     strings.TrimSpace(r.Nick),
		Realname: strings.TrimSpace(r.Realname),
		SASLUser: r.SASLUser,
		SASLPass: r.SASLPass,
	}
}

func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req networkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	n := req.toDBNetwork()
	if n.Name == "" || n.Host == "" || n.Port == 0 || n.Nick == "" {
		http.Error(w, "name, host, port, and nick are required", http.StatusBadRequest)
		return
	}
	created, err := ircdb.CreateNetwork(r.Context(), s.Stores.Control, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ircdb.SetNetworkConnectCommands(r.Context(), s.Stores.Control, created.ID, req.ConnectCommands); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.Stores.OpenNetwork(r.Context(), created); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, s.toNetworkDTO(created, stateString(irc.StateDisconnected)))
}

func (s *Server) reorderNetworks(w http.ResponseWriter, r *http.Request) {
	var req reorderNetworksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "ids are required", http.StatusBadRequest)
		return
	}
	if err := ircdb.ReorderNetworks(r.Context(), s.Stores.Control, req.IDs); err != nil {
		writeNetworkDBError(w, err, http.StatusBadRequest)
		return
	}
	nets, err := ircdb.ListNetworks(r.Context(), s.Stores.Control)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	states := map[uuid.UUID]string{}
	if s.Manager != nil {
		states = s.Manager.StateSnapshot()
	}
	out := make([]networkDTO, 0, len(nets))
	for _, n := range nets {
		out = append(out, s.toNetworkDTO(n, states[n.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"networks": out})
}

func (s *Server) patchNetwork(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	before, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id)
	if err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	if isNonIRCNetwork(before.Kind) {
		http.Error(w, "patching "+before.Kind+" networks is not supported", http.StatusBadRequest)
		return
	}
	var req networkRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if !s.applyDisabledToggle(w, r, id, before, req) {
		return
	}

	dbNet := req.toDBNetwork()
	if req.TLS == nil {
		dbNet.TLS = before.TLS
	}
	if req.ConnectCommands != nil {
		if setErr := ircdb.SetNetworkConnectCommands(r.Context(), s.Stores.Control, id, req.ConnectCommands); setErr != nil {
			http.Error(w, setErr.Error(), http.StatusInternalServerError)
			return
		}
	}

	updated, ok := s.applyNetworkUpdate(w, r, id, before, dbNet, req)
	if !ok {
		return
	}

	status := ""
	if s.Manager != nil {
		status = s.Manager.StateSnapshot()[id]
	}
	writeJSON(w, http.StatusOK, s.toNetworkDTO(updated, status))
}

// applyDisabledToggle persists a disabled change when the request includes one.
// Returns false when an error response was already written.
func (s *Server) applyDisabledToggle(w http.ResponseWriter, r *http.Request, id uuid.UUID, before ircdb.Network, req networkRequest) bool {
	if req.Disabled == nil || *req.Disabled == before.Disabled {
		return true
	}
	if *req.Disabled && s.Manager != nil {
		_ = s.Manager.StopNetwork(id)
	}
	if setErr := ircdb.SetNetworkDisabled(r.Context(), s.Stores.Control, id, *req.Disabled); setErr != nil {
		writeNetworkDBError(w, setErr, http.StatusInternalServerError)
		return false
	}
	return true
}

// applyNetworkUpdate writes the network record when any connection field
// changed, otherwise reloads the row. Handles renames by reopening the
// per-network log DB. Returns the latest record and false when an error
// response was already written.
func (s *Server) applyNetworkUpdate(w http.ResponseWriter, r *http.Request, id uuid.UUID, before ircdb.Network, dbNet ircdb.Network, req networkRequest) (ircdb.Network, bool) {
	if !networkFieldsChanged(dbNet, req) {
		updated, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id)
		if err != nil {
			writeNetworkDBError(w, err, http.StatusInternalServerError)
			return ircdb.Network{}, false
		}
		return updated, true
	}
	updated, err := ircdb.UpdateNetwork(r.Context(), s.Stores.Control, id, dbNet)
	if err != nil {
		writeNetworkDBError(w, err, http.StatusBadRequest)
		return ircdb.Network{}, false
	}
	if before.Name != updated.Name {
		if !s.reopenRenamedNetworkLog(w, r, id, before.Name, updated) {
			return ircdb.Network{}, false
		}
	}
	return updated, true
}

func networkFieldsChanged(dbNet ircdb.Network, req networkRequest) bool {
	return dbNet.Name != "" || dbNet.Host != "" || dbNet.Port != 0 || dbNet.Nick != "" || req.TLS != nil
}

func (s *Server) reopenRenamedNetworkLog(w http.ResponseWriter, r *http.Request, id uuid.UUID, oldName string, updated ircdb.Network) bool {
	_ = s.Manager.StopNetwork(id)
	_ = s.Stores.CloseNetwork(id)
	if renameErr := s.Stores.RenameNetworkLogDB(oldName, updated.Name); renameErr != nil {
		http.Error(w, renameErr.Error(), http.StatusConflict)
		return false
	}
	if _, openErr := s.Stores.OpenNetwork(r.Context(), updated); openErr != nil {
		http.Error(w, openErr.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func (s *Server) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	if s.Manager != nil {
		_ = s.Manager.StopNetwork(id)
	}
	if err := s.Stores.DeleteNetwork(r.Context(), id); err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getNetworkConnectCommands(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	if _, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id); err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	commands, err := ircdb.ListNetworkConnectCommands(r.Context(), s.Stores.Control, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, connectCommandsRequest{Commands: commands})
}

func (s *Server) putNetworkConnectCommands(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	var req connectCommandsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if _, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id); err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	if err := ircdb.SetNetworkConnectCommands(r.Context(), s.Stores.Control, id, req.Commands); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	commands, err := ircdb.ListNetworkConnectCommands(r.Context(), s.Stores.Control, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, connectCommandsRequest{Commands: commands})
}

func (s *Server) connectNetwork(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	n, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id)
	if err != nil {
		writeNetworkDBError(w, err, http.StatusInternalServerError)
		return
	}
	if isNonIRCNetwork(n.Kind) {
		http.Error(w, "connect is only supported on IRC networks", http.StatusBadRequest)
		return
	}
	nc := irc.NetworkConfig{
		Name: n.Name,
		Servers: []irc.ServerConfig{{
			Host: n.Host,
			Port: n.Port,
			TLS:  n.TLS,
		}},
		Nick:     n.Nick,
		User:     n.Nick,
		Realname: n.Realname,
		SASLUser: n.SASLUser,
		SASLPass: n.SASLPass,
	}
	commands, err := ircdb.ListNetworkConnectCommands(r.Context(), s.Stores.Control, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nc.ConnectCommands = commands
	if err := s.Manager.StartNetwork(id, nc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"network_id": id, "state": stateString(irc.StateConnecting)})
}

func (s *Server) disconnectNetwork(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNetworkID(w, r)
	if !ok {
		return
	}
	// Disconnect is a tolerant no-op for unknown network IDs (matches prior
	// behaviour). For known rows we still gate on kind so a Bluesky source
	// can't be torn down through the IRC connect/disconnect plumbing.
	if n, err := ircdb.GetNetwork(r.Context(), s.Stores.Control, id); err == nil && isNonIRCNetwork(n.Kind) {
		http.Error(w, "disconnect is only supported on IRC networks", http.StatusBadRequest)
		return
	}
	if err := s.Manager.StopNetwork(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"network_id": id, "state": stateString(irc.StateDisconnected)})
}
