// Command seedtest populates a data directory with fake networks, buffers,
// and messages so the web UI can be exercised without a live IRC connection.
//
// Usage:
//
//	go run ./cmd/seedtest --data-dir ./data-test [--reset]
//
// Then run the backend against the same directory:
//
//	DATA_DIR=./data-test CONFIG_PATH=/dev/null go run . --web-dir ./web/dist
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	ircdb "github.com/lepinkainen/lurker/db"
)

type seedNetwork struct {
	Name     string
	Host     string
	Port     int
	TLS      bool
	Nick     string
	Realname string
	Channels []seedChannel
	Queries  []seedQuery
}

type seedChannel struct {
	Name    string
	Topic   string
	Members []string
	Lines   []seedLine
}

type seedQuery struct {
	Nick  string
	Lines []seedLine
}

type seedLine struct {
	Sender  string
	Kind    string
	Content string
	Offset  time.Duration
}

func main() {
	dataDir := flag.String("data-dir", "./data-test", "data directory to seed")
	reset := flag.Bool("reset", false, "remove the data directory before seeding")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	if *reset {
		if err := os.RemoveAll(*dataDir); err != nil {
			slog.Error("reset data dir", "err", err)
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		slog.Error("mkdir data dir", "err", err)
		os.Exit(1)
	}

	stores, err := ircdb.OpenMultiStore(*dataDir)
	if err != nil {
		slog.Error("open stores", "err", err)
		os.Exit(1)
	}
	defer func() { _ = stores.Close() }()

	ctx := context.Background()
	if err := seed(ctx, stores, fixture()); err != nil {
		slog.Error("seed", "err", err)
		os.Exit(1)
	}
	slog.Info("seed complete", "data_dir", *dataDir)
}

func seed(ctx context.Context, stores *ircdb.MultiStore, networks []seedNetwork) error {
	base := time.Now().Add(-6 * time.Hour)
	for _, n := range networks {
		net, err := stores.UpsertNetwork(ctx, ircdb.Network{
			Name: n.Name, Host: n.Host, Port: n.Port, TLS: n.TLS,
			Nick: n.Nick, Realname: n.Realname,
		})
		if err != nil {
			return fmt.Errorf("upsert network %q: %w", n.Name, err)
		}
		logStore, err := stores.LogStore(net.ID)
		if err != nil {
			return err
		}

		if _, err := ircdb.EnsureStatusBuffer(ctx, stores, net.ID); err != nil {
			return fmt.Errorf("status buffer %s: %w", n.Name, err)
		}
		if err := seedStatus(ctx, stores, logStore, net.ID, base); err != nil {
			return err
		}

		for _, c := range n.Channels {
			if err := seedChannelBuffer(ctx, stores, logStore, net.ID, c, base); err != nil {
				return fmt.Errorf("seed channel %s/%s: %w", n.Name, c.Name, err)
			}
		}
		for _, q := range n.Queries {
			if err := seedQueryBuffer(ctx, stores, logStore, net.ID, q, base); err != nil {
				return fmt.Errorf("seed query %s/%s: %w", n.Name, q.Nick, err)
			}
		}
	}
	return nil
}

func seedStatus(ctx context.Context, stores *ircdb.MultiStore, log *ircdb.LogStore, networkID int64, base time.Time) error {
	localID, err := resolveLocalBuffer(ctx, stores, log, networkID, "", ircdb.BufferStatus)
	if err != nil {
		return err
	}
	lines := []seedLine{
		{Sender: "*", Kind: "connecting", Content: "connecting to server", Offset: 0},
		{Sender: "*", Kind: "connected", Content: "connected", Offset: 2 * time.Second},
		{Sender: "*", Kind: "notice", Content: "*** Looking up your hostname...", Offset: 3 * time.Second},
		{Sender: "*", Kind: "notice", Content: "*** Welcome to the network", Offset: 4 * time.Second},
	}
	return insertLines(ctx, log, localID, lines, base)
}

func seedChannelBuffer(ctx context.Context, stores *ircdb.MultiStore, log *ircdb.LogStore, networkID int64, c seedChannel, base time.Time) error {
	localID, err := resolveLocalBuffer(ctx, stores, log, networkID, c.Name, ircdb.BufferChannel)
	if err != nil {
		return err
	}
	if c.Topic != "" {
		if err := ircdb.UpdateLogBufferTopic(ctx, log.DB, c.Name, c.Topic); err != nil {
			return err
		}
	}
	return insertLines(ctx, log, localID, c.Lines, base)
}

func seedQueryBuffer(ctx context.Context, stores *ircdb.MultiStore, log *ircdb.LogStore, networkID int64, q seedQuery, base time.Time) error {
	localID, err := resolveLocalBuffer(ctx, stores, log, networkID, q.Nick, ircdb.BufferQuery)
	if err != nil {
		return err
	}
	return insertLines(ctx, log, localID, q.Lines, base)
}

func resolveLocalBuffer(ctx context.Context, stores *ircdb.MultiStore, log *ircdb.LogStore, networkID int64, name, kind string) (localID int64, err error) {
	if _, _, _, err = stores.UpsertBufferRegistry(ctx, networkID, name, kind); err != nil {
		return 0, err
	}
	localID, _, _, err = ircdb.UpsertLogBuffer(ctx, log.DB, networkID, name, kind)
	return localID, err
}

func insertLines(ctx context.Context, log *ircdb.LogStore, bufferID int64, lines []seedLine, base time.Time) error {
	for i, l := range lines {
		ts := base.Add(l.Offset + time.Duration(i)*time.Millisecond)
		_, _, _, err := ircdb.InsertLogMessage(ctx, log.DB, ircdb.LogMessageInput{
			BufferID:  bufferID,
			Timestamp: ts,
			Sender:    l.Sender,
			Kind:      l.Kind,
			Content:   l.Content,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func fixture() []seedNetwork {
	return []seedNetwork{
		{
			Name: "libera", Host: "irc.libera.chat", Port: 6697, TLS: true,
			Nick: "lurkertest", Realname: "Lurker Test",
			Channels: []seedChannel{
				{
					Name:    "#lurker",
					Topic:   "Lurker dev channel — test fixtures loaded",
					Members: []string{"alice", "bob", "carol", "lurkertest"},
					Lines: []seedLine{
						{Sender: "alice", Kind: "privmsg", Content: "morning folks", Offset: 1 * time.Hour},
						{Sender: "bob", Kind: "privmsg", Content: "hey alice", Offset: 1*time.Hour + 30*time.Second},
						{Sender: "carol", Kind: "action", Content: "waves", Offset: 1*time.Hour + 2*time.Minute},
						{Sender: "alice", Kind: "privmsg", Content: "anyone seen the latest PR?", Offset: 1*time.Hour + 5*time.Minute},
						{Sender: "bob", Kind: "privmsg", Content: "looking now", Offset: 1*time.Hour + 6*time.Minute},
						{Sender: "lurkertest", Kind: "privmsg", Content: "LGTM from me", Offset: 1*time.Hour + 10*time.Minute},
						{Sender: "carol", Kind: "privmsg", Content: "same, merging", Offset: 1*time.Hour + 12*time.Minute},
					},
				},
				{
					Name:    "#go-nuts",
					Topic:   "Go programming — https://go.dev",
					Members: []string{"gopher1", "gopher2", "lurkertest"},
					Lines: []seedLine{
						{Sender: "gopher1", Kind: "privmsg", Content: "anyone using generics for sql scanning yet?", Offset: 2 * time.Hour},
						{Sender: "gopher2", Kind: "privmsg", Content: "yeah, works great with sqlc", Offset: 2*time.Hour + 45*time.Second},
						{Sender: "gopher1", Kind: "privmsg", Content: "nice, will try it out", Offset: 2*time.Hour + 2*time.Minute},
					},
				},
			},
			Queries: []seedQuery{
				{
					Nick: "alice",
					Lines: []seedLine{
						{Sender: "alice", Kind: "privmsg", Content: "got a minute?", Offset: 3 * time.Hour},
						{Sender: "lurkertest", Kind: "privmsg", Content: "sure, what's up", Offset: 3*time.Hour + 20*time.Second},
						{Sender: "alice", Kind: "privmsg", Content: "pair on that auth bug later?", Offset: 3*time.Hour + 1*time.Minute},
					},
				},
			},
		},
		{
			Name: "oftc", Host: "irc.oftc.net", Port: 6697, TLS: true,
			Nick: "lurkertest", Realname: "Lurker Test",
			Channels: []seedChannel{
				{
					Name:    "#debian",
					Topic:   "Debian user support",
					Members: []string{"deb1", "deb2", "lurkertest"},
					Lines: []seedLine{
						{Sender: "deb1", Kind: "privmsg", Content: "apt is hanging on trixie", Offset: 4 * time.Hour},
						{Sender: "deb2", Kind: "privmsg", Content: "try --fix-missing", Offset: 4*time.Hour + 30*time.Second},
					},
				},
			},
		},
	}
}
