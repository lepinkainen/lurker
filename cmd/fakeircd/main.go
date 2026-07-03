// Command fakeircd is a minimal IRC server for local testing: it completes
// the girc handshake, echoes JOINs, and injects PRIVMSGs from a fake user
// when a control line of the form "#channel :message" arrives on the
// control port (e.g. echo "#verify :hi" | nc 127.0.0.1 6668).
//
// It exists so client verification can exercise live-message behaviors
// (unread badges, the new-messages marker) without a real IRC network:
// point a lurker network at 127.0.0.1:6667 with TLS off, and every
// connected lurker backend receives whatever the control port injects.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type server struct {
	mu    sync.Mutex
	conns []net.Conn
}

// injectWriteDeadline bounds broadcast writes so one stalled client can't
// wedge injections. Var so tests can shrink it.
var injectWriteDeadline = 5 * time.Second

func main() {
	addr := flag.String("addr", "127.0.0.1:6667", "IRC listen address")
	ctlAddr := flag.String("ctl", "127.0.0.1:6668", "control listen address for message injection")
	flag.Parse()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	ctl, err := lc.Listen(context.Background(), "tcp", *ctlAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fakeircd on %s, control on %s", *addr, *ctlAddr)

	s := &server{}
	s.run(ln, ctl)
}

// run accepts IRC clients on ln and control connections on ctl until ln
// closes.
func (s *server) run(ln, ctl net.Listener) {
	go s.acceptControl(ctl)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		go func() {
			s.serve(conn)
			s.remove(conn)
		}()
	}
}

// remove drops a finished connection so injections stop writing to it.
func (s *server) remove(c net.Conn) {
	_ = c.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ic := range s.conns {
		if ic == c {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			return
		}
	}
}

// acceptControl reads "#channel :message" lines and broadcasts them as
// PRIVMSGs from the fake user bob to every connected IRC client.
func (s *server) acceptControl(ctl net.Listener) {
	for {
		c, err := ctl.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = c.Close() }()
			in := bufio.NewScanner(c)
			for in.Scan() {
				parts := strings.SplitN(in.Text(), " :", 2)
				if len(parts) != 2 {
					continue
				}
				msg := fmt.Sprintf(":bob!bob@fake.host PRIVMSG %s :%s\r\n", parts[0], parts[1])
				// Copy under the lock, write outside it: one stalled client
				// must not wedge the accept loop or other injections.
				s.mu.Lock()
				targets := append([]net.Conn(nil), s.conns...)
				s.mu.Unlock()
				for _, ic := range targets {
					_ = ic.SetWriteDeadline(time.Now().Add(injectWriteDeadline))
					_, _ = ic.Write([]byte(msg))
					// Clear the deadline: it persists on the conn, and an
					// expired one fails every later write — including serve()'s
					// PONG replies, which makes girc ping-timeout the link.
					_ = ic.SetWriteDeadline(time.Time{})
				}
				log.Printf("injected to %d client(s): %s", len(targets), strings.TrimSpace(msg))
			}
		}()
	}
}

func (s *server) serve(c net.Conn) {
	log.Printf("client connected: %s", c.RemoteAddr())
	nick := "unknown"
	w := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...) + "\r\n"
		_, _ = c.Write([]byte(line))
		log.Printf("-> %s", strings.TrimSpace(line))
	}
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		line := sc.Text()
		log.Printf("<- %s", line)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "CAP":
			if len(fields) > 1 && strings.EqualFold(fields[1], "LS") {
				w(":testsrv CAP * LS :")
			}
		case "NICK":
			if len(fields) > 1 {
				nick = fields[1]
			}
		case "USER":
			w(":testsrv 001 %s :Welcome to fakeircd", nick)
			w(":testsrv 002 %s :Your host is testsrv", nick)
			w(":testsrv 003 %s :This server was created now", nick)
			w(":testsrv 004 %s testsrv fake iowghraAsORTVSxNCWqBzvdHtGpI lvhopsmntikrRcaqOALQbSeIKVfMCuzNTGjZ", nick)
			w(":testsrv 375 %s :- MOTD -", nick)
			w(":testsrv 376 %s :End of MOTD", nick)
		case "PING":
			arg := ""
			if len(fields) > 1 {
				arg = fields[1]
			}
			w("PONG testsrv %s", arg)
		case "JOIN":
			if len(fields) > 1 {
				ch := fields[1]
				w(":%s!%s@local JOIN %s", nick, nick, ch)
				w(":testsrv 353 %s = %s :%s bob", nick, ch, nick)
				w(":testsrv 366 %s %s :End of NAMES", nick, ch)
			}
		case "QUIT":
			return
		}
	}
	log.Printf("client gone: %s", c.RemoteAddr())
}
