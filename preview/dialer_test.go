package preview

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestPinningDialContextRejectsPrivateIPs(t *testing.T) {
	cases := []struct {
		name string
		ip   string
	}{
		{"loopback", "127.0.0.1"},
		{"metadata", "169.254.169.254"},
		{"private", "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := staticResolver{ips: map[string][]net.IPAddr{
				"evil.example": {{IP: net.ParseIP(tc.ip)}},
			}}
			dial := pinningDialContext(res)
			conn, err := dial(context.Background(), "tcp", "evil.example:80")
			if conn != nil {
				_ = conn.Close()
				t.Fatalf("expected no connection for %s", tc.ip)
			}
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("expected ErrBlocked for %s, got %v", tc.ip, err)
			}
		})
	}
}

func TestPinningDialContextRejectsIPLiteral(t *testing.T) {
	dial := pinningDialContext(staticResolver{})
	conn, err := dial(context.Background(), "tcp", "127.0.0.1:80")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("expected no connection for loopback literal")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}

func TestPinningDialContextAllowsPublicIP(t *testing.T) {
	res := staticResolver{ips: map[string][]net.IPAddr{
		"good.example": {{IP: net.ParseIP("203.0.113.10")}},
	}}
	dial := pinningDialContext(res)
	// Cancel the context so the dial fails immediately without touching the
	// network. A non-ErrBlocked failure proves the IP passed policy and the
	// dialer proceeded to connect.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, err := dial(ctx, "tcp", "good.example:80")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		return // dial somehow succeeded; policy still allowed it
	}
	if errors.Is(err, ErrBlocked) {
		t.Fatalf("public IP should not be blocked, got %v", err)
	}
}
