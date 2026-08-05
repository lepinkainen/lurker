import { activeBuffer, type Buffer, type Message, state } from "./app-state";
import { dayKeyOf, escapeHTML, formatTime, highlightMentions, inlineCode, linkify, type MessageKind } from "./format";
import { renderSegmentsHTML } from "./mirc";
import { nickEl, sysBodyDOM } from "./nick";
import { renderPreviews } from "./preview";
import type { ScrollStick } from "./scroll-stick";

// Display classification and the mention/self/unread flags are computed
// server-side (irc.ComputeMessageSemantics) and shipped on every message via
// /api/state, history, and live events. The client consumes them verbatim and
// must NOT re-derive them from message.kind — doing so reintroduces the
// Go<->TS drift these helpers exist to eliminate. See irc/semantics.go.
export function msgDisplayKind(message: Message): MessageKind {
  // display_kind is non-omitzero server-side, so it is always present on
  // server-sourced messages; the "message" default is only a type guard.
  return (message.display_kind as MessageKind) || "message";
}

export function msgCountsAsUnread(message: Message): boolean {
  // counts_as_unread is omitzero: true is on the wire, false/absent is falsy.
  return message.counts_as_unread === true;
}

export function msgMentionsMe(message: Message): boolean {
  return message.mentions_me === true;
}

export function msgHighlight(message: Message): boolean {
  return message.highlight === true;
}

export function msgIsSelf(message: Message): boolean {
  return message.is_self === true;
}

export type MessagesDom = {
  messagesEl: HTMLElement;
  statusViewEl: HTMLElement;
  bufferNameEl: HTMLElement;
  bufferTopicEl: HTMLElement;
  inputEl: HTMLInputElement;
};

type MessageDeps = {
  renderPromptNick: () => void;
  iconEl: (symbolId: string, size: number, opts?: { className?: string; label?: string }) => SVGSVGElement;
  stick: ScrollStick;
  // Explicit read ack (unread bar click). Wired to ReadTracker.ackBufferRead.
  ackBufferRead: (bufferId: string) => void;
};

const LEADING_HASH_RE = /^#/u;

export function renderHeader(dom: MessagesDom, deps: MessageDeps) {
  const buffer = activeBuffer();
  if (!buffer) return;
  deps.renderPromptNick();
  const isChannel = buffer.kind === "channel";
  const network = state.networks.get(buffer.network_id);
  dom.bufferNameEl.innerHTML = "";
  if (isChannel) {
    const hash = document.createElement("span");
    hash.className = "hash";
    hash.textContent = "#";
    dom.bufferNameEl.append(hash, document.createTextNode(buffer.name.replace(LEADING_HASH_RE, "")));
  } else if (buffer.kind === "status") {
    dom.bufferNameEl.textContent = network ? `${network.name} (status)` : "(status)";
  } else {
    dom.bufferNameEl.textContent = buffer.name;
  }

  const networkName = network?.name ?? "";
  const channelDisplay =
    buffer.kind === "channel" ? buffer.name : buffer.kind === "status" ? `${networkName} status` : buffer.name;
  document.title = networkName ? `${channelDisplay} | ${networkName}` : channelDisplay;

  dom.bufferTopicEl.innerHTML = "";
  const topicText = document.createElement("span");
  topicText.className = "topictext";
  if (buffer.kind === "status") {
    topicText.textContent = network ? `${network.host || ""} · ${network.status || "offline"}` : "";
  } else {
    topicText.textContent = buffer.topic || "No topic set";
    topicText.classList.toggle("is-empty", !buffer.topic);
  }
  dom.bufferTopicEl.appendChild(topicText);
  // Setter attribution only makes sense next to an actual topic —
  // "No topic set — alice" would misattribute the empty state.
  if (buffer.topic && buffer.topic_set_by) {
    const setter = document.createElement("span");
    setter.className = "topicsetter";
    setter.textContent = `— ${buffer.topic_set_by}`;
    if (buffer.topic_set_at)
      setter.title = `Set by ${buffer.topic_set_by} on ${new Date(buffer.topic_set_at).toLocaleString()}`;
    dom.bufferTopicEl.appendChild(setter);
  }
  const edit = document.createElement("span");
  edit.className = "edit";
  edit.setAttribute("aria-hidden", "true");
  edit.appendChild(deps.iconEl("ic-pencil", 12));
  dom.bufferTopicEl.appendChild(edit);

  dom.inputEl.placeholder = buffer.kind === "status" ? "Commands only, e.g. /nick, /list, /msg NickServ …" : "";
}

