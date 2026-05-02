# Lurker feature parity plan

Features identified from thelounge that lurker lacks. Grouped by area, ordered by rough implementation value. Multi-user, auth, WEBIRC, identd, and other explicitly out-of-scope items are omitted.

---

## 1. Input UX (frontend-only, high value / low effort)

### 1.1 Input history
Arrow-up / arrow-down cycles through previously sent messages, per-buffer.  
Store a ring buffer of sent strings in component state (or `localStorage` keyed by buffer ID). No backend changes needed.

### 1.2 IRC text formatting
Keyboard shortcuts apply IRC formatting codes before the cursor:
| Shortcut | Code | Effect |
|---|---|---|
| Ctrl+B | `\x02` | Bold |
| Ctrl+I | `\x1D` | Italic |
| Ctrl+U | `\x1F` | Underline |
| Ctrl+S | `\x1E` | Strikethrough |
| Ctrl+M | `\x11` | Monospace |
| Ctrl+O | `\x0F` | Reset all |

Color picker (Ctrl+K) inserts `\x03<fg>[,<bg>]` codes. The message renderer already needs to parse and render these codes as styled spans — currently lurker renders text as plain strings.

**Backend**: none. The raw bytes pass through to IRC.  
**Frontend**: input shortcut handling + message renderer that parses mIRC format codes.

### 1.3 Emoji autocompletion
Trigger on `:` followed by two+ characters; show a popup of matching emoji names. Insert the Unicode character on selection. Similar to the existing slash-command autocomplete popup.

---

## 2. Navigation & discovery (frontend, medium effort)

### 2.1 Channel switcher (jump-to)
Global fuzzy-search overlay (e.g. Alt+J or Ctrl+K) across all networks and buffers. Filters as the user types, navigates with arrow keys, confirms with Enter. Purely frontend state from the existing buffer list.

### 2.2 Recent mentions popup
A panel (Alt+M or sidebar button) that aggregates all messages where the user's nick appears across every buffer. Requires the backend to expose a `/api/search?mention=true` endpoint, or the frontend can filter the locally-held recent message cache.

### 2.3 Keyboard shortcuts
Standard set of navigation shortcuts, stored in a help overlay (Alt+/):
| Key | Action |
|---|---|
| Alt+S | Toggle sidebar |
| Alt+U | Toggle user list |
| Alt+J | Open channel switcher |
| Alt+M | Open recent mentions |
| Alt+↑ / Alt+↓ | Previous / next buffer |
| Alt+A | Jump to first unread buffer |

### 2.4 Touch / swipe gestures
For mobile: single-finger swipe right to open sidebar, swipe left to close. Two-finger swipe left/right to navigate between buffers. Implement with `touchstart`/`touchend` handlers in the root layout component.

---

## 3. IRC commands (backend + frontend)

These require adding command handlers in `irc/handler.go` (or a dedicated command dispatcher) and wiring them through the WebSocket command path.

### 3.1 /away and /back
```
/away [message]   → sends  AWAY :<message>  to server
/back             → sends  AWAY             (no message clears away)
```
Display away status in the member list (already tracks it) and in the user's own nick indicator.

### 3.2 /quit
```
/quit [message]   → sends  QUIT :<message>, then disconnects the network
```
Currently `/part` leaves a channel; there is no graceful whole-network quit. Add a configurable default quit message to the network settings.

### 3.3 /rejoin and /cycle
```
/rejoin [channel]   → PART then immediately JOIN the same channel
```
Useful for ops needing to reset channel state. Pure IRC command, no storage changes.

### 3.4 /notice
```
/notice <target> <message>   → sends  NOTICE <target> :<message>
```
Display inbound NOTICEs distinctly from PRIVMSGs (different style in the renderer).

### 3.5 /ctcp
```
/ctcp <nick> <command> [args]   → sends  PRIVMSG <nick> :\x01<command> [args]\x01
```
Display inbound CTCP requests (VERSION, PING, etc.) as system messages.

