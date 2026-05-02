import { activeBuffer, type Message, state } from "./app-state";
import {
  classifyKind,
  dayKeyOf,
  escapeHTML,
  formatTime,
  highlightMentions,
  inlineCode,
  isSelf,
  linkify,
  mentionsMe,
} from "./format";
import { mircFormat } from "./mirc";
import { nickEl, sysBodyDOM } from "./nick";
import { renderPreviews } from "./preview";

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
  if (buffer.topic_set_by) {
    const setter = document.createElement("span");
    setter.className = "topicsetter";
    setter.textContent = `— ${buffer.topic_set_by}`;
    dom.bufferTopicEl.appendChild(setter);
  }
  const edit = document.createElement("span");
  edit.className = "edit";
  edit.setAttribute("aria-hidden", "true");
  edit.appendChild(deps.iconEl("ic-pencil", 12));
  dom.bufferTopicEl.appendChild(edit);

  dom.inputEl.placeholder = "";
}

export function renderActiveView(dom: MessagesDom, _deps: MessageDeps) {
  const buffer = activeBuffer();
  if (!buffer) return;
  if (buffer.kind === "status") {
    dom.messagesEl.hidden = true;
    dom.statusViewEl.hidden = false;
    renderStatusView(dom.statusViewEl);
  } else {
    dom.statusViewEl.hidden = true;
    dom.messagesEl.hidden = false;
    renderMessages(dom.messagesEl);
  }
}

export function inferUnreadCounts() {
  for (const buffer of state.buffers.values()) {
    const list = state.messages.get(buffer.id) || [];
    const lastSeen = buffer.last_seen_id || 0;
    const unreadActivity = list.filter((message) => message.id > lastSeen && countsAsUnreadActivity(message));
    const mentions = unreadActivity.filter((message) => mentionsMe(message, state.me.nick)).length;
    buffer.unread = buffer.id === state.activeId ? 0 : unreadActivity.length;
    buffer.mentions = buffer.id === state.activeId ? 0 : mentions;
  }
}

export function onMessage(
  msg: Message,
  handlers: { renderActiveView: () => void; maybeMarkActiveRead: () => void; renderSidebar: () => void },
) {
  const list = state.messages.get(msg.buffer_id) || [];
  const idx = list.findIndex((message) => message.id === msg.id);
  if (idx >= 0) list[idx] = msg;
  else list.push(msg);
  list.sort((a, b) => a.id.localeCompare(b.id));
  state.messages.set(msg.buffer_id, list);
  const buffer = state.buffers.get(msg.buffer_id);
  if (
    buffer &&
    msg.buffer_id !== state.activeId &&
    msg.id > (buffer.last_seen_id || "") &&
    countsAsUnreadActivity(msg)
  ) {
    buffer.unread = (buffer.unread || 0) + 1;
    if (mentionsMe(msg, state.me.nick)) buffer.mentions = (buffer.mentions || 0) + 1;
  }
  if (msg.buffer_id === state.activeId) {
    handlers.renderActiveView();
    handlers.maybeMarkActiveRead();
  }
  handlers.renderSidebar();
}

export function onPreview(
  msg: { buffer_id: string; message_id: string; previews?: Message["previews"] },
  messagesEl: HTMLElement,
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
  const existing = row.querySelector(".previews");
  if (existing) existing.remove();
  const previewsEl = renderPreviews(message.previews);
  if (previewsEl) row.appendChild(previewsEl);
}

export function onBufferUpdate(
  msg: {
    id: string;
    topic?: string;
    joined?: boolean;
    last_seen_id?: string;
    show_embeds?: boolean;
    show_presence_events?: boolean;
    collapse_presence_events?: boolean;
    pinned?: boolean;
  },
  handlers: { inferUnreadCounts: () => void; renderHeader: () => void; renderSidebar: () => void },
) {
  const buffer = state.buffers.get(msg.id);
  if (!buffer) return;
  if (Object.hasOwn(msg, "topic")) buffer.topic = msg.topic || "";
  if (Object.hasOwn(msg, "joined")) buffer.joined = Boolean(msg.joined);
  if (Object.hasOwn(msg, "last_seen_id")) buffer.last_seen_id = msg.last_seen_id || "";
  if (Object.hasOwn(msg, "show_embeds")) buffer.show_embeds = Boolean(msg.show_embeds);
  if (Object.hasOwn(msg, "show_presence_events")) buffer.show_presence_events = Boolean(msg.show_presence_events);
  if (Object.hasOwn(msg, "collapse_presence_events")) {
    buffer.collapse_presence_events = Boolean(msg.collapse_presence_events);
  }
  if (Object.hasOwn(msg, "pinned")) buffer.pinned = Boolean(msg.pinned);
  handlers.inferUnreadCounts();
  handlers.renderHeader();
  handlers.renderSidebar();
}