export function renderActiveView(dom: MessagesDom, deps: MessageDeps) {
  const buffer = activeBuffer();
  if (!buffer) return;
  if (buffer.kind === "status") {
    dom.messagesEl.hidden = true;
    dom.statusViewEl.hidden = false;
    renderStatusView(dom.statusViewEl);
  } else {
    dom.statusViewEl.hidden = true;
    dom.messagesEl.hidden = false;
    renderMessages(dom.messagesEl, deps.stick, deps.ackBufferRead);
  }
}

export function onMessage(msg: Message, handlers: { renderActiveView: () => void; renderSidebar: () => void }) {
  const list = state.messages.get(msg.buffer_id) || [];
  const idx = list.findIndex((message) => message.id === msg.id);
  const isNew = idx < 0;
  if (isNew) list.push(msg);
  else list[idx] = msg;
  list.sort((a, b) => a.id.localeCompare(b.id));
  state.messages.set(msg.buffer_id, list);
  const buffer = state.buffers.get(msg.buffer_id);
  // Local bookkeeping between server syncs, for EVERY buffer including the
  // active focused one: unread arrivals count up and the marker sticks to the
  // first of them. No auto-ack, no focus gating — the marker clears only on an
  // explicit ack (spec: ai-docs/behaviors/new-messages-marker.md). Re-delivered
  // messages (isNew false) never double-count.
  if (buffer && isNew && msgCountsAsUnread(msg) && !msgIsSelf(msg) && msg.id > (buffer.last_seen_id || "")) {
    buffer.unread = (buffer.unread || 0) + 1;
    if (msgMentionsMe(msg) || msgHighlight(msg)) buffer.mentions = (buffer.mentions || 0) + 1;
    if (buffer.marker_id === undefined) {
      buffer.marker_id = msg.id;
      buffer.marker_ts = msg.ts;
    }
  }
  if (msg.buffer_id === state.activeId) handlers.renderActiveView();
  handlers.renderSidebar();
}

export function onPreview(
  msg: { buffer_id: string; message_id: string; previews?: Message["previews"] },
  messagesEl: HTMLElement,
  stick: ScrollStick,
) {
  const list = state.messages.get(msg.buffer_id);
  if (!list) return;
  const message = list.find((x) => x.id === msg.message_id);
  if (!message) return;
  message.previews = msg.previews || [];
  if (msg.buffer_id !== state.activeId) return;
  if (state.buffers.get(msg.buffer_id)?.show_embeds === false) return;
  const row = messagesEl.querySelector(`[data-id="${msg.message_id}"]`);
  if (!row) return;
  const wasPinned = stick.isPinned();
  const existing = row.querySelector(".previews");
  if (existing) existing.remove();
  const previewsEl = renderPreviews(message.previews);
  if (previewsEl) row.appendChild(previewsEl);
  if (wasPinned) stick.snap();
  if (previewsEl) stick.watch(previewsEl);
}

export function onBufferUpdate(
  msg: {
    id: string;
    topic?: string;
    topic_set_by?: string;
    topic_set_at?: string;
    joined?: boolean;
    archived?: boolean;
    last_seen_id?: string;
    // mark_read variant: marker_id is ALWAYS present (null = caught up →
    // clear). The topic/joined variant omits the key entirely = unchanged.
    marker_id?: string | null;
    marker_ts?: string;
    show_embeds?: boolean;
    show_presence_events?: boolean;
    collapse_presence_events?: boolean;
    pinned?: boolean;
    pin_order?: number;
    unread?: number;
    mentions?: number;
  },
  handlers: { renderHeader: () => void; renderSidebar: () => void },
) {
  const buffer = state.buffers.get(msg.id);
  if (!buffer) return;
  if (Object.hasOwn(msg, "topic")) buffer.topic = msg.topic || "";
  if (Object.hasOwn(msg, "topic_set_by")) buffer.topic_set_by = msg.topic_set_by || "";
  if (Object.hasOwn(msg, "topic_set_at")) buffer.topic_set_at = msg.topic_set_at || "";
  if (Object.hasOwn(msg, "joined")) buffer.joined = Boolean(msg.joined);
  if (Object.hasOwn(msg, "archived")) buffer.archived = Boolean(msg.archived);
  if (Object.hasOwn(msg, "last_seen_id")) buffer.last_seen_id = msg.last_seen_id || "";
  if (Object.hasOwn(msg, "marker_id")) {
    buffer.marker_id = msg.marker_id || undefined;
    buffer.marker_ts = msg.marker_ts || undefined;
  }
  if (Object.hasOwn(msg, "show_embeds")) buffer.show_embeds = Boolean(msg.show_embeds);
  if (Object.hasOwn(msg, "show_presence_events")) buffer.show_presence_events = Boolean(msg.show_presence_events);
  if (Object.hasOwn(msg, "collapse_presence_events")) {
    buffer.collapse_presence_events = Boolean(msg.collapse_presence_events);
  }
  if (Object.hasOwn(msg, "pinned")) buffer.pinned = Boolean(msg.pinned);
  if (typeof msg.pin_order === "number") buffer.pin_order = msg.pin_order;
  if (typeof msg.unread === "number") buffer.unread = msg.unread;
  if (typeof msg.mentions === "number") buffer.mentions = msg.mentions;
  handlers.renderHeader();
  handlers.renderSidebar();
}

