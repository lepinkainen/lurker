package main

import (
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

	nets, err := loadNetworksFromYAML(path)
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
