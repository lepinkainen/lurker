// Minimal IRC client UI.
//
// Data flow:
//   1. fetch /api/state to hydrate networks/buffers/messages
//   2. open /api/stream (WebSocket) for live updates
//   3. user actions → WS commands; server echoes + messages flow back.

const state = {
  networks: new Map(), // id -> {id,name,host,...,status}
  buffers: new Map(), // id -> {id,network_id,name,kind,topic,...}
  messages: new Map(), // buffer_id -> [message,...] (oldest → newest)
  activeId: null,
  pendingReqs: new Map(), // req_id -> resolver
  ws: null,
  wsReady: false,
  loadingHistory: new Set(),
  historyExhausted: new Set(),
  searchResults: [],
  lastMarkedReadId: new Map(),
};

const el = (id) => document.getElementById(id);
const sidebarEl = el("buffer-list");
const messagesEl = el("messages");
const bufferNameEl = el("buffer-name");
const bufferTopicEl = el("buffer-topic");
const inputEl = el("input");
const inputForm = el("input-form");
const connDot = el("conn-dot");
const searchForm = el("search-form");
const searchInput = el("search-input");
const searchResultsEl = el("search-results");
const searchStatusEl = el("search-status");

function init() {
  inputForm.addEventListener("submit", onSubmit);
  messagesEl.addEventListener("scroll", onMessagesScroll);
  if (searchForm) searchForm.addEventListener("submit", onSearch);
  hydrate();
}