export function onHistoryResult(
  msg: { buffer_id: string; messages?: Message[] },
  handlers: { renderActiveView: () => void; renderSidebar?: () => void },
  messagesEl: HTMLElement,
) {
  const existing = state.messages.get(msg.buffer_id) || [];
  const known = new Set(existing.map((message) => message.id));
  const fresh = (msg.messages || []).filter((message) => !known.has(message.id));
  // Merge-sort by id rather than prepend: scroll-up pages are strictly older
  // than everything known (sort is a no-op), but a history_backfill refetch
  // delivers rows that belong *between* existing messages (the disconnect
  // gap) and must slot into place.
  const merged = [...fresh, ...existing].sort((a, b) => a.id.localeCompare(b.id));
  state.messages.set(msg.buffer_id, merged);
  // "No older history" is only provable for a scroll-up request (which set
  // loadingHistory); an empty backfill refetch says nothing about the top.
  if (fresh.length === 0 && state.loadingHistory.has(msg.buffer_id)) {
    state.historyExhausted.add(msg.buffer_id);
  }
  state.loadingHistory.delete(msg.buffer_id);
  const buffer = state.buffers.get(msg.buffer_id);
  let unreadChanged = false;
  if (buffer) {
    // Backfilled gap messages are unread (their ids sort after last_seen);
    // mirror onMessage's bookkeeping so the sidebar count and the marker
    // reflect the recovered messages. Scroll-up pages are older than
    // last_seen and never match.
    for (const message of fresh) {
      if (msgCountsAsUnread(message) && !msgIsSelf(message) && message.id > (buffer.last_seen_id || "")) {
        buffer.unread = (buffer.unread || 0) + 1;
        if (msgMentionsMe(message) || msgHighlight(message)) buffer.mentions = (buffer.mentions || 0) + 1;
        if (buffer.marker_id === undefined || message.id < buffer.marker_id) {
          buffer.marker_id = message.id;
          buffer.marker_ts = message.ts;
        }
        unreadChanged = true;
      }
    }
  }
  if (unreadChanged) handlers.renderSidebar?.();
  if (msg.buffer_id === state.activeId) {
    const oldHeight = messagesEl.scrollHeight;
    handlers.renderActiveView();
    messagesEl.scrollTop = messagesEl.scrollHeight - oldHeight;
  }
}

function renderStatusView(statusViewEl: HTMLElement) {
  statusViewEl.innerHTML = "";
  const buffer = activeBuffer();
  if (!buffer) return;
  const wrap = document.createElement("div");
  wrap.className = "statuslines";
  const list = state.messages.get(buffer.id) || [];
  for (const message of list) wrap.appendChild(statusLine(message));
  if (list.length === 0) {
    const network = state.networks.get(buffer.network_id);
    const empty = document.createElement("div");
    empty.innerHTML = `<span class="stts">—</span><span class="stcat ok">ok</span><span class="stdim">${escapeHTML(network ? `connected to ${network.host || network.name}` : "no log entries yet")}</span>`;
    wrap.appendChild(empty);
  }
  statusViewEl.appendChild(wrap);
  statusViewEl.scrollTop = statusViewEl.scrollHeight;
}

