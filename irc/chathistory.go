package irc

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	ircdb "github.com/lepinkainen/lurker/db"
	"github.com/lrstanley/girc"
)

// draft/chathistory gap backfill.
//
// On reconnect the bouncer asks the server to replay messages missed while
// disconnected: CHATHISTORY AFTER <target> timestamp=<newest stored ts>.
// Replayed messages arrive inside a `chathistory` batch; they are persisted
// with Backfill IDs (id order = message time, not insert time) and NOT
// published as live messages — a single history_backfill event per batch
// tells clients the buffer changed. Duplicates are dropped by the
// (buffer_id, msgid) unique index.
//
// Channels are requested on self-JOIN (servers may refuse history for
// channels you are not in, and a rejoin after a kick has its own gap).
// Query buffers are requested once per connection, right after CONNECTED.

const (
	// chathistoryDefaultLimit is used when the server does not advertise a
	// CHATHISTORY ISUPPORT token (0 there means "no limit").
	chathistoryDefaultLimit = 100
	// chathistoryMaxLimit caps how many messages we request per page even
	// when the server allows more.
	chathistoryMaxLimit = 500
	// chathistoryMaxPages bounds pagination per target per connection so a
	// misbehaving server can't make us loop forever.
	chathistoryMaxPages = 5
)

// chathistoryBatch is one in-flight `chathistory` batch keyed by batch ref.
type chathistoryBatch struct {
	target   string
	bufferID uuid.UUID // uuid.Nil until the first message is stored
	received int       // messages seen in the batch (including dedupe drops)
	inserted int       // messages actually written
	// newestTS/newestMsgID anchor the next page. The msgid is preferred:
	// AFTER timestamp=X excludes *every* message at X, so a page boundary
	// inside a same-millisecond cluster would silently drop the rest of the
	// cluster. newestMsgID is empty when the newest message carried no msgid.
	newestTS    string
	newestMsgID string
}

// chathistoryState is per-connection bookkeeping (the handler is rebuilt for
// every connection attempt, so this resets naturally on reconnect).
type chathistoryState struct {
	mu      sync.Mutex
	batches map[string]*chathistoryBatch
	pages   map[string]int // pages requested per target
}

func (h *handler) chathistorySupported() bool {
	return h.hasCap != nil && h.sendRaw != nil && h.hasCap("draft/chathistory")
}

func (h *handler) chathistoryEnsureState() *chathistoryState {
	h.chathistoryMu.Lock()
	defer h.chathistoryMu.Unlock()
	if h.chathistory == nil {
		h.chathistory = &chathistoryState{
			batches: map[string]*chathistoryBatch{},
			pages:   map[string]int{},
		}
	}
	return h.chathistory
}

// chathistoryLimit resolves the per-request message limit from the server's
// CHATHISTORY ISUPPORT token.
func (h *handler) chathistoryLimit() int {
	limit := 0
	if h.historyLimit != nil {
		limit = h.historyLimit()
	}
	if limit <= 0 || limit > chathistoryMaxLimit {
		if limit == 0 {
			return chathistoryDefaultLimit
		}
		return chathistoryMaxLimit
	}
	return limit
}

