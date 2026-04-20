package api

import (
	"encoding/json"
	"net/http"
	"strings"

	ircdb "github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/irc"
)

type networkRequest struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      *bool  `json:"tls,omitzero"`
	Nick     string `json:"nick"`
	Realname string `json:"realname,omitzero"`
	SASLUser string `json:"sasl_user,omitzero"`
	SASLPass string `json:"sasl_pass,omitzero"`
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
	if _, err := s.Stores.OpenNetwork(r.Context(), created); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, toNetworkDTO(created, stateString(irc.StateDisconnected)))
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	updated, err := ircdb.UpdateNetwork(r.Context(), s.Stores.Control, id, req.toDBNetwork())
	if err != nil {
		writeNetworkDBError(w, err, http.StatusBadRequest)
		return
	}
	if before.Name != updated.Name {
		_ = s.Manager.StopNetwork(id)
		_ = s.Stores.CloseNetwork(id)
		if err := s.Stores.RenameNetworkLogDB(before.Name, updated.Name); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if _, err := s.Stores.OpenNetwork(r.Context(), updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
