//go:build integration

package irc

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/hub"
)

// fakeIRCServer is a minimal ircd that accepts one client, completes the
// USER/NICK handshake, and then lets tests drive the conversation by
// calling Send. The advertised caps are controlled by the `caps` field.
type fakeIRCServer struct {
	t     *testing.T
	ln    net.Listener
	conn  net.Conn
	rd    *bufio.Reader
	wr    *bufio.Writer
	ready chan struct{}
	caps  string // space-separated caps to advertise in CAP LS
}

func newFakeIRC(t *testing.T) *fakeIRCServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeIRCServer{t: t, ln: ln, ready: make(chan struct{})}
	go s.loop()
	return s
}

func newFakeIRCTLSWithCert(t *testing.T, cert tls.Certificate) *fakeIRCServer {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeIRCServer{t: t, ln: ln, ready: make(chan struct{})}
	go s.loop()
	return s
}

func (s *fakeIRCServer) addr() (host string, port int) {
	a := s.ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func (s *fakeIRCServer) loop() {
	c, err := s.ln.Accept()
	if err != nil {
		return
	}
	s.conn = c
	s.rd = bufio.NewReader(c)
	s.wr = bufio.NewWriter(c)

	nick := ""
	welcomed := false
	for {
		line, err := s.rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "CAP LS"):
			s.send(":fake CAP * LS :%s", s.caps)
		case strings.HasPrefix(line, "CAP REQ"):
			reqIdx := strings.Index(line, ":")
			req := ""
			if reqIdx >= 0 {
				req = line[reqIdx+1:]
			}
			s.send(":fake CAP * ACK :%s", req)
		case strings.HasPrefix(line, "CAP END"):
		case strings.HasPrefix(line, "NICK "):
			nick = strings.TrimPrefix(line, "NICK ")
		case strings.HasPrefix(line, "USER "):
			if nick == "" {
				nick = "tester"
			}
			if !welcomed {
				welcomed = true
				s.send(":fake 001 %s :Welcome", nick)
				s.send(":fake 002 %s :Your host", nick)
				s.send(":fake 003 %s :This server was created", nick)
				s.send(":fake 004 %s fake 1.0 o o", nick)
				s.send(":fake 005 %s CHANTYPES=# :are supported", nick)
				s.send(":fake 376 %s :End of MOTD", nick)
				close(s.ready)
			}
		case strings.HasPrefix(line, "JOIN "):
			channel := strings.TrimPrefix(line, "JOIN ")
			s.send(":%s!~u@h JOIN %s", nick, channel)
			s.send(":fake 353 %s = %s :%s", nick, channel, nick)
			s.send(":fake 366 %s %s :End of NAMES", nick, channel)
		case strings.HasPrefix(line, "PING "):
			s.send(":fake PONG fake %s", strings.TrimPrefix(line, "PING "))
		case strings.HasPrefix(line, "QUIT"):
			return
		}
	}
}

func (s *fakeIRCServer) send(format string, args ...any) {
	if s.wr == nil {
		return
	}
	fmt.Fprintf(s.wr, format+"\r\n", args...)
	s.wr.Flush()
}

func (s *fakeIRCServer) injectPrivmsg(channel, from, text string) {
	s.send(":%s!~u@h PRIVMSG %s :%s", from, channel, text)
}

func (s *fakeIRCServer) injectTaggedPrivmsg(tags, channel, from, text string) {
	s.send("@%s :%s!~u@h PRIVMSG %s :%s", tags, from, channel, text)
}

func (s *fakeIRCServer) close() {
	s.ln.Close()
	if s.conn != nil {
		s.conn.Close()
	}
}

