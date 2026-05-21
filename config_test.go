package main

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	nets, _, _, err := parseYAMLConfig(path)
	if err != nil {
		t.Fatal(err)
	}
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
