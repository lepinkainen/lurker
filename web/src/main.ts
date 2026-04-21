import "./style.css";

type TweakSettings = {
  accent: string;
  density: string;
  showSeconds: boolean;
  showMemberList: boolean;
};

type LayoutSettings = {
  order: number[];
  collapsed: Record<number, boolean>;
  pinned: number[];
};

type Network = {
  id: number;
  name: string;
  host?: string;
  status?: string;
};

type Buffer = {
  id: number;
  network_id: number;
  name: string;
  kind: string;
  joined?: boolean;
  topic?: string;
  topic_set_by?: string;
  last_seen_id?: number;
  unread: number;
  mentions: number;
};

type Message = {
  id: number;
  buffer_id: number;
  sender?: string;
  target?: string;
  content?: string;
  kind?: string;
  ts?: string;
};

type Member = {
  nick: string;
  prefix: string;
  away: boolean;
  self: boolean;
};

type SlashCommand = {
  cmd: string;
  args: string;
  desc: string;
};

type AppState = {
  networks: Map<number, Network>;
  buffers: Map<number, Buffer>;
  messages: Map<number, Message[]>;
  members: Map<string, Member>;
  activeId: number | null;
  ws: WebSocket | null;
  wsReady: boolean;
  loadingHistory: Set<number>;
  historyExhausted: Set<number>;
  lastMarkedReadId: Map<number, number>;
  me: { nick: string };
  tweaks: TweakSettings;
  layout: LayoutSettings;
  drag: { id: number | null; over: number | null };
};

const TWEAKS_KEY = "lurker.tweaks";
const LAYOUT_KEY = "lurker.layout";
const DEFAULT_TWEAKS = {
  accent: "blue",
  density: "dense",
  showSeconds: false,
  showMemberList: true,
};
const DEFAULT_LAYOUT = { order: [], collapsed: {}, pinned: [] };

const SLASH_COMMANDS: SlashCommand[] = [
  { cmd: "/join", args: "<channel>", desc: "Join a channel" },
  { cmd: "/part", args: "[reason]", desc: "Leave the current channel" },
  { cmd: "/msg", args: "<nick> <text>", desc: "Open a private message" },
  { cmd: "/me", args: "<action>", desc: "Send a /me action" },
  { cmd: "/nick", args: "<newnick>", desc: "Change your nick" },
];

const state: AppState = {
  networks: new Map(),
  buffers: new Map(),
  messages: new Map(),
  members: new Map(),
  activeId: null,
  ws: null,
  wsReady: false,
  loadingHistory: new Set(),
  historyExhausted: new Set(),
  lastMarkedReadId: new Map(),
  me: { nick: "you" },
  tweaks: loadTweaks(),
  layout: loadLayout(),
  drag: { id: null, over: null },
};

function mustEl<T extends HTMLElement>(id: string): T {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing element #${id}`);
  return node as T;
}

const sbScrollEl = mustEl<HTMLDivElement>("sb-scroll");
const messagesEl = mustEl<HTMLElement>("messages");
const statusViewEl = mustEl<HTMLElement>("status-view");
const bufferNameEl = mustEl<HTMLElement>("buffer-name");
const bufferTopicEl = mustEl<HTMLElement>("buffer-topic");
const bufferMemcountEl = mustEl<HTMLElement>("buffer-memcount");
const memberCountInlineEl = mustEl<HTMLElement>("member-count-inline");
const toggleMembersEl = mustEl<HTMLButtonElement>("toggle-members");
const inputEl = mustEl<HTMLInputElement>("input");
const inputForm = mustEl<HTMLFormElement>("input-form");
const inputNickEl = mustEl<HTMLElement>("input-nick");
const cmdPopEl = mustEl<HTMLElement>("cmd-pop");
const memberListEl = mustEl<HTMLElement>("member-list");
const memberCountEl = mustEl<HTMLElement>("member-count");
const memberPaneEl = mustEl<HTMLElement>("member-pane");
const tweaksTabEl = mustEl<HTMLButtonElement>("tweaks-tab");
const tweaksPanelEl = mustEl<HTMLElement>("tweaks-panel");
const tweaksCloseEl = mustEl<HTMLButtonElement>("tweaks-close");

function init() {
  applyTweaks(state.tweaks);
  initTweaksPanel();
  initCmdPop();
  inputForm.addEventListener("submit", onSubmit);
  messagesEl.addEventListener("scroll", onMessagesScroll);
  toggleMembersEl.addEventListener("click", () => {
    state.tweaks.showMemberList = !state.tweaks.showMemberList;
    saveTweaks(state.tweaks);
    applyTweaks(state.tweaks);
    syncTweaksPanel();
    renderMembers();
  });
  hydrate();
}

