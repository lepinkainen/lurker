package preview

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// pinningTransport clones http.DefaultTransport and installs a DialContext that
// resolves the host with resolver, rejects any IP failing IPAllowed, and dials
// the validated IP directly. This closes the DNS-rebinding TOCTOU gap between
// CheckURL (which resolves+validates but discards the result) and the dial,
// where http.DefaultTransport would otherwise re-resolve DNS.
//
// TLS SNI/ServerName is unaffected: the Transport still derives ServerName from
// the request URL host, and we intentionally leave TLSClientConfig.ServerName
// unset so certificate verification uses the hostname, not the pinned IP.
func pinningTransport(resolver Resolver) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	base, _ := http.DefaultTransport.(*http.Transport)
	tr := base.Clone()
	tr.DialContext = pinningDialContext(resolver)
	return tr
}

// pinningDialContext returns a DialContext that only dials IPs allowed by the
// SSRF policy, using the supplied resolver for name resolution.
func pinningDialContext(resolver Resolver) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("preview: split host port %q: %w", addr, err)
		}
		// Host is already an IP literal: validate and dial as-is.
		if ip := net.ParseIP(host); ip != nil {
			if !IPAllowed(ip) {
				return nil, fmt.Errorf("%w: ip %s not allowed", ErrBlocked, ip)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("preview: resolve %s: %w", host, err)
		}
		var blocked error
		for _, ia := range ips {
			if !IPAllowed(ia.IP) {
				blocked = fmt.Errorf("%w: ip %s not allowed", ErrBlocked, ia.IP)
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ia.IP.String(), port))
		}
		if blocked != nil {
			return nil, blocked
		}
		return nil, fmt.Errorf("%w: no ips for %s", ErrBlocked, host)
	}
}
