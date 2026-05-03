package api

import (
	"net"
	"net/http"
	"net/netip"
)

type tailscaleClass string

const (
	classTailscale tailscaleClass = "tailscale"
	classLocal     tailscaleClass = "local"
	classExternal  tailscaleClass = "external"
	classUnknown   tailscaleClass = "unknown"
)

var (
	tsCGNAT = netip.MustParsePrefix("100.64.0.0/10")
	tsULA   = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

func classifyRemoteAddr(remoteAddr string) (class tailscaleClass, ip string) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return classUnknown, host
	}
	addr = addr.Unmap()
	ip = addr.String()
	switch {
	case addr.IsLoopback():
		return classLocal, ip
	case tsCGNAT.Contains(addr) || tsULA.Contains(addr):
		return classTailscale, ip
	default:
		return classExternal, ip
	}
}

func (s *Server) tailscaleStatus(w http.ResponseWriter, r *http.Request) {
	class, ip := classifyRemoteAddr(r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    string(class),
		"remote_ip": ip,
	})
}
