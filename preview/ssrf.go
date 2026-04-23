package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// ErrBlocked is returned when SSRF policy rejects a target.
var ErrBlocked = errors.New("preview: blocked by SSRF policy")

// Resolver resolves a hostname to IPs. Replaced in tests.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// CheckURL validates a URL's scheme, port, and resolved IPs against the
// SSRF policy. It returns nil when safe.
func CheckURL(ctx context.Context, resolver Resolver, raw string) error {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("preview: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: non-http scheme %q", ErrBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlocked)
	}
	port := u.Port()
	if port != "" && port != "80" && port != "443" {
		return fmt.Errorf("%w: disallowed port %s", ErrBlocked, port)
	}
	// If host is already an IP literal, check it directly.
	if ip := net.ParseIP(host); ip != nil {
		if !IPAllowed(ip) {
			return fmt.Errorf("%w: ip %s not allowed", ErrBlocked, ip)
		}
		return nil
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("preview: resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: no ips for %s", ErrBlocked, host)
	}
	for _, ia := range ips {
		if !IPAllowed(ia.IP) {
			return fmt.Errorf("%w: ip %s not allowed", ErrBlocked, ia.IP)
		}
	}
	return nil
}

// IPAllowed returns false for loopback, private, link-local, multicast, and
// unspecified addresses. Public globally routable addresses return true.
func IPAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// Reject IPv4-mapped private ranges and common cloud metadata addresses.
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 already covered by IsLinkLocalUnicast.
		// 0.0.0.0/8 covered by IsUnspecified for 0.0.0.0.
		// 100.64.0.0/10 (carrier-grade NAT)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
	}
	return true
}
