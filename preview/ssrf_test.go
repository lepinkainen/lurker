package preview

import (
	"context"
	"errors"
	"net"
	"testing"
)

type staticResolver struct {
	ips map[string][]net.IPAddr
	err error
}

func (s staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.ips[host], nil
}

func TestIPAllowed(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"172.16.0.1", false},
		{"169.254.169.254", false}, // AWS metadata
		{"100.64.0.1", false},      // CGNAT
		{"0.0.0.0", false},
		{"::1", false},
		{"fe80::1", false},
		{"fd00::1", false},
		{"2606:4700:4700::1111", true}, // cloudflare public v6
	}
	for _, tc := range cases {
		got := IPAllowed(net.ParseIP(tc.ip))
		if got != tc.want {
			t.Errorf("IPAllowed(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestCheckURL(t *testing.T) {
	publicRes := staticResolver{ips: map[string][]net.IPAddr{
		"public.test": {{IP: net.ParseIP("8.8.8.8")}},
	}}
	privateRes := staticResolver{ips: map[string][]net.IPAddr{
		"internal.test": {{IP: net.ParseIP("10.0.0.5")}},
	}}

	if err := CheckURL(context.Background(), publicRes, "https://public.test/foo"); err != nil {
		t.Fatalf("public host rejected: %v", err)
	}
	err := CheckURL(context.Background(), privateRes, "https://internal.test/foo")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("private host not blocked: %v", err)
	}
	err = CheckURL(context.Background(), publicRes, "ftp://public.test/")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("ftp scheme not blocked: %v", err)
	}
	err = CheckURL(context.Background(), publicRes, "https://public.test:22/")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("odd port not blocked: %v", err)
	}
	// IP literal path.
	if err := CheckURL(context.Background(), publicRes, "http://127.0.0.1/"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback literal not blocked: %v", err)
	}
}