async function hydrate() {
  try {
    const res = await fetch("/api/state");
    if (!res.ok) throw new Error("state " + res.status);
    const s: any = await res.json();
    state.me.nick = s.current_nick || s.nick || s.user?.nick || "you";
    inputNickEl.textContent = state.me.nick;
    for (const n of s.networks || []) state.networks.set(n.id, n);
    for (const b of s.buffers || []) state.buffers.set(b.id, { unread: 0, mentions: 0, ...b });
    for (const [id, msgs] of Object.entries(s.initial_messages || {})) state.messages.set(+id, msgs as Message[]);
    inferUnreadCounts();
    renderSidebar();
    if (!state.activeId && state.buffers.size) {
      const firstChannel = [...state.buffers.values()].find((b) => b.kind === "channel" && b.joined !== false);
      setActive((firstChannel || state.buffers.values().next().value).id);
    }
    connectWS();
  } catch (err) {
    console.error("hydrate failed", err);
    setTimeout(hydrate, 2000);
  }
}

function connectWS() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(proto + "//" + location.host + "/api/stream");
  state.ws = ws;
  ws.addEventListener("open", () => {
    state.wsReady = true;
    updateInputEnabled();
    maybeMarkActiveRead();
    renderSidebar();
  });
  ws.addEventListener("message", (ev) => {
    try { handleWSMessage(JSON.parse(ev.data)); }
    catch { console.warn("non-json ws frame", ev.data); }
  });
  ws.addEventListener("close", () => {
    state.wsReady = false;
    updateInputEnabled();
    renderSidebar();
    setTimeout(connectWS, 1000);
  });
  ws.addEventListener("error", () => ws.close());
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case "message":
      onMessage(msg);
      break;
    case "buffer_created":
      state.buffers.set(msg.id, {
        id: msg.id,
        network_id: msg.network_id,
        name: msg.name,
        kind: msg.kind,
        joined: true,
        topic: "",
        unread: 0,
        mentions: 0,
        last_seen_id: 0,
      });
      renderSidebar();
      break;
    case "buffer_update":
      onBufferUpdate(msg);
      break;
    case "network_state": {
      const n = state.networks.get(msg.network_id);
      if (n) {
        n.status = msg.state;
        renderSidebar();
        renderHeader();
      }
      break;
    }
    case "history_result":
      onHistoryResult(msg);
      break;
  }
}

function onMessage(msg) {
  const list = state.messages.get(msg.buffer_id) || [];
  const idx = list.findIndex((m) => m.id === msg.id);
  if (idx >= 0) list[idx] = msg; else list.push(msg);
  list.sort((a, b) => a.id - b.id);
  state.messages.set(msg.buffer_id, list);
  const b = state.buffers.get(msg.buffer_id);
  if (b && msg.buffer_id !== state.activeId && msg.id > (b.last_seen_id || 0)) {
    b.unread = (b.unread || 0) + 1;
    if (mentionsMe(msg)) b.mentions = (b.mentions || 0) + 1;
  }
  if (msg.buffer_id === state.activeId) {
    renderActiveView();
    maybeMarkActiveRead();
  }
  renderSidebar();
}

function onBufferUpdate(msg) {
  const b = state.buffers.get(msg.id);
  if (!b) return;
  if (Object.prototype.hasOwnProperty.call(msg, "topic")) b.topic = msg.topic || "";
  if (Object.prototype.hasOwnProperty.call(msg, "joined")) b.joined = !!msg.joined;
  if (Object.prototype.hasOwnProperty.call(msg, "last_seen_id")) b.last_seen_id = msg.last_seen_id || 0;
  inferUnreadCounts();
  renderHeader();
  renderSidebar();
}

function onHistoryResult(msg) {
  const existing = state.messages.get(msg.buffer_id) || [];
  const known = new Set(existing.map((m) => m.id));
  const prepend = (msg.messages || []).filter((m) => !known.has(m.id));
  state.messages.set(msg.buffer_id, [...prepend, ...existing]);
  if (!prepend.length) state.historyExhausted.add(msg.buffer_id);
  state.loadingHistory.delete(msg.buffer_id);
  if (msg.buffer_id === state.activeId) {
    const oldHeight = messagesEl.scrollHeight;
    renderActiveView();
    messagesEl.scrollTop = messagesEl.scrollHeight - oldHeight;
  }
}

