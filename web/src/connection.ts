import { type Member, type Message, type StateResponse, state, type UpdateStatus } from "./app-state";

export const RECONNECT_BASE_MS = 1000;
export const RECONNECT_MAX_MS = 30000;

export type ConnectionDeps = {
  domReady: () => boolean;
  renderSidebarStatus: () => void;
  renderPromptNick: () => void;
  renderSidebar: () => void;
  renderHeader: () => void;
  renderActiveView: () => void;
  renderMembers: () => void;
  updateInputEnabled: () => void;
  maybeMarkActiveRead: () => void;
  inferUnreadCounts: () => void;
  scheduleReconnect: (delayMs: number) => void;
  nextReconnectDelay: () => number;
  setActive: (id: number, opts?: { skipHash?: boolean; replaceHash?: boolean }) => void;
  bufferFromHash: (hash: string) => { id: number } | null;
  handleWSMessage: (msg: unknown) => void;
};

export async function hydrate(deps: ConnectionDeps) {
  try {
    state.backendStatus = "connecting";
    deps.renderSidebarStatus();
    const [stateRes, updateRes] = await Promise.all([
      fetch("/api/state"),
      fetch("/api/update-status", { headers: { Accept: "application/json" } }).catch(() => null),
    ]);
    if (!stateRes.ok) throw new Error(`state ${stateRes.status}`);
    const s: StateResponse = await stateRes.json();
    state.me.nick = s.current_nick || s.nick || s.user?.nick || s.networks?.[0]?.nick || "you";
    for (const network of s.networks || []) state.networks.set(network.id, network);
    deps.renderPromptNick();
    for (const buffer of s.buffers || []) {
      state.buffers.set(buffer.id, {
        unread: 0,
        mentions: 0,
        show_embeds: true,
        show_presence_events: true,
        collapse_presence_events: false,
        pinned: false,
        ...buffer,
      });
    }
    for (const [id, msgs] of Object.entries(s.initial_messages || {})) state.messages.set(+id, msgs as Message[]);
    for (const [id, members] of Object.entries(s.members || {})) state.members.set(+id, members as Member[]);
    if (updateRes?.ok) {
      state.updateStatus = (await updateRes.json()) as UpdateStatus;
    } else {
      state.updateStatus = null;
    }
    deps.inferUnreadCounts();
    deps.renderSidebar();
    if (!state.activeId && state.buffers.size) {
      const fromUrl = deps.bufferFromHash(location.hash);
      const firstChannel = [...state.buffers.values()].find(
        (buffer) => buffer.kind === "channel" && buffer.joined === true,
      );
      const initial = fromUrl || firstChannel || state.buffers.values().next().value;
      deps.setActive(initial.id, { replaceHash: true });
    }
    state.reconnectAttempts = 0;
    deps.scheduleReconnect(0);
  } catch (err) {
    state.backendStatus = "offline";
    deps.renderSidebarStatus();
    console.error("hydrate failed", err);
    deps.scheduleReconnect(deps.nextReconnectDelay());
  }
}

export function connectWS(deps: ConnectionDeps) {
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) return;
  state.backendStatus = "connecting";
  deps.renderSidebarStatus();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/api/stream`);
  state.ws = ws;
  ws.addEventListener("open", () => {
    state.wsReady = true;
    state.backendStatus = "connected";
    state.reconnectAttempts = 0;
    deps.renderSidebarStatus();
    deps.updateInputEnabled();
    deps.maybeMarkActiveRead();
    deps.renderSidebar();
  });
  ws.addEventListener("message", (ev) => {
    try {
      deps.handleWSMessage(JSON.parse(ev.data));
    } catch {
      console.warn("non-json ws frame", ev.data);
    }
  });
  ws.addEventListener("close", () => {
    if (state.ws === ws) state.ws = null;
    state.wsReady = false;
    state.backendStatus = "offline";
    if (!deps.domReady()) return;
    deps.renderSidebarStatus();
    deps.updateInputEnabled();
    deps.renderSidebar();
    deps.scheduleReconnect(deps.nextReconnectDelay());
  });
  ws.addEventListener("error", () => ws.close());
}

export function clearReconnectTimer() {
  if (state.reconnectTimer != null) {
    window.clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  if (state.reconnectTicker != null) {
    window.clearInterval(state.reconnectTicker);
    state.reconnectTicker = null;
  }
  state.reconnectAt = null;
}

export function nextReconnectDelay(): number {
  const delay = Math.min(RECONNECT_BASE_MS * 2 ** state.reconnectAttempts, RECONNECT_MAX_MS);
  state.reconnectAttempts += 1;
  return delay;
}

export function scheduleReconnect(
  deps: Pick<ConnectionDeps, "domReady" | "renderSidebarStatus"> & { connectWS: () => void },
  delayMs: number,
) {
  clearReconnectTimer();
  state.backendStatus = "reconnecting";
  state.reconnectAt = Date.now() + delayMs;
  deps.renderSidebarStatus();
  if (delayMs > 0) {
    state.reconnectTicker = window.setInterval(() => {
      if (!deps.domReady() || state.backendStatus !== "reconnecting") return;
      deps.renderSidebarStatus();
    }, 250);
  }
  state.reconnectTimer = window.setTimeout(() => {
    if (state.reconnectTicker != null) {
      window.clearInterval(state.reconnectTicker);
      state.reconnectTicker = null;
    }
    state.reconnectTimer = null;
    state.reconnectAt = null;
    if (!deps.domReady()) return;
    deps.connectWS();
  }, delayMs);
}