func TestManagerPersistsMessages(t *testing.T) {
	dir := t.TempDir()
	stores, err := db.OpenMultiStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv := newFakeIRC(t)
	defer srv.close()
	host, port := srv.addr()

	ctx := t.Context()

	mgr := NewManager(stores, nil)
	err = mgr.Start(ctx, []NetworkConfig{{
		Name: "fake",
		Servers: []ServerConfig{{
			Host: host, Port: port, TLS: false,
		}},
		Nick:     "tester",
		User:     "tester",
		Realname: "tester",
		Channels: []string{"#test"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server never saw registration")
	}
	waitFor(t, 5*time.Second, func() bool {
		logStore, _ := stores.LogStore(1)
		row := logStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='#test'`)
		var n int
		row.Scan(&n)
		return n == 1
	}, "join never landed")

	srv.injectPrivmsg("#test", "alice", "hello from fake")

	waitFor(t, 5*time.Second, func() bool {
		var n int
		logStore, _ := stores.LogStore(1)
		logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE kind='privmsg' AND content='hello from fake'`).Scan(&n)
		return n == 1
	}, "privmsg never persisted")

	var hit string
	logStore, _ := stores.LogStore(1)
	err = logStore.DB.QueryRow(`SELECT content FROM messages_fts WHERE messages_fts MATCH 'fake' LIMIT 1`).Scan(&hit)
	if err != nil || !strings.Contains(hit, "hello from fake") {
		t.Fatalf("fts hit = %q, err = %v", hit, err)
	}

	mgr.Wait()
}

func TestMsgIDDedupAndServerTime(t *testing.T) {
	dir := t.TempDir()
	stores, err := db.OpenMultiStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv := newFakeIRC(t)
	srv.caps = "message-tags server-time msgid batch"
	defer srv.close()
	host, port := srv.addr()

	ctx := t.Context()

	mgr := NewManager(stores, nil)
	err = mgr.Start(ctx, []NetworkConfig{{
		Name: "fake",
		Servers: []ServerConfig{{
			Host: host, Port: port, TLS: false,
		}},
		Nick: "tester", User: "tester", Realname: "tester",
		Channels: []string{"#test"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server never saw registration")
	}
	waitFor(t, 5*time.Second, func() bool {
		var n int
		logStore, _ := stores.LogStore(1)
		logStore.DB.QueryRow(`SELECT COUNT(*) FROM buffers WHERE name='#test'`).Scan(&n)
		return n == 1
	}, "join never landed")

	const msgid = "mid-42"
	const serverTime = "2025-01-02T03:04:05.678Z"
	tags := "time=" + serverTime + ";msgid=" + msgid
	srv.injectTaggedPrivmsg(tags, "#test", "alice", "once")

	waitFor(t, 5*time.Second, func() bool {
		var n int
		logStore, _ := stores.LogStore(1)
		logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE msgid=?`, msgid).Scan(&n)
		return n == 1
	}, "tagged privmsg never landed")

	srv.injectTaggedPrivmsg(tags, "#test", "alice", "once")
	time.Sleep(250 * time.Millisecond)

	logStore, _ := stores.LogStore(1)
	var count int
	if err := logStore.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE msgid=?`, msgid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected dedup via msgid, got %d rows", count)
	}

	var gotTS string
	if err := logStore.DB.QueryRow(`SELECT ts FROM messages WHERE msgid=?`, msgid).Scan(&gotTS); err != nil {
		t.Fatal(err)
	}
	gotParsed, err := time.Parse("2006-01-02T15:04:05.000Z", gotTS)
	if err != nil {
		t.Fatalf("unparseable ts %q: %v", gotTS, err)
	}
	want, _ := time.Parse(time.RFC3339Nano, serverTime)
	if !gotParsed.Equal(want) {
		t.Fatalf("ts mismatch: got %s, want %s", gotParsed, want)
	}

	mgr.Wait()
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestChannelMembersFromNames(t *testing.T) {
	dir := t.TempDir()
	stores, err := db.OpenMultiStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	srv := newFakeIRC(t)
	defer srv.close()
	host, port := srv.addr()

	h := hub.New()
	mgr := NewManager(stores, h)
	err = mgr.Start(t.Context(), []NetworkConfig{{
		Name:    "fake",
		Servers: []ServerConfig{{Host: host, Port: port, TLS: false}},
		Nick:    "tester", User: "tester", Realname: "tester",
		Channels: []string{"#test"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server never saw registration")
	}

	waitFor(t, 5*time.Second, func() bool {
		members := mgr.ChannelMembers(1, "#test")
		return len(members) == 1 && members[0].Nick == "tester"
	}, "names never populated channel members")

	events, unsub := h.Subscribe(16)
	defer unsub()
	if err := mgr.Join(1, "#test2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		for {
			select {
			case ev := <-events:
				ml, ok := ev.(*MemberListEvent)
				if ok && ml.Channel == "#test2" && len(ml.Members) == 1 && ml.Members[0].Nick == "tester" {
					return true
				}
			default:
				return false
			}
		}
	}, "member_list event never published")

	mgr.Wait()
}

func TestTLSInsecureSkipVerify(t *testing.T) {
	cert, err := selfSignedCert("wrong.example")
	if err != nil {
		t.Fatal(err)
	}
	srv := newFakeIRCTLSWithCert(t, cert)
	defer srv.close()
	host, port := srv.addr()

	dir := t.TempDir()
	stores, err := db.OpenMultiStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	ctx := t.Context()

	mgr := NewManager(stores, nil)
	err = mgr.Start(ctx, []NetworkConfig{{
		Name: "ircnet",
		Servers: []ServerConfig{{
			Host: host, Port: port, TLS: true, TLSInsecure: true,
		}},
		Nick: "tester", User: "tester", Realname: "tester",
	}})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(250 * time.Millisecond)
	waitFor(t, 2*time.Second, func() bool {
		state := mgr.StateSnapshot()
		return state[1] == "connected"
	}, "expected insecure TLS connection to succeed")

	mgr.Wait()
}

func selfSignedCert(cn string) (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return tls.X509KeyPair(certPEM, keyPEM)
}

var _ = os.ErrNotExist
var _ = filepath.Join
