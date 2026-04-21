package api

import (
	"errors"
	"net/http"
	"strconv"

	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/irc"
)

func parsePathInt64(w http.ResponseWriter, r *http.Request, key, msg string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		http.Error(w, msg, http.StatusBadRequest)
		return 0, false
	}
	return v, true
}

func parseOptionalQueryInt64(w http.ResponseWriter, r *http.Request, key, msg string) (int64, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.Error(w, msg, http.StatusBadRequest)
		return 0, false
	}
	return v, true
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

func parseNetworkID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return parsePathInt64(w, r, "id", "bad network id")
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
	}
}

func stateString(s irc.NetworkState) string {
	return s.String()
}
