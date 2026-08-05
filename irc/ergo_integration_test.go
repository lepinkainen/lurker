//go:build ergo

package irc

// Integration test against a real Ergo IRCv3 server. Run via `task test-ergo`,
// which starts the server from testdata/ergo/ircd.yaml in docker and sets
// ERGO_ADDR. Exercises the seams unit tests can't: real CAP negotiation
// (draft/chathistory ACK -> HasCapability), a real CHATHISTORY request, and a
// real batch replay through girc's parser.

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
)

func ergoAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("ERGO_ADDR")
	if addr == "" {
		addr = "127.0.0.1:16667"
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("no Ergo server at %s (run via `task test-ergo`): %v", addr, err)
	}
	_ = conn.Close()
	return addr
}

// rawClient is a minimal scripted IRC client (the "other user" in tests).
type rawClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
	nick string
}

func dialRaw(t *testing.T, addr, nick string) *rawClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	rc := &rawClient{t: t, conn: conn, r: bufio.NewReader(conn), nick: nick}
	t.Cleanup(func() { _ = conn.Close() })
	rc.send("NICK %s", nick)
	rc.send("USER %s 0 * :%s", nick, nick)
	rc.waitFor(" 001 ", 10*time.Second)
	return rc
}

func (rc *rawClient) send(format string, a ...any) {
	rc.t.Helper()
	if _, err := fmt.Fprintf(rc.conn, format+"\r\n", a...); err != nil {
		rc.t.Fatalf("%s: send: %v", rc.nick, err)
	}
}

// waitFor reads lines (answering PINGs) until one contains substr.
func (rc *rawClient) waitFor(substr string, timeout time.Duration) string {
	rc.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if err := rc.conn.SetReadDeadline(deadline); err != nil {
			rc.t.Fatalf("%s: set deadline: %v", rc.nick, err)
		}
		line, err := rc.r.ReadString('\n')
		if err != nil {
			rc.t.Fatalf("%s: waiting for %q: %v", rc.nick, substr, err)
		}
		if strings.HasPrefix(line, "PING") {
			rc.send("PONG %s", strings.TrimSpace(strings.TrimPrefix(line, "PING")))
			continue
		}
		if strings.Contains(line, substr) {
			return line
		}
	}
}

// privmsgRows returns the buffer's stored privmsgs from sender, in id order.
// Filtering by sender skips Ergo's HistServ lines: without event-playback,
// replayed join/quit events arrive as PRIVMSGs from "HistServ" and are
// legitimately stored alongside the real messages.
func privmsgRows(t *testing.T, logStore *ircdb.LogStore, buffer, sender string) []ircdb.LogMessageRow {
	t.Helper()
	ctx := t.Context()
	bufID, found, err := ircdb.LookupLogBufferIDByName(ctx, logStore.DB, buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return nil
	}
	rows, err := ircdb.RecentLogMessages(ctx, logStore.DB, bufID, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := rows[:0]
	for _, r := range rows {
		if r.Kind == "privmsg" && r.Sender == sender {
			out = append(out, r)
		}
	}
	return out
}

func waitForPrivmsgCount(t *testing.T, logStore *ircdb.LogStore, buffer, sender string, want int, timeout time.Duration) []ircdb.LogMessageRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		rows := privmsgRows(t, logStore, buffer, sender)
		if len(rows) >= want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d privmsgs in %s, have %d: %+v", want, buffer, len(rows), rows)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForMember polls the manager's member snapshot until the nick appears
// with the wanted bot flag.
func waitForMember(t *testing.T, m *Manager, networkID uuid.UUID, channel, nick string, wantBot bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var last *ChannelUser
		for _, member := range m.ChannelMembers(networkID, channel) {
			if strings.EqualFold(member.Nick, nick) {
				mem := member
				last = &mem
				break
			}
		}
		if last != nil && last.Bot == wantBot {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s bot=%v in %s, last seen: %+v", nick, wantBot, channel, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestErgoBotModeDetection covers IRCv3 bot mode
// (https://ircv3.net/specs/extensions/bot-mode) against Ergo's real
// implementation (user mode +B, ISUPPORT BOT=B, WHO/WHOX flags): a bot
// already present when lurker joins (channel WHOX path) and a bot joining
// afterwards (per-nick WHOX path) must both get member Bot=true; a plain
// user must not.
func TestErgoBotModeDetection(t *testing.T) {
	addr := ergoAddr(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}

	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	channel := fmt.Sprintf("#bots-%d", time.Now().UnixNano()%1_000_000_000)
	ctx := t.Context()

	// robo is a bot (+B) sitting in the channel before lurker arrives.
	robo := dialRaw(t, addr, "robo")
	robo.send("MODE robo +B")
	robo.waitFor("MODE", 10*time.Second)
	robo.send("JOIN %s", channel)
	robo.waitFor(" 366 ", 10*time.Second)

	// alice is a plain user, also present before lurker.
	alice := dialRaw(t, addr, "alice")
	alice.send("JOIN %s", channel)
	alice.waitFor(" 366 ", 10*time.Second)

	nc := NetworkConfig{
		Name:     "ergobots",
		Servers:  []ServerConfig{{Host: host, Port: port}},
		Nick:     "lurker",
		Channels: []string{channel},
	}
	nrow, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: nc.Name, Host: host, Port: port, Nick: nc.Nick})
	if err != nil {
		t.Fatal(err)
	}

	m := NewManager(ctx, stores, hub.New())
	if err := m.StartNetwork(nrow.ID, nc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.StopNetwork(nrow.ID)
		m.Wait()
	})

	// Channel WHOX path: flags for members present at our join.
	waitForMember(t, m, nrow.ID, channel, "robo", true, 20*time.Second)
	waitForMember(t, m, nrow.ID, channel, "alice", false, 20*time.Second)

	// Per-nick WHOX path: a bot joining after us.
	robo2 := dialRaw(t, addr, "robo2")
	robo2.send("MODE robo2 +B")
	robo2.waitFor("MODE", 10*time.Second)
	robo2.send("JOIN %s", channel)
	robo2.waitFor(" 366 ", 10*time.Second)
	waitForMember(t, m, nrow.ID, channel, "robo2", true, 20*time.Second)
}