function statusLine(message: Message) {
  const row = document.createElement("div");
  const ts = document.createElement("span");
  ts.className = "stts";
  ts.textContent = formatTime(message.ts);
  const cat = document.createElement("span");
  let catText = "-->",
    catCls = "";
  if (message.kind === "connected") {
    catText = "OK";
    catCls = "ok";
  } else if (message.kind === "disconnected" || message.kind === "error") {
    catText = "ERR";
    catCls = "bad";
  } else if (message.kind === "notice") {
    catText = "<--";
  }
  cat.className = `stcat ${catCls}`.trim();
  cat.textContent = catText;
  const body = document.createElement("span");
  body.className = "stdim";
  body.textContent = (message.sender ? `${message.sender} ` : "") + (message.content || message.kind || "");
  row.append(ts, cat, body);
  return row;
}

// The ONE sanctioned Go<->TS mirror: the server ships no per-message
// "is_presence" flag, so presence grouping is derived client-side from kind.
// Kept in lockstep with irc.presenceKinds via the semantic-kinds fixture
// contract test (tests/semantics-contract.test.ts). Do not add new local kind
// lists — every other classification is server-supplied (see msgDisplayKind).
export const PRESENCE_KINDS = ["join", "part", "quit", "nick", "away", "back", "account", "chghost"] as const;
const expandedPresenceGroups = new Set<string>();

// Consecutive plain messages from the same sender within this window
// render as one author group: only the first row shows the nick.
const GROUP_WINDOW_MS = 5 * 60 * 1000;

function renderMessages(messagesEl: HTMLElement, stick: ScrollStick, ackBufferRead: (bufferId: string) => void) {
  messagesEl.replaceChildren();
  const list = state.messages.get(state.activeId ?? "") || [];
  const buffer = activeBuffer();
  // The divider and the pinned unread bar are pure functions of the
  // server-derived buffer.marker_id — no client-side marker state exists.
  const anchorId = buffer?.marker_id || "";
  if (buffer && anchorId && list.length > 0) {
    const markerLoaded = list.some((message) => message.id === anchorId);
    messagesEl.appendChild(unreadBarEl(buffer, markerLoaded, ackBufferRead));
  }
  const collapsePresence = shouldCollapsePresence(buffer);
  let unreadInserted = false;
  let lastDayKey: string | null = null;
  let presenceGroup: Message[] = [];
  // Author-grouping state: the sender/time of the last plain message row
  // rendered, or null when the run was broken by a separator or other row.
  let groupSender: string | null = null;
  let groupTsMs = 0;
  const breakGroup = () => {
    groupSender = null;
  };

  const rerender = () => {
    const scrollTop = messagesEl.scrollTop;
    renderMessages(messagesEl, stick, ackBufferRead);
    messagesEl.scrollTop = scrollTop;
  };
  const renderPlainRun = (items: Message[]) => {
    if (items.length === 0) return;
    if (items.length === 1) {
      const [message] = items;
      if (message) messagesEl.appendChild(messageRow(message, msgDisplayKind(message)));
      return;
    }
    messagesEl.appendChild(presenceSummaryRow(items, rerender));
    if (expandedPresenceGroups.has(presenceGroupKey(items))) {
      for (const message of items) {
        const row = messageRow(message, msgDisplayKind(message));
        row.classList.add("presence-expanded");
        messagesEl.appendChild(row);
      }
    }
  };
  const flushPresenceGroup = () => {
    if (presenceGroup.length === 0) return;
    breakGroup();
    const groups = groupPresenceRun(presenceGroup);
    for (const group of groups) {
      if (group.kind === "plain") {
        renderPlainRun(group.items);
        continue;
      }
      messagesEl.appendChild(netsplitSummaryRow(group, rerender));
      const key = netsplitGroupKey(group);
      if (expandedPresenceGroups.has(key)) {
        for (const message of [...group.quits, ...group.rejoins]) {
          const row = messageRow(message, msgDisplayKind(message));
          row.classList.add("presence-expanded");
          messagesEl.appendChild(row);
        }
      }
    }
    presenceGroup = [];
  };

  for (const message of list) {
    if (isHiddenPresence(message, buffer)) continue;
    const dayKey = dayKeyOf(message.ts);
    if (dayKey && dayKey !== lastDayKey) {
      flushPresenceGroup();
      messagesEl.appendChild(daySeparator(message.ts));
      lastDayKey = dayKey;
      breakGroup();
    }
    if (!unreadInserted && anchorId && message.id === anchorId && state.activeId !== null) {
      flushPresenceGroup();
      const bar = document.createElement("div");
      bar.className = "unreadbar";
      const label = document.createElement("span");
      label.textContent = "New messages";
      bar.appendChild(label);
      messagesEl.appendChild(bar);
      unreadInserted = true;
      breakGroup();
    }
    if (collapsePresence && isPresenceEvent(message)) {
      presenceGroup.push(message);
      continue;
    }
    flushPresenceGroup();
    const kind = msgDisplayKind(message);
    const sender = message.sender || "";
    const tsMs = Date.parse(message.ts || "") || 0;
    const continued =
      kind === "message" && groupSender !== null && sender === groupSender && tsMs - groupTsMs <= GROUP_WINDOW_MS;
    messagesEl.appendChild(messageRow(message, kind, continued));
    if (kind === "message") {
      groupSender = sender;
      groupTsMs = tsMs;
    } else {
      breakGroup();
    }
  }
  flushPresenceGroup();
  stick.snap();
  stick.watch(messagesEl);
}

