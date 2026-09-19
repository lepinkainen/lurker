// Command irctestclient keeps a real IRC user connected to the test Ergo
// server. POST /message queues a message to its channel; GET /ready only
// becomes available after registration and the channel's end-of-NAMES.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lrstanley/girc"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:16667", "Ergo IRC address")
	control := flag.String("control", "127.0.0.1:16668", "HTTP control address")
	channel := flag.String("channel", "#verify", "channel to join and send to")
	nick := flag.String("nick", "bob", "IRC nickname")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *addr, *control, *channel, *nick); err != nil {
		log.Fatal(err)
	}
}

func connectSender(ctx context.Context, addr, channel, nick string) (*girc.Client, <-chan error, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, nil, err
	}
	client := girc.New(girc.Config{
		Server: host, Port: port, Nick: nick, User: nick, Name: "Lurker test sender",
		AllowFlood: true,
	})
	ready := make(chan struct{})
	var once sync.Once
	client.Handlers.Add(girc.CONNECTED, func(c *girc.Client, _ girc.Event) { c.Cmd.Join(channel) })
	client.Handlers.Add(girc.RPL_ENDOFNAMES, func(_ *girc.Client, e girc.Event) {
		// The server echoes its own canonical casing here, and channel names
		// are case-insensitive, so a strict == would never match callers
		// that pass a differently-cased -channel.
		if len(e.Params) > 1 && strings.EqualFold(e.Params[1], channel) {
			once.Do(func() { close(ready) })
		}
	})
	done := make(chan error, 1)
	go func() {
		connectErr := client.Connect()
		if connectErr == nil {
			connectErr = errors.New("IRC connection closed")
		}
		done <- connectErr
	}()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		return client, done, nil
	case err = <-done:
		err = fmt.Errorf("IRC disconnected before joining %s: %w", channel, err)
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = fmt.Errorf("timed out joining %s", channel)
	}
	client.Close()
	return nil, nil, err
}

func messageLimit(client *girc.Client, channel string) int {
	// Reserve the PRIVMSG command, target, and trailing-parameter delimiter.
	// girc splits at >= MaxEventLength(), which can also normalize whitespace,
	// so stay strictly below that threshold even for messages containing spaces.
	return max(0, client.MaxEventLength()-len("PRIVMSG "+channel+" :")-1)
}

func messageHandler(limit func() int, send func(string) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		// Keep a request to one ordinary IRC message, without girc splitting it.
		maxBytes := limit()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(maxBytes)))
		if err != nil || len(body) == 0 || strings.ContainsAny(string(body), "\r\n\x00") {
			http.Error(w, fmt.Sprintf("expected one nonempty message, at most %d bytes", maxBytes), http.StatusBadRequest)
			return
		}
		if err := send(string(body)); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	return mux
}

func run(ctx context.Context, addr, control, channel, nick string) error {
	client, disconnected, err := connectSender(ctx, addr, channel, nick)
	if err != nil {
		return err
	}
	defer client.Close()
	server := &http.Server{
		Addr: control, ReadHeaderTimeout: 5 * time.Second,
		Handler: messageHandler(func() int { return messageLimit(client, channel) }, func(message string) error {
			// Cmd.Message drops the write silently when the link is down;
			// without this the caller gets a 202 for a message nobody sends.
			if !client.IsConnected() {
				return errors.New("IRC connection is down")
			}
			client.Cmd.Message(channel, message)
			return nil
		}),
	}
	defer func() { _ = server.Close() }()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	log.Printf("%s joined %s on %s; HTTP control on %s", nick, channel, addr, control)
	select {
	case <-ctx.Done():
		return nil
	case err = <-disconnected:
		return fmt.Errorf("IRC disconnected: %w", err)
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
