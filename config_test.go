package main

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
)

func TestLoadNetworksFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
nick: globalnick
user: globaluser
realname: Global Real Name
networks:
  - network: Ircnet
    nick: mynick
    realname: My Real Name
    channels: ["#test"]
    connect_commands:
      - "PRIVMSG NickServ :IDENTIFY secret"
      - "MODE mynick +x"
    servers:
      - host: irc.ircnet.com
        port: 6697
        tls: true
        tls_max_version: "1.3"
      - host: open.ircnet.net
        tls: false
  - network: Libera
    servers:
      - host: irc.libera.chat
        port: 6697
        tls: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	nets := parsed.Networks
	if len(nets) != 2 {
		t.Fatalf("len = %d, want 2", len(nets))
	}
	if nets[0].Name != "Ircnet" || nets[1].Name != "Libera" {
		t.Fatalf("unexpected network names: %+v", nets)
	}
	if nets[0].Nick != "mynick" {
		t.Fatalf("nick = %q", nets[0].Nick)
	}
	if nets[0].Realname != "My Real Name" {
		t.Fatalf("realname = %q", nets[0].Realname)
	}
	if len(nets[0].Servers) != 2 {
		t.Fatalf("servers len = %d, want 2", len(nets[0].Servers))
	}
	if !nets[0].Servers[0].TLSInsecure {
		t.Fatal("expected first server tls_insecure=true")
	}
	if nets[0].Servers[0].TLSMaxVersion != tls.VersionTLS13 {
		t.Fatalf("tls max version = %x, want TLS 1.3", nets[0].Servers[0].TLSMaxVersion)
	}
	if nets[0].Servers[1].Port != 6667 {
		t.Fatalf("port = %d, want 6667", nets[0].Servers[1].Port)
	}
	if nets[0].Servers[1].TLS {
		t.Fatal("expected second server tls=false")
	}
	if nets[0].User != "globaluser" {
		t.Fatalf("user = %q, want globaluser", nets[0].User)
	}
	if len(nets[0].ConnectCommands) != 2 || nets[0].ConnectCommands[0] != "PRIVMSG NickServ :IDENTIFY secret" {
		t.Fatalf("connect commands = %#v", nets[0].ConnectCommands)
	}
	if nets[1].Nick != "globalnick" || nets[1].User != "globaluser" || nets[1].Realname != "Global Real Name" {
		t.Fatalf("global inheritance failed: %+v", nets[1])
	}
}

