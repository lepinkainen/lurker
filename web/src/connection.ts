import {
  type Member,
  type Message,
  type StateResponse,
  state,
  type TailscaleStatus,
  type UpdateStatus,
} from "./app-state";

export const RECONNECT_BASE_MS = 1000;
export const RECONNECT_MAX_MS = 30_000;
export const WS_STALE_MS = 60_000;
export const WS_HEALTHCHECK_MS = 10_000;

export type StateSyncDeps = {
  renderPromptNick: () => void;
  inferUnreadCounts: () => void;
  renderSidebar: () => void;
  renderHeader: () => void;
  renderActiveView: () => void;
  renderMembers: () => void;
  setActive: (id: string, opts?: { skipHash?: boolean; replaceHash?: boolean }) => void;
  bufferFromHash: (hash: string) => { id: string } | null;
};

export type HydrateDeps = {
  renderSidebarStatus: () => void;
  syncStateFromServer: () => Promise<void>;
  scheduleReconnect: (delayMs: number) => void;
  nextReconnectDelay: () => number;
};

export type WebSocketDeps = {
  domReady: () => boolean;
  renderSidebarStatus: () => void;
  renderSidebar: () => void;
  updateInputEnabled: () => void;
  maybeMarkActiveRead: () => void;
  syncStateFromServer: () => Promise<void>;
  scheduleReconnect: (delayMs: number) => void;
  nextReconnectDelay: () => number;
  handleWSMessage: (msg: unknown) => void;
};

type HealthCheckDeps = Pick<
  WebSocketDeps,
  "domReady" | "renderSidebarStatus" | "updateInputEnabled" | "renderSidebar" | "scheduleReconnect"
>;

type ReconnectDeps = {
  domReady: () => boolean;
  renderSidebarStatus: () => void;
  connectWS: () => void;
};

export async function syncStateFromServer(deps: StateSyncDeps) {
  const [stateRes, updateRes, tailscaleRes] = await Promise.all([
    fetch("/api/state"),
    fetch("/api/update-status", { headers: { Accept: "application/json" } }).catch(() => null),
    fetch("/api/tailscale-status", { headers: { Accept: "application/json" } }).catch(() => null),
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
      ...buffer,
    });
  }
  for (const [id, msgs] of Object.entries(s.initial_messages || {})) {
    const existing = state.messages.get(id) || [];
    const byID = new Map(existing.map((msg) => [msg.id, msg]));
    for (const msg of msgs as Message[]) byID.set(msg.id, msg);
    state.messages.set(
      id,
      [...byID.values()].sort((a, b) => a.id.localeCompare(b.id)),
    );
  }
  for (const [id, members] of Object.entries(s.members || {})) state.members.set(id, members as Member[]);
  if (updateRes?.ok) {
    state.updateStatus = (await updateRes.json()) as UpdateStatus;
  } else {
    state.updateStatus = null;
  }
  if (tailscaleRes?.ok) {
    state.tailscaleStatus = (await tailscaleRes.json()) as TailscaleStatus;
  } else {
    state.tailscaleStatus = null;
  }
  deps.inferUnreadCounts();
  deps.renderSidebar();
  deps.renderHeader();
  deps.renderActiveView();
  deps.renderMembers();
  if (!state.activeId && state.buffers.size > 0) {
    const fromUrl = deps.bufferFromHash(location.hash);
    const firstChannel = [...state.buffers.values()].find(
      (buffer) => buffer.kind === "channel" && buffer.joined === true,
    );
    const initial = fromUrl ?? firstChannel ?? state.buffers.values().next().value;
    if (initial) deps.setActive(initial.id, { replaceHash: true });
  }
}

export async function hydrate(deps: HydrateDeps) {
  try {
    state.backendStatus = "connecting";
    deps.renderSidebarStatus();
    await deps.syncStateFromServer();
    state.reconnectAttempts = 0;
    deps.scheduleReconnect(0);
  } catch (err) {
    state.backendStatus = "offline";
    deps.renderSidebarStatus();
    console.error("hydrate failed", err);
    deps.scheduleReconnect(deps.nextReconnectDelay());
  }
}

function startWSHealthCheck(deps: HealthCheckDeps) {
  if (state.wsHealthTimer !== null) return;
  state.wsHealthTimer = window.setInterval(() => checkWebSocketHealth(deps), WS_HEALTHCHECK_MS);
}

export function checkWebSocketHealth(deps: HealthCheckDeps) {
  const ws = state.ws;
  if (!(ws && state.wsReady) || ws.readyState !== WebSocket.OPEN) return;
  if (Date.now() - state.lastWSActivityAt <= WS_STALE_MS) return;

  try {
    ws.close();
  } catch {
    // Some browsers can throw while closing a dead socket; recovery continues below.
  }
  if (state.ws === ws) state.ws = null;
  state.wsReady = false;
  state.backendStatus = "offline";
  state.needsStateSyncOnConnect = true;
  if (!deps.domReady()) return;
  deps.renderSidebarStatus();
  deps.updateInputEnabled();
  deps.renderSidebar();
  deps.scheduleReconnect(0);
}

export function connectWS(deps: WebSocketDeps) {
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) return;
  state.backendStatus = "connecting";
  deps.renderSidebarStatus();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/api/stream`);
  state.ws = ws;
  startWSHealthCheck(deps);
  ws.addEventListener("open", () => {
    state.wsReady = true;
    state.lastWSActivityAt = Date.now();
    state.backendStatus = "connected";
    state.reconnectAttempts = 0;
    deps.renderSidebarStatus();
    deps.updateInputEnabled();
    deps.maybeMarkActiveRead();
    deps.renderSidebar();
    if (state.needsStateSyncOnConnect) {
      deps
        .syncStateFromServer()
        .then(() => {
          state.needsStateSyncOnConnect = false;
        })
        .catch((err) => console.error("reconnect state sync failed", err));
    }
  });
  ws.addEventListener("message", (ev) => {
    state.lastWSActivityAt = Date.now();
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
    state.needsStateSyncOnConnect = true;
    if (!deps.domReady()) return;
    deps.renderSidebarStatus();
    deps.updateInputEnabled();
    deps.renderSidebar();
    deps.scheduleReconnect(deps.nextReconnectDelay());
  });
  ws.addEventListener("error", () => ws.close());
}

export function clearReconnectTimer() {
  if (state.reconnectTimer !== null) {
    window.clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  if (state.reconnectTicker !== null) {
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

export function scheduleReconnect(deps: ReconnectDeps, delayMs: number) {
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
    if (state.reconnectTicker !== null) {
      window.clearInterval(state.reconnectTicker);
      state.reconnectTicker = null;
    }
    state.reconnectTimer = null;
    state.reconnectAt = null;
    if (!deps.domReady()) return;
    deps.connectWS();
  }, delayMs);
}