async function hydrate() {
  try {
    const res = await fetch("/api/state");
    if (!res.ok) throw new Error("state " + res.status);
    const s = await res.json();
    for (const n of s.networks || []) state.networks.set(n.id, n);
    for (const b of s.buffers || []) state.buffers.set(b.id, b);
    for (const [id, msgs] of Object.entries(s.initial_messages || {})) {
      state.messages.set(+id, msgs);
    }
    renderSidebar();
    if (!state.activeId && state.buffers.size) {
      const firstChannel = [...state.buffers.values()].find((b) => b.kind === "channel");
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
  const url = proto + "//" + location.host + "/api/stream";
  const ws = new WebSocket(url);
  state.ws = ws;
  ws.addEventListener("open", () => {
    state.wsReady = true;
    connDot.classList.remove("offline");
    connDot.classList.add("online");
    connDot.title = "connected";
    updateInputEnabled();
    maybeMarkActiveRead();
  });
  ws.addEventListener("message", (ev) => {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch (e) {
      console.warn("non-json ws frame", ev.data);
      return;
    }
    handleWSMessage(msg);
  });
  ws.addEventListener("close", () => {
    state.wsReady = false;
    connDot.classList.remove("online");
    connDot.classList.add("offline");
    connDot.title = "disconnected";
    updateInputEnabled();
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
        created_at: msg.created_at,
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
      }
      break;
    }
    case "history_result":
      onHistoryResult(msg);
      break;
    case "ack":
      break;
    case "error":
      console.warn("server error", msg.message, "req_id=", msg.req_id);
      break;
    default:
      console.debug("unknown ws type", msg.type, msg);
  }
}

function onMessage(msg) {
  const list = state.messages.get(msg.buffer_id) || [];
  const idx = list.findIndex((m) => m.id === msg.id || (m.id < 0 && sameRenderedMessage(m, msg)));
  if (idx >= 0) {
    list[idx] = msg;
  } else {
    list.push(msg);
  }
  list.sort((a, b) => a.id - b.id);
  state.messages.set(msg.buffer_id, list);
  if (msg.buffer_id === state.activeId) {
    renderMessages();
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
  renderSidebar();
  if (msg.id === state.activeId) {
    bufferTopicEl.textContent = b.topic || "";
    updateInputEnabled();
  }
}

function onHistoryResult(msg) {
  const existing = state.messages.get(msg.buffer_id) || [];
  const knownIds = new Set(existing.map((m) => m.id));
  const prepend = msg.messages.filter((m) => !knownIds.has(m.id));
  state.messages.set(msg.buffer_id, [...prepend, ...existing]);
  if (!prepend.length) state.historyExhausted.add(msg.buffer_id);
  state.loadingHistory.delete(msg.buffer_id);
  if (msg.buffer_id === state.activeId) {
    const oldHeight = messagesEl.scrollHeight;
    renderMessages();
    const newHeight = messagesEl.scrollHeight;
    messagesEl.scrollTop = newHeight - oldHeight;
  }
}

function renderSidebar() {
  const byNetwork = new Map();
  for (const b of state.buffers.values()) {
    if (!byNetwork.has(b.network_id)) byNetwork.set(b.network_id, []);
    byNetwork.get(b.network_id).push(b);
  }
  sidebarEl.innerHTML = "";
  for (const n of [...state.networks.values()].sort((a, b) => a.id - b.id)) {
    const bufs = (byNetwork.get(n.id) || []).sort((a, b) => {
      if (a.kind !== b.kind) {
        const order = { status: 0, channel: 1, query: 2 };
        return (order[a.kind] ?? 99) - (order[b.kind] ?? 99);
      }
      return a.name.localeCompare(b.name);
    });
    const block = document.createElement("div");
    block.className = "network-block";
    const h = document.createElement("div");
    h.className = "network-header";
    h.innerHTML = `<span>${escapeHTML(n.name)}</span>`;
    const dot = document.createElement("span");
    dot.className = "dot " + networkDotClass(n.status);
    h.appendChild(dot);
    block.appendChild(h);
    for (const b of bufs) {
      const item = document.createElement("div");
      item.className = "buffer-item";
      if (b.id === state.activeId) item.classList.add("active");
      if (hasUnread(b.id)) item.classList.add("unread");
      if (b.joined === false && b.kind === "channel") item.classList.add("inactive");
      item.innerHTML = `<span>${escapeHTML(displayName(b))}</span><span class="kind">${bufferMeta(b)}</span>`;
      item.addEventListener("click", () => setActive(b.id));
      block.appendChild(item);
    }
    sidebarEl.appendChild(block);
  }
}

function networkDotClass(status) {
  if (status === "connected") return "online";
  if (status === "connecting") return "connecting";
  return "offline";
}

function bufferMeta(b) {
  if (b.kind === "channel") return b.joined === false ? "parted" : "channel";
  return b.kind;
}

function hasUnread(bufferId) {
  if (bufferId === state.activeId) return false;
  const b = state.buffers.get(bufferId);
  const list = state.messages.get(bufferId) || [];
  if (!b || !list.length) return false;
  return list[list.length - 1].id > (b.last_seen_id || 0);
}

function displayName(b) {
  if (b.kind === "status") return "(status)";
  return b.name;
}

function setActive(id) {
  state.activeId = id;
  const b = state.buffers.get(id);
  bufferNameEl.textContent = b ? displayName(b) : "—";
  bufferTopicEl.textContent = (b && b.topic) || "";
  renderSidebar();
  renderMessages();
  updateInputEnabled();
  maybeMarkActiveRead();
  inputEl.focus();
}

function renderMessages() {
  messagesEl.innerHTML = "";
  const list = state.messages.get(state.activeId) || [];
  const frag = document.createDocumentFragment();
  for (const m of list) frag.appendChild(messageRow(m));
  messagesEl.appendChild(frag);
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

function messageRow(m) {
  const row = document.createElement("div");
  row.className = "msg " + m.kind;
  row.dataset.id = m.id;
  const ts = formatTime(m.ts);
  row.innerHTML = `
    <span class="ts">${ts}</span>
    <span class="sender"></span>
    <span class="content"></span>`;
  row.querySelector(".sender").textContent = senderLabel(m);
  row.querySelector(".content").textContent = renderBody(m);
  return row;
}

function senderLabel(m) {
  switch (m.kind) {
    case "join":
    case "part":
    case "quit":
    case "nick":
    case "kick":
    case "mode":
    case "topic":
    case "connected":
    case "disconnected":
      return "—";
    default:
      return m.sender;
  }
}

function renderBody(m) {
  switch (m.kind) {
    case "join":
      return `${m.sender} joined`;
    case "part":
      return `${m.sender} left${m.content ? " (" + m.content + ")" : ""}`;
    case "quit":
      return `${m.sender} quit${m.content ? " (" + m.content + ")" : ""}`;
    case "nick":
      return `${m.sender} is now known as ${m.target}`;
    case "kick":
      return `${m.target} was kicked by ${m.sender}${m.content ? " (" + m.content + ")" : ""}`;
    case "mode":
      return `${m.sender} set mode ${m.content}${m.target ? " on " + m.target : ""}`;
    case "topic":
      return `${m.sender} set topic: ${m.content}`;
    case "connected":
      return "connected";
    case "disconnected":
      return "disconnected" + (m.content ? " (" + m.content + ")" : "");
    default:
      return m.content;
  }
}

function appendMessageRow(m, stickToBottom) {
  messagesEl.appendChild(messageRow(m));
  if (stickToBottom) messagesEl.scrollTop = messagesEl.scrollHeight;
}

function sameRenderedMessage(a, b) {
  return a.kind === b.kind && a.sender === b.sender && a.content === b.content && a.buffer_id === b.buffer_id;
}

function atBottom() {
  const gap = messagesEl.scrollHeight - messagesEl.scrollTop - messagesEl.clientHeight;
  return gap < 40;
}

function updateInputEnabled() {
  const b = state.buffers.get(state.activeId);
  inputEl.disabled = !(state.wsReady && b && (b.kind !== "status") && !(b.kind === "channel" && b.joined === false));
}

function formatTime(iso) {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso || "";
  return (
    String(d.getHours()).padStart(2, "0") +
    ":" +
    String(d.getMinutes()).padStart(2, "0") +
    ":" +
    String(d.getSeconds()).padStart(2, "0")
  );
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function onMessagesScroll() {
  if (messagesEl.scrollTop <= 40) {
    loadOlderHistory();
  }
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
  state.lastMarkedReadId.set(b.id, lastId);
  renderSidebar();
  sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
}

function onSubmit(ev) {
  ev.preventDefault();
  const text = inputEl.value.trim();
  if (!text || !state.wsReady) return;
  const buf = state.buffers.get(state.activeId);
  if (!buf) return;
  if (text.startsWith("/")) {
    if (handleSlashCommand(text, buf)) {
      inputEl.value = "";
    }
    return;
  }
  sendCmd({ type: "send", buffer_id: buf.id, content: text });
  inputEl.value = "";
}

function handleSlashCommand(text, buf) {
  const [cmd, ...rest] = text.slice(1).split(/\s+/);
  switch ((cmd || "").toLowerCase()) {
    case "join": {
      const channel = rest.join(" ").trim();
      if (!channel) return true;
      sendCmd({ type: "join", network_id: buf.network_id, channel });
      return true;
    }
    case "part": {
      if (buf.kind !== "channel") return true;
      const reason = rest.join(" ").trim();
      sendCmd({ type: "part", buffer_id: buf.id, content: reason });
      return true;
    }
    default:
      return false;
  }
}

async function onSearch(ev) {
  ev.preventDefault();
  const q = (searchInput.value || "").trim();
  if (!q) {
    state.searchResults = [];
    renderSearchResults();
    return;
  }
  searchStatusEl.textContent = "searching…";
  const params = new URLSearchParams({ q });
  if (state.activeId) params.set("buffer", String(state.activeId));
  const res = await fetch("/api/search?" + params.toString());
  const body = await res.json();
  state.searchResults = body.results || [];
  searchStatusEl.textContent = `${state.searchResults.length} result(s)`;
  renderSearchResults();
}

function renderSearchResults() {
  if (!searchResultsEl) return;
  searchResultsEl.innerHTML = "";
  for (const r of state.searchResults) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "search-result";
    row.innerHTML = `<span class="search-meta">${escapeHTML(formatTime(r.ts))} · ${escapeHTML(r.sender || "*")}</span><span class="search-body">${escapeHTML(r.content || renderBody(r))}</span>`;
    row.addEventListener("click", async () => {
      if (state.activeId !== r.buffer_id) setActive(r.buffer_id);
      await ensureMessageVisible(r.buffer_id, r.id);
      scrollToMessage(r.id);
    });
    searchResultsEl.appendChild(row);
  }
}

async function ensureMessageVisible(bufferId, messageId) {
  let list = state.messages.get(bufferId) || [];
  let guard = 0;
  while (!list.some((m) => m.id === messageId) && guard < 20) {
    if (!list.length) break;
    const before = list[0].id;
    const res = await fetch(`/api/buffers/${bufferId}/history?before=${before}&limit=200`);
    if (!res.ok) break;
    const body = await res.json();
    const prepend = (body.messages || []).filter((m) => !list.some((existing) => existing.id === m.id));
    if (!prepend.length) break;
    list = [...prepend, ...list];
    state.messages.set(bufferId, list);
    guard += 1;
  }
  if (bufferId === state.activeId) renderMessages();
}

function scrollToMessage(messageId) {
  const node = messagesEl.querySelector(`[data-id="${messageId}"]`);
  if (!node) return;
  node.scrollIntoView({ block: "center" });
  node.classList.add("flash");
  setTimeout(() => node.classList.remove("flash"), 1200);
}

let reqSeq = 0;
function sendCmd(cmd) {
  cmd.req_id = cmd.req_id || "r" + ++reqSeq;
  state.ws.send(JSON.stringify(cmd));
}

init();