// maybeRequestChathistory requests the missed-message gap for one buffer.
// No-op when the cap is missing, the buffer doesn't exist yet, or it has no
// stored messages to anchor the request on.
func (h *handler) maybeRequestChathistory(target string) {
	if !h.chathistorySupported() {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	bufID, found, err := ircdb.LookupLogBufferIDByName(ctx, h.db, target)
	if err != nil || !found {
		return
	}
	lastTS, err := ircdb.LatestLogMessageTS(ctx, h.db, bufID)
	if err != nil || lastTS == "" {
		return
	}
	// Timestamp anchor for the initial request: the newest stored row may be
	// weeks old, and an expired msgid makes servers return nothing, while a
	// timestamp still selects whatever window the server retains.
	h.requestChathistoryAfter(target, "timestamp="+lastTS)
}

// requestQueryBackfills requests the gap for every query buffer. Channels
// are handled per self-JOIN instead; the status buffer is local-only.
func (h *handler) requestQueryBackfills() {
	if !h.chathistorySupported() {
		return
	}
	ctx, cancel := h.eventContext()
	defer cancel()
	bufs, err := ircdb.ListLogBuffers(ctx, h.db)
	if err != nil {
		slog.Warn("list buffers for chathistory", "err", err, "network", h.networkName)
		return
	}
	for _, b := range bufs {
		if b.Kind == ircdb.BufferQuery {
			h.maybeRequestChathistory(b.Name)
		}
	}
}

// requestChathistoryAfter sends one CHATHISTORY AFTER page. selector is a
// spec selector: "timestamp=<ts>" or "msgid=<id>".
func (h *handler) requestChathistoryAfter(target, selector string) {
	st := h.chathistoryEnsureState()
	st.mu.Lock()
	pages := st.pages[target]
	if pages >= chathistoryMaxPages {
		st.mu.Unlock()
		return
	}
	st.pages[target] = pages + 1
	st.mu.Unlock()

	line := fmt.Sprintf("CHATHISTORY AFTER %s %s %d", target, selector, h.chathistoryLimit())
	if err := h.sendRaw(line); err != nil {
		slog.Warn("send chathistory request", "err", err, "network", h.networkName, "target", target)
	}
}

// onBatch tracks chathistory batch lifecycles. Other batch types pass
// through untouched (their inner events keep flowing the normal live path).
func (h *handler) onBatch(_ *girc.Client, e girc.Event) {
	if len(e.Params) == 0 {
		return
	}
	ref := e.Params[0]
	switch {
	case strings.HasPrefix(ref, "+"):
		if len(e.Params) >= 3 && strings.EqualFold(e.Params[1], "chathistory") {
			st := h.chathistoryEnsureState()
			st.mu.Lock()
			st.batches[ref[1:]] = &chathistoryBatch{target: e.Params[2]}
			st.mu.Unlock()
		}
	case strings.HasPrefix(ref, "-"):
		h.finishChathistoryBatch(ref[1:])
	}
}

// chathistoryDivert reports whether e belongs to an open chathistory batch,
// returning the batch ref and its target (the authoritative buffer name —
// for query history it names the peer even for our own replayed messages,
// which a Source-based guess would misfile).
func (h *handler) chathistoryDivert(e girc.Event) (ref, target string, ok bool) {
	ref, ok = e.Tags.Get("batch")
	if !ok || ref == "" {
		return "", "", false
	}
	h.chathistoryMu.Lock()
	st := h.chathistory
	h.chathistoryMu.Unlock()
	if st == nil {
		return "", "", false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	b, found := st.batches[ref]
	if !found {
		return "", "", false
	}
	return ref, b.target, true
}

// storeBackfillMessage persists one replayed message. Mirrors storeEvent's
// extraction but: the buffer comes from the batch target, the row gets a
// Backfill ID, and no per-message hub event goes out (a batch-level
// history_backfill event is published instead — replayed messages are old
// and must not render as live traffic or feed the netsplit tracker).
func (h *handler) storeBackfillMessage(e girc.Event, batchRef, target string) {
	bufKind := ircdb.BufferQuery
	if girc.IsValidChannel(target) {
		bufKind = ircdb.BufferChannel
	}
	kind, content := privmsgKindContent(e)

	sender := ""
	userhost := ""
	if e.Source != nil {
		sender = e.Source.Name
		if e.Source.Ident != "" {
			userhost = e.Source.Ident + "@" + e.Source.Host
		}
	}
	msgID, _ := e.Tags.Get("msgid")
	account, _ := e.Tags.Get("account")
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Batch bookkeeping counts every replayed message — including ones we
	// don't persist (ignored senders, dedupes). Pagination compares received
	// against the server's page size, so skipping the count here would make
	// a full page look partial and strand the rest of the gap.
	st := h.chathistoryEnsureState()
	st.mu.Lock()
	if b, ok := st.batches[batchRef]; ok {
		b.received++
		if tsStr := ircdb.FormatTime(ts); tsStr >= b.newestTS {
			b.newestTS = tsStr
			b.newestMsgID = msgID
		}
	}
	st.mu.Unlock()

	if sender != "" && sender != "*" && h.isIgnored(sender) {
		return
	}

	ctx, cancel := h.eventContext()
	defer cancel()
	bufID, err := h.ensureBuffer(ctx, target, bufKind)
	if err != nil {
		slog.Error("ensure backfill buffer", "err", err, "network", h.networkName, "buffer", target)
		return
	}
	id, _, inserted, err := ircdb.InsertLogMessage(ctx, h.db, ircdb.LogMessageInput{
		BufferID:  bufID,
		MsgID:     msgID,
		Timestamp: ts,
		Sender:    sender,
		Userhost:  userhost,
		Account:   account,
		Kind:      kind,
		Content:   ensureUTF8(content),
		Raw:       e.String(),
		Backfill:  true,
	})
	if err != nil {
		slog.Error("insert backfill message", "err", err, "network", h.networkName, "kind", kind)
		return
	}

	st.mu.Lock()
	if b, ok := st.batches[batchRef]; ok && inserted {
		b.inserted++
		if b.bufferID == uuid.Nil {
			b.bufferID = bufID
		}
	}
	st.mu.Unlock()

	if inserted {
		h.enqueuePreviews(id, bufID, bufKind, kind, content)
	}
}

// finishChathistoryBatch closes a batch: announces the backfill to clients
// and paginates when the server returned a full page (more gap may remain).
func (h *handler) finishChathistoryBatch(ref string) {
	h.chathistoryMu.Lock()
	st := h.chathistory
	h.chathistoryMu.Unlock()
	if st == nil {
		return
	}
	st.mu.Lock()
	b, ok := st.batches[ref]
	delete(st.batches, ref)
	st.mu.Unlock()
	if !ok {
		return
	}

	if b.inserted > 0 && b.bufferID != uuid.Nil && h.hub != nil {
		h.hub.Publish(&HistoryBackfillEvent{
			Type:      "history_backfill",
			NetworkID: h.networkID,
			BufferID:  b.bufferID,
			Count:     b.inserted,
		})
	}
	if b.received >= h.chathistoryLimit() {
		switch {
		case b.newestMsgID != "":
			// msgid selector is exact: resumes inside a same-timestamp
			// cluster without re-fetching or skipping anything. The id is
			// fresh (the server just sent it), so expiry isn't a concern.
			h.requestChathistoryAfter(b.target, "msgid="+b.newestMsgID)
		case b.newestTS != "":
			h.requestChathistoryAfter(b.target, "timestamp="+b.newestTS)
		}
	}
}

// privmsgKindContent derives the stored kind and content of a PRIVMSG/NOTICE
// event (action and non-action CTCPs included). Shared by the live and
// backfill paths.
func privmsgKindContent(e girc.Event) (kind, content string) {
	kind = "privmsg"
	if e.Command == girc.NOTICE {
		kind = "notice"
	}
	content = e.Last()
	if e.IsAction() {
		kind = "action"
		content = e.StripAction()
	} else if ok, ctcp := e.IsCTCP(); ok && ctcp.Command != girc.CTCP_ACTION {
		kind = "ctcp"
		if ctcp.Text != "" {
			content = ctcp.Command + " " + ctcp.Text
		} else {
			content = ctcp.Command
		}
	}
	return kind, content
}