// Server caps unread counting at 1000; at the cap the exact count is unknown.
const UNREAD_COUNT_CAP = 1000;

// Pinned bar at the top of the message area, shown whenever the active buffer
// has a server-derived marker. Activating it is one of the only two explicit
// read acks (the other is Esc).
function unreadBarEl(buffer: Buffer, markerLoaded: boolean, ackBufferRead: (bufferId: string) => void) {
  const bar = document.createElement("button");
  bar.type = "button";
  bar.className = "unread-banner";
  bar.textContent = unreadBarLabel(buffer, markerLoaded);
  bar.title = "Mark as read";
  bar.addEventListener("click", () => ackBufferRead(buffer.id));
  return bar;
}

function unreadBarLabel(buffer: Buffer, markerLoaded: boolean): string {
  const unread = buffer.unread || 0;
  // Count is unreliable at the server cap or when the marker message sits
  // outside loaded history — fall back to the age of the boundary.
  if (unread >= UNREAD_COUNT_CAP || !markerLoaded) {
    const since = formatMarkerTs(buffer.marker_ts);
    return since ? `new since ${since}` : "new messages";
  }
  return unread === 1 ? "1 new message" : `${unread} new messages`;
}

// Marker boundary timestamp in local time: today → HH:MM, yesterday →
// "yesterday HH:MM", older → short date + HH:MM.
function formatMarkerTs(ts: string | undefined): string {
  if (!ts) return "";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  const time = formatTime(ts);
  const today = new Date();
  if (d.toDateString() === today.toDateString()) return time;
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return `yesterday ${time}`;
  const date = d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  return `${date} ${time}`;
}

function messageRow(message: Message, kind: MessageKind, continued = false) {
  const row = document.createElement("div");
  row.dataset.id = String(message.id);
  const isMention = msgMentionsMe(message) || msgHighlight(message);
  const self = msgIsSelf(message);
  row.className = `msg ${kind === "message" ? "flat" : kind}${continued ? " cont" : ""}${isMention ? " mention" : ""}${self ? " self" : ""}`;

  const ts = document.createElement("span");
  ts.className = "ts";
  ts.textContent = formatTime(message.ts);
  const gutter = document.createElement("span");
  gutter.className = "gutter";
  const body = document.createElement("span");
  body.className = "body";

  if (kind === "sys") {
    const arrowCls =
      message.kind === "join" ? "in" : message.kind === "part" || message.kind === "quit" ? "out" : "nil";
    const glyph = message.kind === "join" ? "→" : message.kind === "part" || message.kind === "quit" ? "←" : "·";
    gutter.innerHTML = `<span class="arrow ${arrowCls}">${glyph}</span>`;
    body.replaceChildren(...sysBodyDOM(message));
    row.append(ts, gutter, body);
    return row;
  }
  if (kind === "notice") {
    gutter.textContent = "!";
  } else if (kind === "ctcp") {
    gutter.textContent = "?";
  } else if (kind === "action") {
    gutter.textContent = "*";
  }

  const nick = nickEl(message.sender || "", "nick", kind === "notice" ? `-${message.sender || "*"}-` : undefined);
  body.innerHTML = renderBodyHTML(message);
  if (kind === "action") {
    body.innerHTML = `${escapeHTML(message.sender || "")} ${renderBodyHTML(message)}`;
  } else if (kind === "ctcp") {
    body.innerHTML = `[CTCP] ${escapeHTML(message.content || "")}`;
  }

  row.append(ts, gutter, nick, body);
  const buffer = state.buffers.get(message.buffer_id);
  const previewsEl = buffer?.show_embeds === false ? null : renderPreviews(message.previews);
  if (previewsEl) row.appendChild(previewsEl);
  return row;
}

