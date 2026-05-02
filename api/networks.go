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
	writeJSON(w, http.StatusCreated, toNetworkDTO(created, stateString(irc.StateDisconnected)))
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
		out = append(out, toNetworkDTO(n, states[n.ID]))
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
	var req networkRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&req)
	if decodeErr != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Handle disabled toggle separately before the config update.
	if req.Disabled != nil && *req.Disabled != before.Disabled {
		if *req.Disabled && s.Manager != nil {
			_ = s.Manager.StopNetwork(id)
		}
		if setErr := ircdb.SetNetworkDisabled(r.Context(), s.Stores.Control, id, *req.Disabled); setErr != nil {
			writeNetworkDBError(w, setErr, http.StatusInternalServerError)
			return
		}
	}

	// Only update other fields if any non-disabled fields were sent.
	dbNet := req.toDBNetwork()
	var updated ircdb.Network
	if req.ConnectCommands != nil {
		setErr := ircdb.SetNetworkConnectCommands(r.Context(), s.Stores.Control, id, req.ConnectCommands)
		if setErr != nil {
			http.Error(w, setErr.Error(), http.StatusInternalServerError)
			return
		}
	}

	if dbNet.Name != "" || dbNet.Host != "" || dbNet.Port != 0 || dbNet.Nick != "" {
		updated, err = ircdb.UpdateNetwork(r.Context(), s.Stores.Control, id, dbNet)
		if err != nil {
			writeNetworkDBError(w, err, http.StatusBadRequest)
			return
		}
		if before.Name != updated.Name {
			_ = s.Manager.StopNetwork(id)
			_ = s.Stores.CloseNetwork(id)
			if renameErr := s.Stores.RenameNetworkLogDB(before.Name, updated.Name); renameErr != nil {
				http.Error(w, renameErr.Error(), http.StatusConflict)
				return
			}
			if _, openErr := s.Stores.OpenNetwork(r.Context(), updated); openErr != nil {
				http.Error(w, openErr.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		updated, err = ircdb.GetNetwork(r.Context(), s.Stores.Control, id)
		if err != nil {
			writeNetworkDBError(w, err, http.StatusInternalServerError)
			return
		}
	}

	status := ""
	if s.Manager != nil {
		status = s.Manager.StateSnapshot()[id]
	}
	writeJSON(w, http.StatusOK, toNetworkDTO(updated, status))
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
	if err := s.Manager.StartNetwork(r.Context(), id, nc); err != nil {
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
	if err := s.Manager.StopNetwork(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"network_id": id, "state": stateString(irc.StateDisconnected)})
}