function inferUnreadCounts() {
  for (const b of state.buffers.values()) {
    const list = state.messages.get(b.id) || [];
    const lastSeen = b.last_seen_id || 0;
    const unread = list.filter((m) => m.id > lastSeen).length;
    const mentions = list.filter((m) => m.id > lastSeen && mentionsMe(m)).length;
    b.unread = b.id === state.activeId ? 0 : unread;
    b.mentions = b.id === state.activeId ? 0 : mentions;
  }
}

/* =================================================================
   Sidebar
   ================================================================= */

function orderedNetworks() {
  const all = [...state.networks.values()];
  const order = state.layout.order || [];
  const rank = new Map(order.map((id, i) => [id, i]));
  return all.sort((a, b) => {
    const ra = rank.has(a.id) ? rank.get(a.id) : 1e9 + a.id;
    const rb = rank.has(b.id) ? rank.get(b.id) : 1e9 + b.id;
    return ra - rb;
  });
}

function renderSidebar() {
  if (!sbScrollEl) return;
  sbScrollEl.innerHTML = "";

  // Pinned section
  const pinned = (state.layout.pinned || [])
    .map((id) => state.buffers.get(id))
    .filter(Boolean);
  if (pinned.length) {
    const sec = document.createElement("div");
    sec.className = "sb-section";
    const hdr = document.createElement("div");
    hdr.className = "sb-hdr pinned-hdr";
    hdr.innerHTML = '<span class="pinico">⚑</span><span class="title">Pinned</span>';
    sec.appendChild(hdr);
    for (const b of pinned) sec.appendChild(bufferRow(b, { pinned: true }));
    sbScrollEl.appendChild(sec);
  }

  // Networks
  const networks = orderedNetworks();
  for (const n of networks) sbScrollEl.appendChild(networkSection(n));

  // Add a network button
  const add = document.createElement("button");
  add.type = "button";
  add.className = "sb-add";
  add.innerHTML = '<span>+</span><span>Add a network</span>';
  add.style.cssText = "margin:10px 10px 6px;padding:6px 10px;width:calc(100% - 20px);display:flex;align-items:center;gap:8px;color:var(--fg-2);font-family:var(--sans);font-size:12px;border:1px dashed var(--hair-strong);border-radius:5px;";
  sbScrollEl.appendChild(add);
}

function networkSection(n) {
  const sec = document.createElement("div");
  const collapsed = !!state.layout.collapsed[n.id];
  sec.className = [
    "sb-section",
    "netsection",
    state.drag.id === n.id && "dragging",
    state.drag.over === n.id && state.drag.id !== n.id && "dragover",
  ].filter(Boolean).join(" ");
  sec.draggable = true;
  sec.addEventListener("dragstart", (e) => {
    state.drag.id = n.id;
    try { e.dataTransfer.setData("text/plain", String(n.id)); e.dataTransfer.effectAllowed = "move"; } catch {}
    sec.classList.add("dragging");
  });
  sec.addEventListener("dragend", () => {
    state.drag.id = null;
    state.drag.over = null;
    renderSidebar();
  });
  sec.addEventListener("dragover", (e) => {
    if (state.drag.id == null || state.drag.id === n.id) return;
    e.preventDefault();
    if (state.drag.over !== n.id) { state.drag.over = n.id; renderSidebar(); }
  });
  sec.addEventListener("dragleave", () => {
    if (state.drag.over === n.id) { state.drag.over = null; }
  });
  sec.addEventListener("drop", (e) => {
    e.preventDefault();
    const fromId = state.drag.id;
    if (fromId != null && fromId !== n.id) reorderNetwork(fromId, n.id);
    state.drag.id = null;
    state.drag.over = null;
    renderSidebar();
  });

  const netBufs = [...state.buffers.values()].filter((b) => b.network_id === n.id);
  const statusB = netBufs.find((b) => b.kind === "status");
  const channels = netBufs.filter((b) => b.kind === "channel" && b.joined !== false).sort(byName);
  const queries = netBufs.filter((b) => b.kind === "query").sort(byName);
  const parted = netBufs.filter((b) => b.kind === "channel" && b.joined === false).sort(byName);
  const headerActive = statusB && state.activeId === statusB.id;
  const dot = dotClass(n.status);
  const unreadTotal = netBufs.reduce((s, b) => s + (b.unread || 0), 0);
  const mentionTotal = netBufs.reduce((s, b) => s + (b.mentions || 0), 0);

  const hdr = document.createElement("button");
  hdr.type = "button";
  hdr.className = ["sb-hdr", "net-hdr", headerActive && "active", collapsed && "collapsed", dot].filter(Boolean).join(" ");
  hdr.title = `${n.host || ""} · ${n.status || "offline"} · click to show server log`;

  const caret = document.createElement("span");
  caret.className = "caret";
  caret.textContent = collapsed ? "▸" : "▾";
  caret.addEventListener("click", (e) => {
    e.stopPropagation();
    state.layout.collapsed[n.id] = !collapsed;
    saveLayout(state.layout);
    renderSidebar();
  });
  const grip = document.createElement("span");
  grip.className = "grip";
  grip.title = "Drag to reorder";
  grip.textContent = "⋮⋮";
  const name = document.createElement("span");
  name.className = "netname";
  name.textContent = n.name;
  const actions = document.createElement("span");
  actions.className = "netactions";
  if (collapsed && mentionTotal > 0) {
    actions.appendChild(badge("mentionbadge", mentionTotal));
  } else if (collapsed && unreadTotal > 0) {
    actions.appendChild(badge("unreadbadge", unreadTotal));
  }
  hdr.append(caret, grip, name, actions);
  hdr.addEventListener("click", () => { if (statusB) setActive(statusB.id); });
  sec.appendChild(hdr);

  if (!collapsed) {
    for (const b of channels) sec.appendChild(bufferRow(b));
    for (const b of queries) sec.appendChild(bufferRow(b));
    if (parted.length) {
      const arch = document.createElement("button");
      arch.type = "button";
      arch.className = "sbrow archives";
      const nm = document.createElement("span");
      nm.className = "name"; nm.textContent = "Archives";
      const cnt = document.createElement("span");
      cnt.className = "archcount"; cnt.textContent = String(parted.length);
      arch.append(nm, cnt);
      arch.addEventListener("click", () => { if (statusB) setActive(statusB.id); });
      sec.appendChild(arch);
    }
  }

  return sec;
}