function presenceSummaryRow(messages: Message[], rerender: () => void) {
  const key = presenceGroupKey(messages);
  const expanded = expandedPresenceGroups.has(key);
  const row = document.createElement("div");
  row.className = `msg sys presence-summary${expanded ? " expanded" : ""}`;
  row.dataset.presenceCount = String(messages.length);
  row.dataset.presenceIds = messages.map((message) => message.id).join(",");
  row.dataset.expanded = String(expanded);
  row.setAttribute("role", "button");
  row.tabIndex = 0;
  row.setAttribute("aria-expanded", String(expanded));

  const ts = document.createElement("span");
  ts.className = "ts";
  ts.textContent = formatTime(messages[0]?.ts);
  const gutter = document.createElement("span");
  gutter.className = "gutter";
  gutter.textContent = expanded ? "−" : "+";
  const body = document.createElement("span");
  body.className = "body";
  body.textContent = presenceSummaryText(messages);

  const toggle = () => {
    if (expandedPresenceGroups.has(key)) expandedPresenceGroups.delete(key);
    else expandedPresenceGroups.add(key);
    rerender();
  };
  row.addEventListener("click", toggle);
  row.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    toggle();
  });

  row.append(ts, gutter, body);
  return row;
}

function presenceSummaryText(messages: Message[]) {
  const counts = new Map<string, number>();
  for (const message of messages) counts.set(message.kind || "event", (counts.get(message.kind || "event") || 0) + 1);
  const parts = PRESENCE_KINDS.map((kind) => [kind, counts.get(kind) || 0] as const)
    .filter(([, count]) => count > 0)
    .map(([kind, count]) => `${count} ${presenceKindLabel(kind, count)}`);
  return `${messages.length} presence events: ${parts.join(", ")}`;
}

function presenceKindLabel(kind: (typeof PRESENCE_KINDS)[number], count: number) {
  if (kind === "nick") return count === 1 ? "nick change" : "nick changes";
  if (kind === "away" || kind === "back") return kind;
  if (kind === "account") return count === 1 ? "account change" : "account changes";
  if (kind === "chghost") return count === 1 ? "host change" : "host changes";
  return count === 1 ? kind : `${kind}s`;
}

function presenceGroupKey(messages: Message[]) {
  return messages.map((message) => message.id).join("|");
}

// NetsplitGroup is one collapsed netsplit within a presence run. Membership
// and rejoin matching are server-computed: messages arrive annotated with a
// shared netsplit id (message.netsplit), the client only collates them.
type NetsplitGroup = {
  kind: "netsplit";
  id: string;
  serverA: string;
  serverB: string;
  quits: Message[];
  rejoins: Message[];
};
type PresenceGroup = { kind: "plain"; items: Message[] } | NetsplitGroup;

// groupPresenceRun collates a run of presence messages by their server-side
// netsplit annotation. A group renders at its first member's position;
// later members (more quits, rejoins) fold into it.
export function groupPresenceRun(messages: Message[]): PresenceGroup[] {
  const out: PresenceGroup[] = [];
  const groupsById = new Map<string, NetsplitGroup>();
  let plain: Message[] = [];
  const flushPlain = () => {
    if (plain.length > 0) {
      out.push({ kind: "plain", items: plain });
      plain = [];
    }
  };
  for (const message of messages) {
    const ns = message.netsplit;
    if (!ns) {
      plain.push(message);
      continue;
    }
    let group = groupsById.get(ns.id);
    if (!group) {
      flushPlain();
      group = { kind: "netsplit", id: ns.id, serverA: ns.server_a, serverB: ns.server_b, quits: [], rejoins: [] };
      groupsById.set(ns.id, group);
      out.push(group);
    }
    (message.kind === "join" ? group.rejoins : group.quits).push(message);
  }
  flushPlain();
  return out;
}