func TestErgoChathistoryBackfill(t *testing.T) {
	addr := ergoAddr(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}

	stores, err := ircdb.OpenMultiStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	// Unique channel per run: the Ergo container keeps in-memory history
	// for the lifetime of the process, so reruns must not see old messages.
	channel := fmt.Sprintf("#backfill-%d", time.Now().UnixNano()%1_000_000_000)

	ctx := t.Context()
	nc := NetworkConfig{
		Name:     "ergotest",
		Servers:  []ServerConfig{{Host: host, Port: port}},
		Nick:     "lurker",
		Channels: []string{channel},
	}
	nrow, err := stores.UpsertNetwork(ctx, ircdb.Network{Name: nc.Name, Host: host, Port: port, Nick: nc.Nick})
	if err != nil {
		t.Fatal(err)
	}
	logStore, err := stores.LogStore(nrow.ID)
	if err != nil {
		t.Fatal(err)
	}

	m := NewManager(ctx, stores, hub.New())
	if err := m.StartNetwork(nrow.ID, nc); err != nil {
		t.Fatal(err)
	}
	// Wait for the runtime to fully exit before the stores close (cleanups
	// run LIFO), or late girc handlers log "database is closed" noise.
	t.Cleanup(func() {
		_ = m.StopNetwork(nrow.ID)
		m.Wait()
	})

	// Alice joins the same channel and talks while lurker is connected.
	alice := dialRaw(t, addr, "alice")
	alice.send("JOIN %s", channel)
	alice.waitFor(" 366 ", 10*time.Second) // end of NAMES
	// Wait until lurker is in the channel from the server's perspective:
	// alice sees its JOIN (Ergo relays joins to channel members).
	alice.waitFor("JOIN", 15*time.Second)

	alice.send("PRIVMSG %s :live message one", channel)
	waitForPrivmsgCount(t, logStore, channel, "alice", 1, 15*time.Second)

	// Disconnect lurker; wait until the server has actually dropped it so
	// the next messages are genuinely missed, not delivered live.
	if err := m.StopNetwork(nrow.ID); err != nil {
		t.Fatal(err)
	}
	alice.waitFor("QUIT", 15*time.Second)

	alice.send("PRIVMSG %s :missed message two", channel)
	alice.send("PRIVMSG %s :missed message three", channel)

	// Reconnect. Self-JOIN must trigger CHATHISTORY AFTER anchored on
	// "live message one" and backfill the two missed messages.
	if err := m.StartNetwork(nrow.ID, nc); err != nil {
		t.Fatal(err)
	}
	rows := waitForPrivmsgCount(t, logStore, channel, "alice", 3, 30*time.Second)

	// Settle briefly, then re-read: replay must not duplicate anything.
	time.Sleep(500 * time.Millisecond)
	rows = privmsgRows(t, logStore, channel, "alice")
	if len(rows) != 3 {
		t.Fatalf("expected exactly 3 privmsgs after backfill, got %d: %+v", len(rows), rows)
	}
	wantContents := []string{"live message one", "missed message two", "missed message three"}
	for i, want := range wantContents {
		if rows[i].Content != want {
			t.Errorf("row %d content = %q, want %q (order must be chronological)", i, rows[i].Content, want)
		}
		if rows[i].MsgID == "" {
			t.Errorf("row %d (%q) has no msgid", i, rows[i].Content)
		}
		if rows[i].Sender != "alice" {
			t.Errorf("row %d sender = %q, want alice", i, rows[i].Sender)
		}
	}
}
