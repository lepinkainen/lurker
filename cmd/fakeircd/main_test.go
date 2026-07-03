package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// startTestServer runs the fake ircd on ephemeral ports and returns dialed
// IRC + control connections.
func startTestServer(t *testing.T) (irc, ctl net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{}
	go s.run(ln, ctlLn)
	t.Cleanup(func() {
		_ = ln.Close()
		_ = ctlLn.Close()
	})

	irc, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = irc.Close() })
	ctl, err = net.Dial("tcp", ctlLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctl.Close() })
	return irc, ctl
}

// readLineContaining reads lines until one contains want, or times out.
func readLineContaining(t *testing.T, r *bufio.Reader, c net.Conn, want string) string {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if strings.Contains(line, want) {
			return strings.TrimSpace(line)
		}
	}
}

// Regression: an injection's write deadline used to persist on the conn, so
// once it expired every later write — including PONG replies — failed and
// girc ping-timed-out the connection. PING must still be answered after an
// injection plus a wait past the deadline.
func TestPongStillAnsweredAfterInjectionDeadlineExpires(t *testing.T) {
	old := injectWriteDeadline
	injectWriteDeadline = 50 * time.Millisecond
	t.Cleanup(func() { injectWriteDeadline = old })

	irc, ctl := startTestServer(t)
	r := bufio.NewReader(irc)

	fmt.Fprintf(ctl, "#verify :first injection\r\n")
	readLineContaining(t, r, irc, "PRIVMSG #verify :first injection")

	// Let the injection's write deadline expire, then require a PONG.
	time.Sleep(3 * injectWriteDeadline)
	fmt.Fprintf(irc, "PING 12345\r\n")
	pong := readLineContaining(t, r, irc, "PONG")
	if !strings.Contains(pong, "12345") {
		t.Errorf("PONG did not echo the PING id: %q", pong)
	}

	// The conn must also still be injectable.
	fmt.Fprintf(ctl, "#verify :second injection\r\n")
	readLineContaining(t, r, irc, "PRIVMSG #verify :second injection")
}