function bufferRow(b, opts: { pinned?: boolean } = {}) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = [
    "sbrow", "chan", b.kind,
    opts.pinned && "pinned",
    b.id === state.activeId && "active",
    b.unread > 0 && "unread",
    b.mentions > 0 && "mention",
    b.kind === "channel" && b.joined === false && "parted",
  ].filter(Boolean).join(" ");
  const display = b.kind === "status" ? "(status)" : b.name;
  const nm = document.createElement("span");
  nm.className = "name";
  nm.textContent = display;
  btn.appendChild(nm);
  if (b.mentions > 0) btn.appendChild(badge("mentionbadge", b.mentions));
  else if (b.unread > 0) btn.appendChild(badge("unreadbadge", b.unread));
  btn.addEventListener("click", () => setActive(b.id));
  return btn;
}

function badge(cls, n) {
  const el = document.createElement("span");
  el.className = `badge ${cls}`;
  el.textContent = n > 99 ? "99+" : String(n);
  return el;
}

function reorderNetwork(fromId, toId) {
  const ids = orderedNetworks().map((n) => n.id);
  const fromIdx = ids.indexOf(fromId);
  const toIdx = ids.indexOf(toId);
  if (fromIdx < 0 || toIdx < 0) return;
  const [moved] = ids.splice(fromIdx, 1);
  ids.splice(toIdx, 0, moved);
  state.layout.order = ids;
  saveLayout(state.layout);
}

/* =================================================================
   Header / Messages / Status view
   ================================================================= */