### 3.6 /query
```
/query <nick>   → opens a PM buffer without sending any message
```
Currently a PM buffer only opens when a message is sent or received.

### 3.7 /list
```
/list [filter]   → sends  LIST [filter]  and displays results in a temporary panel
```
The server streams RPL_LIST (322) replies. Render them in a sortable table overlay (channel name, user count, topic). Allow clicking a channel to join.

### 3.8 Channel moderation commands
```
/op <nick>        → MODE <channel> +o <nick>
/deop <nick>      → MODE <channel> -o <nick>
/voice <nick>     → MODE <channel> +v <nick>
/devoice <nick>   → MODE <channel> -v <nick>
/kickban <nick>   → KICK + BAN in one command
/ban <mask>       → MODE <channel> +b <mask>
/unban <mask>     → MODE <channel> -b <mask>
/banlist          → MODE <channel> b  (request ban list, display in panel)
```
These are all thin wrappers over MODE/KICK that the frontend can construct from shorthand. No new storage required.

### 3.9 /ignore, /unignore, /ignorelist
Client-side ignore list stored per-network in the control DB (new column or table). Messages from ignored nicks are suppressed in the renderer. No IRC protocol interaction needed.

---

## 4. Mention & notification features

### 4.1 Custom highlight regex
User-defined patterns (in addition to own-nick matching) that trigger mention highlighting. Store as a list of regex strings in user settings (new `settings` table in control DB, or a flat JSON file). The frontend evaluates them against each incoming message.

### 4.2 Mute channels/networks
Per-buffer mute flag stored in the control DB buffers table. Muted buffers suppress unread counts, mention highlights, and push notifications. Toggle via context menu or buffer settings panel.

### 4.3 Web Push notifications
Implement the Web Push API so notifications arrive even when the tab is closed:
1. Frontend subscribes via `PushManager.subscribe()` using the VAPID public key.
2. Backend stores the subscription endpoint in the control DB.
3. On a mention event the backend sends a push via the Web Push protocol.
4. Requires generating a VAPID key pair (one-time, stored in config/DB).

---

## 5. Media & file handling

### 5.1 Video and audio previews
Extend the existing preview pipeline to detect video (`video/mp4`, `video/webm`) and audio (`audio/mpeg`, `audio/ogg`) content types and return a `type: "video"` or `type: "audio"` preview event. The frontend renders an HTML5 `<video>` or `<audio>` element instead of an `<img>`. HTTPS-only to avoid mixed content.

### 5.2 File upload
Add a `POST /api/upload` endpoint that accepts multipart file data, stores it under a configurable directory, and returns a public URL. The frontend adds a drag-and-drop zone and a paperclip button that inserts the returned URL into the input field. Config options: max file size, storage path, optional base URL override.

---

## 6. Server / operational

### 6.1 Per-network connect commands
Implemented. A list of raw IRC commands (e.g. `PRIVMSG NickServ :IDENTIFY hunter2`) executes in order after successful IRC registration and before autojoin. Commands are stored in `control.db` and are editable via explicit API/web UI paths because they may contain secrets.

### 6.2 Message log retention
Backend message logs are retained indefinitely. Do not add automatic max-age cleanup, max-row cleanup, or periodic deletion jobs for stored messages.

### 6.3 HTTPS for the web interface
Add optional TLS to the HTTP server itself. Config options: `tls_cert_file`, `tls_key_file`. When set, listen on HTTPS instead of HTTP. Alternatively, document the recommended nginx/Caddy reverse-proxy setup as the preferred approach (lower maintenance burden).

---

## Out of scope (will not implement)

- Multi-user support, LDAP, session auth — explicitly outside lurker's design
- WEBIRC — only relevant for multi-user gateways
- Identd / Oidentd — server infrastructure, not client-side
- STS auto-upgrade — handled by the IRC server, transparent to the client
- Client certificate IRC auth — SASL covers this use case
- Prefetch/proxy for link previews — lurker already has a preview pipeline; proxying adds hosting complexity with minimal benefit for a single-user setup
