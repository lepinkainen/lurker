package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

func parsePathUUID(w http.ResponseWriter, r *http.Request, key, msg string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(key))
	if err != nil {
		http.Error(w, msg, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

func parseOptionalQueryUUID(w http.ResponseWriter, r *http.Request, key, msg string) (uuid.UUID, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, msg, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

func clampLimit(raw string, def, maxLimit int) int {
	limit, _ := strconv.Atoi(raw)
	return clampLimitInt(limit, def, maxLimit)
}

func clampLimitInt(limit, def, maxLimit int) int {
	if limit <= 0 || limit > maxLimit {
		return def
	}
	return limit
}

func parseNetworkID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return parsePathUUID(w, r, "id", "bad network id")
}

func writeNetworkDBError(w http.ResponseWriter, err error, fallbackStatus int) {
	status := fallbackStatus
	if errors.Is(err, ircdb.ErrNetworkNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, ircdb.ErrInvalidNetworkReorder) {
		status = http.StatusBadRequest
	}
	http.Error(w, err.Error(), status)
}

func toNetworkDTO(n ircdb.Network, status string) networkDTO {
	return networkDTO{
		ID: n.ID, Name: n.Name, Host: n.Host, Port: n.Port,
		TLS: n.TLS, Nick: n.Nick, Realname: n.Realname, Status: status, SortOrder: n.SortOrder,
		Disabled: n.Disabled,
	}
}

func stateString(s irc.NetworkState) string {
	return s.String()
}