function renderHeader() {
  const b = state.buffers.get(state.activeId);
  if (!b) return;
  const isChannel = b.kind === "channel";
  bufferNameEl.innerHTML = "";
  if (isChannel) {
    const hash = document.createElement("span");
    hash.className = "hash";
    hash.textContent = "#";
    bufferNameEl.append(hash, document.createTextNode(b.name.replace(/^#/, "")));
  } else if (b.kind === "status") {
    const n = state.networks.get(b.network_id);
    bufferNameEl.textContent = n ? `${n.name} (status)` : "(status)";
  } else {
    bufferNameEl.textContent = b.name;
  }

  bufferTopicEl.innerHTML = "";
  const topicText = document.createElement("span");
  topicText.className = "topictext";
  if (b.kind === "status") {
    const n = state.networks.get(b.network_id);
    topicText.textContent = n ? `${n.host || ""} · ${n.status || "offline"}` : "";
  } else {
    topicText.textContent = b.topic || "No topic set";
    if (!b.topic) topicText.style.color = "var(--fg-3)";
  }
  bufferTopicEl.appendChild(topicText);
  if (b.topic_set_by) {
    const setter = document.createElement("span");
    setter.style.cssText = "color:var(--fg-3);font-family:var(--mono);font-size:11px;margin-left:6px;";
    setter.textContent = `— ${b.topic_set_by}`;
    bufferTopicEl.appendChild(setter);
  }
  const edit = document.createElement("span");
  edit.className = "edit";
  edit.setAttribute("aria-hidden", "true");
  edit.innerHTML = '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M2 14l3.5-1L13 5.5 10.5 3 3 10.5 2 14z" stroke-linejoin="round"/><path d="M9.5 4L12 6.5"/></svg>';
  bufferTopicEl.appendChild(edit);

  inputEl.placeholder = `Message ${b.name}`;
}

function renderActiveView() {
  const b = state.buffers.get(state.activeId);
  if (!b) return;
  if (b.kind === "status") {
    messagesEl.hidden = true;
    statusViewEl.hidden = false;
    renderStatusView();
  } else {
    statusViewEl.hidden = true;
    messagesEl.hidden = false;
    renderMessages();
  }
}

function renderStatusView() {
  statusViewEl.innerHTML = "";
  const b = state.buffers.get(state.activeId);
  if (!b) return;
  const wrap = document.createElement("div");
  wrap.className = "statuslines";
  const list = state.messages.get(b.id) || [];
  for (const m of list) wrap.appendChild(statusLine(m));
  if (!list.length) {
    const n = state.networks.get(b.network_id);
    const empty = document.createElement("div");
    empty.innerHTML = `<span class="stts">—</span><span class="stcat ok">ok</span><span class="stdim">${escapeHTML(n ? `connected to ${n.host || n.name}` : "no log entries yet")}</span>`;
    wrap.appendChild(empty);
  }
  statusViewEl.appendChild(wrap);
  statusViewEl.scrollTop = statusViewEl.scrollHeight;
}

function statusLine(m) {
  const row = document.createElement("div");
  const ts = document.createElement("span");
  ts.className = "stts";
  ts.textContent = formatTime(m.ts);
  const cat = document.createElement("span");
  let catText = "-->", catCls = "";
  if (m.kind === "connected") { catText = "OK"; catCls = "ok"; }
  else if (m.kind === "disconnected" || m.kind === "error") { catText = "ERR"; catCls = "bad"; }
  else if (m.kind === "notice") { catText = "<--"; }
  cat.className = `stcat ${catCls}`.trim();
  cat.textContent = catText;
  const body = document.createElement("span");
  body.className = "stdim";
  body.textContent = (m.sender ? `${m.sender} ` : "") + (m.content || m.kind || "");
  row.append(ts, cat, body);
  return row;
}

function renderMessages() {
  messagesEl.innerHTML = "";
  const list = state.messages.get(state.activeId) || [];
  const b = state.buffers.get(state.activeId);
  const lastSeen = b?.last_seen_id || 0;
  let unreadInserted = false;
  let lastDayKey = null;

  for (const m of list) {
    const dayKey = dayKeyOf(m.ts);
    if (dayKey && dayKey !== lastDayKey) {
      messagesEl.appendChild(daySeparator(m.ts));
      lastDayKey = dayKey;
    }
    if (!unreadInserted && m.id > lastSeen && state.activeId !== null && lastSeen > 0) {
      const bar = document.createElement("div");
      bar.className = "unreadbar";
      const label = document.createElement("span");
      label.textContent = "New messages";
      bar.appendChild(label);
      messagesEl.appendChild(bar);
      unreadInserted = true;
    }
    messagesEl.appendChild(messageRow(m));
  }
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

function messageRow(m) {
  const row = document.createElement("div");
  row.dataset.id = m.id;
  const kind = classifyKind(m.kind);
  const isMention = mentionsMe(m);
  const self = isSelf(m);
  row.className = `msg ${kind === "message" ? "flat" : kind}${isMention ? " mention" : ""}${self ? " self" : ""}`;

  const ts = document.createElement("span");
  ts.className = "ts";
  ts.textContent = formatTime(m.ts);
  const gutter = document.createElement("span");
  gutter.className = "gutter";
  const body = document.createElement("span");
  body.className = "body";

  if (kind === "sys") {
    const arrowCls = m.kind === "join" ? "in" : (m.kind === "part" || m.kind === "quit") ? "out" : "nil";
    const glyph = m.kind === "join" ? "→" : (m.kind === "part" || m.kind === "quit") ? "←" : "·";
    gutter.innerHTML = `<span class="arrow ${arrowCls}">${glyph}</span>`;
    body.innerHTML = sysBodyHTML(m);
    row.append(ts, gutter, body);
    return row;
  }
  if (kind === "notice") {
    gutter.textContent = "!";
  } else if (kind === "action") {
    gutter.textContent = "*";
  } else {
    gutter.textContent = "H";
  }

  const nick = document.createElement("span");
  nick.className = `nick ${nickClass(m.sender || "")}`;
  nick.textContent = kind === "notice" ? `-${m.sender || "*"}-` : (m.sender || "*");
  body.innerHTML = renderBodyHTML(m);
  if (kind === "action") {
    body.innerHTML = `${escapeHTML(m.sender || "")} ${renderBodyHTML(m)}`;
  }

  row.append(ts, gutter, nick, body);
  return row;
}

function classifyKind(kind) {
  if (["join", "part", "quit", "nick", "kick", "mode", "topic", "connected", "disconnected"].includes(kind)) return "sys";
  if (kind === "notice") return "notice";
  if (kind === "action") return "action";
  return "message";
}

function renderBodyHTML(m) {
  const body = m.content || "";
  return highlightMentions(linkify(inlineCode(escapeHTML(body))));
}

function sysBodyHTML(m) {
  const sender = `<span class="nickref ${nickClass(m.sender || "")}">${escapeHTML(m.sender || "")}</span>`;
  const extra = m.content ? ` (${escapeHTML(m.content)})` : "";
  switch (m.kind) {
    case "join": return `${sender} joined`;
    case "part": return `${sender} left${extra}`;
    case "quit": return `${sender} quit${extra}`;
    case "nick": return m.target
      ? `${sender} is now known as <span class="nickref ${nickClass(m.target)}">${escapeHTML(m.target)}</span>`
      : escapeHTML(m.content || "nick change");
    case "kick": return `<span class="nickref ${nickClass(m.target || "")}">${escapeHTML(m.target || "")}</span> was kicked by ${sender}${extra}`;
    case "mode": return `${sender} set mode ${escapeHTML(m.content || "")}${m.target ? ` on ${escapeHTML(m.target)}` : ""}`;
    case "topic": return `${sender} set topic: ${escapeHTML(m.content || "")}`;
    case "connected": return "connected";
    case "disconnected": return `disconnected${extra}`;
    default: return escapeHTML(m.content || "");
  }
}

function dayKeyOf(ts) {
  if (!ts) return null;
  const d = new Date(ts);
  if (isNaN(d.getTime())) return null;
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

function daySeparator(ts) {
  const sep = document.createElement("div");
  sep.className = "daysep";
  const label = document.createElement("span");
  const d = new Date(ts);
  const today = new Date();
  const isToday = d.toDateString() === today.toDateString();
  const yesterday = new Date(today); yesterday.setDate(today.getDate() - 1);
  const isYesterday = d.toDateString() === yesterday.toDateString();
  const dateLabel = d.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" });
  label.textContent = isToday ? `Today · ${dateLabel}` : isYesterday ? `Yesterday · ${dateLabel}` : dateLabel;
  sep.appendChild(label);
  return sep;
}

/* =================================================================
   Members
   ================================================================= */

function renderMembers() {
  const active = state.buffers.get(state.activeId);
  const isChannel = active && active.kind === "channel";
  const showPane = state.tweaks.showMemberList && isChannel;
  memberPaneEl.dataset.hidden = showPane ? "false" : "true";
  toggleMembersEl.classList.toggle("toggled", state.tweaks.showMemberList);

  const members = isChannel ? membersForActive() : [];
  memberCountEl.textContent = String(members.length);
  memberCountInlineEl.textContent = String(members.length);
  bufferMemcountEl.hidden = !isChannel;

  memberListEl.innerHTML = "";
  if (!showPane) return;

  const groups = [
    ["ops", members.filter((m) => m.prefix === "@")],
    ["half-ops", members.filter((m) => m.prefix === "%")],
    ["voice", members.filter((m) => m.prefix === "+")],
    ["regulars", members.filter((m) => !m.prefix)],
  ];
  for (const [name, items] of groups) {
    if (!items.length) continue;
    const h = document.createElement("div");
    h.className = "mgroup";
    h.textContent = `${name} · ${items.length}`;
    memberListEl.appendChild(h);
    for (const m of items) memberListEl.appendChild(memberRow(m));
  }
}

function memberRow(m) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = `member ${m.away ? "away" : ""} ${m.self ? "me" : ""}`.trim();
  const cls = m.prefix === "@" ? "op" : m.prefix === "%" ? "hop" : m.prefix === "+" ? "voice" : "none";
  const pfx = document.createElement("span");
  pfx.className = `pfx ${cls}`;
  pfx.textContent = m.prefix || "\u00a0";
  const mn = document.createElement("span");
  mn.className = `mn ${nickClass(m.nick)}`;
  mn.textContent = m.nick;
  btn.append(pfx, mn);
  return btn;
}

function populateMembersForActive() {
  const b = state.buffers.get(state.activeId);
  state.members.clear();
  if (!b || b.kind !== "channel") return;
  const names = new Set<string>();
  for (const m of state.messages.get(b.id) || []) {
    if (m.sender) names.add(m.sender);
    if (m.target && ["kick", "nick"].includes(m.kind)) names.add(m.target);
  }
  if (state.me.nick) names.add(state.me.nick);
  const sorted = [...names].sort((a, b) => a.localeCompare(b));
  for (const nick of sorted) {
    state.members.set(nick, {
      nick,
      prefix: nick === state.me.nick ? "+" : "",
      away: false,
      self: nick === state.me.nick,
    });
  }
}

function membersForActive() {
  return [...state.members.values()];
}

/* =================================================================
   Active buffer
   ================================================================= */

function setActive(id) {
  state.activeId = id;
  inferUnreadCounts();
  populateMembersForActive();
  renderSidebar();
  renderHeader();
  renderActiveView();
  renderMembers();
  updateInputEnabled();
  maybeMarkActiveRead();
  inputEl.focus();
}

function dotClass(status) {
  if (status === "connected") return "on";
  if (status === "connecting") return "warn";
  return "bad";
}

function mentionsMe(m) {
  if (!state.me.nick || !m.content) return false;
  return new RegExp(`\\b${escapeRegExp(state.me.nick)}\\b`, "i").test(m.content);
}

function isSelf(m) {
  return (m.sender || "").toLowerCase() === (state.me.nick || "").toLowerCase();
}

function nickClass(nick) {
  let h = 5381;
  const s = String(nick || "").toLowerCase();
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
  return `n${Math.abs(h) % 10}`;
}

function linkify(html) {
  return html.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noreferrer">$1</a>');
}

function inlineCode(html) {
  return html.replace(/`([^`]+)`/g, "<code>$1</code>");
}

function highlightMentions(html) {
  if (!state.me.nick) return html;
  const re = new RegExp(`\\b(${escapeRegExp(state.me.nick)})\\b`, "gi");
  return html.replace(re, '<span class="selfmention">$1</span>');
}

function formatTime(iso) {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  if (!state.tweaks.showSeconds) return `${hh}:${mm}`;
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

function byName(a: Buffer, b: Buffer) { return a.name.localeCompare(b.name); }

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeRegExp(s) {
  return String(s).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function onMessagesScroll() {
  if (messagesEl.scrollTop <= 40) loadOlderHistory();
  maybeMarkActiveRead();
}

function loadOlderHistory() {
  const bufferId = state.activeId;
  if (!bufferId || !state.wsReady || state.loadingHistory.has(bufferId) || state.historyExhausted.has(bufferId)) return;
  const list = state.messages.get(bufferId) || [];
  if (!list.length) return;
  state.loadingHistory.add(bufferId);
  sendCmd({ type: "history", buffer_id: bufferId, before: list[0].id, limit: 100 });
}

function maybeMarkActiveRead() {
  const b = state.buffers.get(state.activeId);
  const list = state.messages.get(state.activeId) || [];
  if (!state.wsReady || !b || !list.length) return;
  const lastId = list[list.length - 1].id;
  const current = b.last_seen_id || 0;
  const sent = state.lastMarkedReadId.get(b.id) || 0;
  if (lastId <= current || lastId <= sent) return;
  b.last_seen_id = lastId;
  b.unread = 0;
  b.mentions = 0;
  state.lastMarkedReadId.set(b.id, lastId);
  renderSidebar();
  sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
}

function updateInputEnabled() {
  const b = state.buffers.get(state.activeId);
  inputEl.disabled = !(state.wsReady && b && b.kind !== "status" && !(b.kind === "channel" && b.joined === false));
}

function onSubmit(ev: SubmitEvent) {
  ev.preventDefault();
  const text = inputEl.value.trim();
  if (!text || !state.wsReady) return;
  const buf = state.buffers.get(state.activeId);
  if (!buf) return;
  if (text.startsWith("/")) {
    if (handleSlashCommand(text, buf)) {
      inputEl.value = "";
      updateCmdPop();
    }
    return;
  }
  sendCmd({ type: "send", buffer_id: buf.id, content: text });
  inputEl.value = "";
  updateCmdPop();
}

function handleSlashCommand(text: string, buf: Buffer) {
  const [cmd, ...rest] = text.slice(1).split(/\s+/);
  switch ((cmd || "").toLowerCase()) {
    case "join":
      sendCmd({ type: "join", network_id: buf.network_id, channel: rest.join(" ").trim() });
      return true;
    case "part":
      sendCmd({ type: "part", buffer_id: buf.id, content: rest.join(" ").trim() });
      return true;
    default:
      return false;
  }
}

function initCmdPop() {
  inputEl.addEventListener("input", updateCmdPop);
  inputEl.addEventListener("blur", () => setTimeout(() => { cmdPopEl.hidden = true; }, 100));
}

function updateCmdPop() {
  const v = inputEl.value;
  if (!v.startsWith("/")) {
    cmdPopEl.hidden = true;
    return;
  }
  const q = v.slice(1).split(/\s+/)[0].toLowerCase();
  const matches = SLASH_COMMANDS.filter((c) => c.cmd.slice(1).startsWith(q));
  if (!matches.length) { cmdPopEl.hidden = true; return; }
  cmdPopEl.innerHTML = "";
  matches.forEach((c, i) => {
    const row = document.createElement("div");
    row.className = `ci ${i === 0 ? "hl" : ""}`;
    row.innerHTML = `<span class="c">${c.cmd} <span style="color:var(--fg-2);font-weight:400">${escapeHTML(c.args)}</span></span><span class="d">${escapeHTML(c.desc)}</span>`;
    cmdPopEl.appendChild(row);
  });
  cmdPopEl.hidden = false;
}

let reqSeq = 0;
function sendCmd(cmd) {
  if (!state.ws) return;
  cmd.req_id = cmd.req_id || `r${++reqSeq}`;
  state.ws.send(JSON.stringify(cmd));
}

/* =================================================================
   Persistence
   ================================================================= */

function loadTweaks(): TweakSettings {
  try {
    const saved = JSON.parse(localStorage.getItem(TWEAKS_KEY) || "{}");
    return { ...DEFAULT_TWEAKS, ...saved };
  } catch { return { ...DEFAULT_TWEAKS }; }
}
function saveTweaks(t: TweakSettings) { try { localStorage.setItem(TWEAKS_KEY, JSON.stringify(t)); } catch {} }

function loadLayout(): LayoutSettings {
  try {
    const saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || "{}");
    return { ...DEFAULT_LAYOUT, ...saved };
  } catch { return { ...DEFAULT_LAYOUT }; }
}
function saveLayout(l: LayoutSettings) { try { localStorage.setItem(LAYOUT_KEY, JSON.stringify(l)); } catch {} }

function applyTweaks(t: TweakSettings) {
  document.documentElement.dataset.accent = t.accent;
  document.documentElement.dataset.density = t.density;
}

function initTweaksPanel() {
  tweaksTabEl.addEventListener("click", () => {
    tweaksPanelEl.hidden = false;
    tweaksTabEl.hidden = true;
  });
  tweaksCloseEl.addEventListener("click", () => {
    tweaksPanelEl.hidden = true;
    tweaksTabEl.hidden = false;
  });
  for (const seg of tweaksPanelEl.querySelectorAll<HTMLElement>(".seg")) {
    const key = seg.dataset.tweak as keyof TweakSettings;
    for (const btn of seg.querySelectorAll<HTMLButtonElement>("button")) {
      btn.addEventListener("click", () => {
        state.tweaks[key] = btn.dataset.value as never;
        saveTweaks(state.tweaks);
        applyTweaks(state.tweaks);
        syncTweaksPanel();
      });
    }
  }
  for (const sw of tweaksPanelEl.querySelectorAll<HTMLElement>(".switch")) {
    const key = sw.dataset.tweak as keyof TweakSettings;
    sw.addEventListener("click", () => {
      state.tweaks[key] = !state.tweaks[key] as never;
      saveTweaks(state.tweaks);
      applyTweaks(state.tweaks);
      renderMembers();
      if (state.activeId) renderActiveView();
      syncTweaksPanel();
    });
  }
  syncTweaksPanel();
}

function syncTweaksPanel() {
  for (const seg of tweaksPanelEl.querySelectorAll<HTMLElement>(".seg")) {
    const key = seg.dataset.tweak as keyof TweakSettings;
    for (const btn of seg.querySelectorAll<HTMLButtonElement>("button")) {
      btn.classList.toggle("sel", state.tweaks[key] === btn.dataset.value);
    }
  }
  for (const sw of tweaksPanelEl.querySelectorAll<HTMLElement>(".switch")) {
    const key = sw.dataset.tweak as keyof TweakSettings;
    sw.classList.toggle("on", !!state.tweaks[key]);
  }
}

init();