function netsplitGroupKey(group: NetsplitGroup) {
  // The server-side group id is stable across re-renders and late rejoins,
  // so the expandedPresenceGroups Set keeps tracking the same row.
  return `netsplit:${group.id}`;
}

const MAX_NETSPLIT_NICKS = 8;

function netsplitSummaryRow(group: NetsplitGroup, rerender: () => void) {
  const key = netsplitGroupKey(group);
  const expanded = expandedPresenceGroups.has(key);
  const row = document.createElement("div");
  row.className = `msg sys presence-summary netsplit${expanded ? " expanded" : ""}`;
  row.dataset.expanded = String(expanded);
  row.setAttribute("role", "button");
  row.tabIndex = 0;
  row.setAttribute("aria-expanded", String(expanded));

  const ts = document.createElement("span");
  ts.className = "ts";
  ts.textContent = formatTime(group.quits[0]?.ts);
  const gutter = document.createElement("span");
  gutter.className = "gutter";
  gutter.textContent = expanded ? "−" : "+";
  const body = document.createElement("span");
  body.className = "body";

  const servers = document.createElement("span");
  servers.className = "netsplit-servers";
  servers.textContent = `${group.serverA} ↮ ${group.serverB}`;
  body.appendChild(servers);

  const quitNicks = group.quits.map((m) => m.sender || "");
  // Rejoin matching is server-side (a join is only annotated when it pairs
  // with a quit in this group), so the count needs no nick comparison.
  const stillGoneCount = Math.max(0, group.quits.length - group.rejoins.length);

  if (group.rejoins.length > 0) {
    body.appendChild(document.createTextNode(" → "));
    appendNickList(
      body,
      group.rejoins.map((m) => m.sender || ""),
    );
    body.appendChild(document.createTextNode(` rejoined`));
  }
  if (stillGoneCount > 0) {
    body.appendChild(document.createTextNode(` ⇐ `));
    body.appendChild(document.createTextNode(`${stillGoneCount} still gone`));
  }
  body.appendChild(document.createTextNode(` · ${group.quits.length} nipped out: `));
  appendNickList(body, quitNicks);

  const toggle = () => {
    if (expandedPresenceGroups.has(key)) expandedPresenceGroups.delete(key);
    else expandedPresenceGroups.add(key);
    rerender();
  };
  row.addEventListener("click", toggle);
  row.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    toggle();
  });

  row.append(ts, gutter, body);
  return row;
}

function appendNickList(parent: HTMLElement, nicks: string[]) {
  const visible = nicks.slice(0, MAX_NETSPLIT_NICKS);
  visible.forEach((nick, idx) => {
    if (idx > 0) parent.appendChild(document.createTextNode(", "));
    parent.appendChild(nickEl(nick, "nick"));
  });
  const overflow = nicks.length - visible.length;
  if (overflow > 0) parent.appendChild(document.createTextNode(` … +${overflow} more`));
}

function shouldCollapsePresence(buffer: import("./app-state").Buffer | undefined) {
  return buffer?.show_presence_events === true && buffer.collapse_presence_events === true;
}

function isPresenceEvent(message: Message) {
  return PRESENCE_KINDS.includes(message.kind as (typeof PRESENCE_KINDS)[number]);
}

function isHiddenPresence(message: Message, buffer: import("./app-state").Buffer | undefined) {
  return buffer?.show_presence_events === false && isPresenceEvent(message);
}

function renderBodyHTML(message: Message) {
  // mIRC parsing is server-side: formatted content arrives as segments,
  // plain content (no codes) ships without them.
  const html =
    message.segments !== undefined && message.segments.length > 0
      ? renderSegmentsHTML(message.segments)
      : escapeHTML(message.content || "");
  return highlightMentions(linkify(inlineCode(html)), state.me.nick);
}

function daySeparator(ts?: string) {
  const sep = document.createElement("div");
  sep.className = "daysep";
  const label = document.createElement("span");
  const d = new Date(ts || Date.now());
  const today = new Date();
  const isToday = d.toDateString() === today.toDateString();
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  const isYesterday = d.toDateString() === yesterday.toDateString();
  const dateLabel = d.toLocaleDateString(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric",
  });
  label.textContent = isToday ? `Today · ${dateLabel}` : isYesterday ? `Yesterday · ${dateLabel}` : dateLabel;
  sep.appendChild(label);
  return sep;
}