func TestBuildServersTLSVerify(t *testing.T) {
	cases := []struct {
		name         string
		tlsVerify    bool
		wantInsecure bool
	}{
		{"verify off by default -> insecure true", false, true},
		{"verify on -> insecure false", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			servers, err := buildServers(NetworkFileConfig{
				Network:   "Ircnet",
				TLSVerify: tc.tlsVerify,
				Servers:   []ServerFileConfig{{Host: "irc.example", Port: 6697}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(servers) != 1 {
				t.Fatalf("servers len = %d, want 1", len(servers))
			}
			if servers[0].TLSInsecure != tc.wantInsecure {
				t.Fatalf("TLSInsecure = %v, want %v", servers[0].TLSInsecure, tc.wantInsecure)
			}
		})
	}
}

func TestParseYAMLConfigTLSVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
networks:
  - network: Verified
    tls_verify: true
    servers:
      - host: irc.verified.example
        port: 6697
        tls: true
  - network: Insecure
    servers:
      - host: irc.insecure.example
        port: 6697
        tls: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	nets := parsed.Networks
	if len(nets) != 2 {
		t.Fatalf("len = %d, want 2", len(nets))
	}
	if nets[0].Servers[0].TLSInsecure {
		t.Fatal("tls_verify:true should set TLSInsecure=false")
	}
	if !nets[1].Servers[0].TLSInsecure {
		t.Fatal("absent tls_verify should leave TLSInsecure=true")
	}
}

func TestParseYAMLConfigRejectsInvalidNetwork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Network with no servers is invalid; buildNetworks must return an
	// error so loadConfig's hard-quit path is exercised.
	if err := os.WriteFile(path, []byte(`
networks:
  - network: NoServers
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseYAMLConfig(path); err == nil {
		t.Fatal("expected error for network with no servers")
	}
}

// A missing config file must be fatal, same as a broken one: the backend
// never boots half-configured (a misplaced file would otherwise silently
// disable every network).
func TestParseYAMLConfigMissingFileIsError(t *testing.T) {
	if _, err := parseYAMLConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("missing config file should be an error")
	}
}

func TestBuildBlueskyConfigEnvExpansion(t *testing.T) {
	t.Setenv("LURKER_TEST_BSKY_PASS", "s3cret-app-pass")
	cfg, err := buildBlueskyConfig(BlueskyFileConfig{
		Network:     "bluesky",
		Identifier:  "alice.bsky.social",
		AppPassword: "${LURKER_TEST_BSKY_PASS}",
		Channels:    []BlueskyChannelFileConfig{{Kind: "timeline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppPassword != "s3cret-app-pass" {
		t.Fatalf("password not expanded: %q", cfg.AppPassword)
	}
	if cfg.Network != "bluesky" || cfg.Identifier != "alice.bsky.social" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Kind != "timeline" {
		t.Fatalf("channels = %+v", cfg.Channels)
	}
}

func TestBuildBlueskyConfigRequiresFields(t *testing.T) {
	cases := []struct {
		name string
		in   BlueskyFileConfig
		want string
	}{
		{"missing network", BlueskyFileConfig{Identifier: "x", AppPassword: "y"}, "network"},
		{"missing identifier", BlueskyFileConfig{Network: "bsky", AppPassword: "y"}, "identifier"},
		{"missing password", BlueskyFileConfig{Network: "bsky", Identifier: "x"}, "app_password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildBlueskyConfig(tc.in)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestBuildBlueskyConfigDefaultsToTimelineChannel(t *testing.T) {
	cfg, err := buildBlueskyConfig(BlueskyFileConfig{
		Network:     "bsky",
		Identifier:  "alice.bsky.social",
		AppPassword: "literal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Kind != "timeline" {
		t.Fatalf("default channels = %+v", cfg.Channels)
	}
}

func TestBuildBlueskyConfigRejectsReservedKinds(t *testing.T) {
	for _, kind := range []string{"search", "list", "feed", "notifications"} {
		_, err := buildBlueskyConfig(BlueskyFileConfig{
			Network:     "bsky",
			Identifier:  "alice.bsky.social",
			AppPassword: "literal",
			Channels:    []BlueskyChannelFileConfig{{Kind: kind}},
		})
		if err == nil {
			t.Fatalf("kind %q should be rejected", kind)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"first wins", []string{"a", "b"}, "a"},
		{"skips empty", []string{"", "", "c"}, "c"},
		{"all empty", []string{"", "", ""}, ""},
		{"no values", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstNonEmpty(tc.in...); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultServerPort(t *testing.T) {
	cases := []struct {
		name   string
		port   int
		useTLS bool
		want   int
	}{
		{"explicit port wins", 6697, true, 6697},
		{"zero port + tls -> 6697", 0, true, 6697},
		{"zero port + no tls -> 6667", 0, false, 6667},
		{"explicit port no tls", 6667, false, 6667},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultServerPort(tc.port, tc.useTLS); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOverrideIfDifferent(t *testing.T) {
	if got := overrideIfDifferent("same", "same"); got != "" {
		t.Fatalf("equal values should return empty, got %q", got)
	}
	if got := overrideIfDifferent("custom", "default"); got != "custom" {
		t.Fatalf("different values should return value, got %q", got)
	}
}

func TestResolveIdentity(t *testing.T) {
	cases := []struct {
		name                             string
		fc                               FileConfig
		n                                NetworkFileConfig
		wantNick, wantUser, wantRealname string
	}{
		{
			name:         "all defaults",
			wantNick:     "ircsvc",
			wantUser:     "ircsvc",
			wantRealname: "ircsvc",
		},
		{
			name:         "network overrides global",
			fc:           FileConfig{Nick: "g", User: "gu", Realname: "GR"},
			n:            NetworkFileConfig{Nick: "n", User: "nu", Realname: "NR"},
			wantNick:     "n",
			wantUser:     "nu",
			wantRealname: "NR",
		},
		{
			name:         "user falls back to nick",
			n:            NetworkFileConfig{Nick: "n"},
			wantNick:     "n",
			wantUser:     "n",
			wantRealname: "n",
		},
		{
			name:         "globals fill gaps",
			fc:           FileConfig{Nick: "g", Realname: "GR"},
			wantNick:     "g",
			wantUser:     "g",
			wantRealname: "GR",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nick, user, realname := resolveIdentity(tc.fc, tc.n)
			if nick != tc.wantNick || user != tc.wantUser || realname != tc.wantRealname {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)",
					nick, user, realname, tc.wantNick, tc.wantUser, tc.wantRealname)
			}
		})
	}
}

func TestParseTLSMaxVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{"", 0, false},
		{"1.2", tls.VersionTLS12, false},
		{"TLSv1.2", tls.VersionTLS12, false},
		{"tls12", tls.VersionTLS12, false},
		{"1.3", tls.VersionTLS13, false},
		{"tls13", tls.VersionTLS13, false},
		{"1.1", 0, true},
		{"garbage", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseTLSMaxVersion(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %x, want %x", got, tc.want)
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("LURKER_TEST_ENVOR", "from-env")
	if got := envOr("LURKER_TEST_ENVOR", "fallback"); got != "from-env" {
		t.Fatalf("got %q", got)
	}
	if got := envOr("LURKER_TEST_ENVOR_UNSET", "fallback"); got != "fallback" {
		t.Fatalf("got %q", got)
	}
}

func TestEnvBoolOr(t *testing.T) {
	cases := []struct {
		raw  string
		def  bool
		want bool
	}{
		{"true", false, true},
		{"false", true, false},
		{"1", false, true},
		{"0", true, false},
		{"garbage", true, true},
		{"garbage", false, false},
		{"", true, true},
		{"", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("LURKER_TEST_BOOL", tc.raw)
			key := "LURKER_TEST_BOOL"
			if tc.raw == "" {
				key = "LURKER_TEST_BOOL_UNSET"
			}
			if got := envBoolOr(key, tc.def); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnvDurationOr(t *testing.T) {
	t.Setenv("LURKER_TEST_DUR", "1h30m")
	if got := envDurationOr("LURKER_TEST_DUR", time.Second); got != 90*time.Minute {
		t.Fatalf("got %v", got)
	}
	t.Setenv("LURKER_TEST_DUR_BAD", "garbage")
	if got := envDurationOr("LURKER_TEST_DUR_BAD", 7*time.Second); got != 7*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := envDurationOr("LURKER_TEST_DUR_UNSET", 5*time.Second); got != 5*time.Second {
		t.Fatalf("got %v", got)
	}
}

func TestClampUpdateInterval(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{0, 24 * time.Hour},
		{-1, 24 * time.Hour},
		{10 * time.Minute, time.Hour},
		{time.Hour, time.Hour},
		{6 * time.Hour, 6 * time.Hour},
	}
	for _, tc := range cases {
		if got := clampUpdateInterval(tc.in); got != tc.want {
			t.Fatalf("clamp(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestExpandEnvSecret(t *testing.T) {
	t.Setenv("LURKER_TEST_SECRET", "hunter2")
	cases := []struct{ in, want string }{
		{"${LURKER_TEST_SECRET}", "hunter2"},
		{"literal", "literal"},
		{"$LURKER_TEST_SECRET", "$LURKER_TEST_SECRET"}, // no braces -> literal
		{"${UNSET_VAR_xyz}", ""},
		{"prefix${LURKER_TEST_SECRET}", "prefix${LURKER_TEST_SECRET}"}, // strict match only
	}
	for _, tc := range cases {
		if got := expandEnvSecret(tc.in); got != tc.want {
			t.Fatalf("expandEnvSecret(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPreviewConfigYAMLIncludesConnectCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`networks:
  - network: Libera
    channels: ["#go"]
    servers:
      - host: old.example
        port: 6667
        tls: false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, proposed, err := previewConfigYAML(path, []ircdb.Network{{
		Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester", ConnectCommands: []string{"MODE tester +x"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proposed, "connect_commands:") || !strings.Contains(proposed, "MODE tester +x") {
		t.Fatalf("proposed yaml missing connect commands:\n%s", proposed)
	}
}