export function onHistoryResult(
  msg: { buffer_id: string; messages?: Message[] },
  handlers: { renderActiveView: () => void },
  messagesEl: HTMLElement,
) {
  const existing = state.messages.get(msg.buffer_id) || [];
  const known = new Set(existing.map((message) => message.id));
  const prepend = (msg.messages || []).filter((message) => !known.has(message.id));
  state.messages.set(msg.buffer_id, [...prepend, ...existing]);
  if (prepend.length === 0) state.historyExhausted.add(msg.buffer_id);
  state.loadingHistory.delete(msg.buffer_id);
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

const PRESENCE_KINDS = ["join", "part", "quit", "nick"] as const;
const expandedPresenceGroups = new Set<string>();

function renderMessages(messagesEl: HTMLElement) {
  messagesEl.innerHTML = "";
  const list = state.messages.get(state.activeId ?? "") || [];
  const buffer = activeBuffer();
  const lastSeen = buffer?.last_seen_id || "";
  const collapsePresence = shouldCollapsePresence(buffer);
  let unreadInserted = false;
  let lastDayKey: string | null = null;
  let presenceGroup: Message[] = [];

  const flushPresenceGroup = () => {
    if (presenceGroup.length === 0) return;
    if (presenceGroup.length === 1) {
      const [message] = presenceGroup;
      if (message) messagesEl.appendChild(messageRow(message));
    } else {
      messagesEl.appendChild(
        presenceSummaryRow(presenceGroup, () => {
          const scrollTop = messagesEl.scrollTop;
          renderMessages(messagesEl);
          messagesEl.scrollTop = scrollTop;
        }),
      );
      if (expandedPresenceGroups.has(presenceGroupKey(presenceGroup))) {
        for (const message of presenceGroup) {
          const row = messageRow(message);
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
    }
    if (
      !unreadInserted &&
      message.id > lastSeen &&
      countsAsUnreadActivity(message) &&
      state.activeId !== null &&
      lastSeen !== ""
    ) {
      flushPresenceGroup();
      const bar = document.createElement("div");
      bar.className = "unreadbar";
      const label = document.createElement("span");
      label.textContent = "New messages";
      bar.appendChild(label);
      messagesEl.appendChild(bar);
      unreadInserted = true;
    }
    if (collapsePresence && isPresenceEvent(message)) {
      presenceGroup.push(message);
      continue;
    }
    flushPresenceGroup();
    messagesEl.appendChild(messageRow(message));
  }
  flushPresenceGroup();
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

function messageRow(message: Message) {
  const row = document.createElement("div");
  row.dataset.id = String(message.id);
  const kind = classifyKind(message.kind);
  const isMention = mentionsMe(message, state.me.nick);
  const self = isSelf(message, state.me.nick);
  row.className = `msg ${kind === "message" ? "flat" : kind}${isMention ? " mention" : ""}${self ? " self" : ""}`;

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
  return count === 1 ? kind : `${kind}s`;
}

function presenceGroupKey(messages: Message[]) {
  return messages.map((message) => message.id).join("|");
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

function countsAsUnreadActivity(message: Message) {
  return ![
    "join",
    "part",
    "quit",
    "nick",
    "mode",
    "kick",
    "connected",
    "disconnected",
    "error",
    "away",
    "back",
    "account",
    "chghost",
    "status",
  ].includes(message.kind || "");
}

function renderBodyHTML(message: Message) {
  const body = message.content || "";
  return highlightMentions(linkify(inlineCode(mircFormat(escapeHTML(body)))), state.me.nick);
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
