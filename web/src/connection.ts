import {
  loadLastActive,
  type Member,
  type Message,
  reconcileAnchor,
  type StateResponse,
  state,
  type TailscaleStatus,
  type UpdateStatus,
} from "./app-state";
import { getStartupFallbackBufferIds } from "./buffers";

export const RECONNECT_BASE_MS = 1000;
export const RECONNECT_MAX_MS = 30_000;
export const WS_STALE_MS = 60_000;
export const WS_HEALTHCHECK_MS = 10_000;

// Grouped collaborator interfaces. Rendering, navigation, and transport are
// the three cohesive seams used across connection.ts.

export type Renderer = {
  renderStatus: () => void;
  renderPromptNick: () => void;
  renderSidebar: () => void;
  renderHeader: () => void;
  renderActiveView: () => void;
  renderMembers: () => void;
  updateInputEnabled: () => void;
};

export type Navigation = {
  setActive: (id: string, opts?: { skipHash?: boolean; replaceHash?: boolean }) => void;
  bufferFromHash: (hash: string) => { id: string } | null;
  maybeMarkActiveRead: () => void;
};

export type Transport = {
  syncState: () => Promise<void>;
  handleMessage: (msg: unknown) => void;
};

export type StateSyncDeps = {
  renderer: Pick<
    Renderer,
    "renderPromptNick" | "renderSidebar" | "renderHeader" | "renderActiveView" | "renderMembers"
  >;
  navigation: Pick<Navigation, "setActive" | "bufferFromHash">;
};

export type HydrateDeps = {
  renderer: Pick<Renderer, "renderStatus">;
  transport: Pick<Transport, "syncState">;
  scheduleReconnect: (delayMs: number) => void;
};

export type ConnectionDeps = {
  domReady: () => boolean;
  renderer: Pick<Renderer, "renderStatus" | "renderSidebar" | "updateInputEnabled">;
  navigation: Pick<Navigation, "maybeMarkActiveRead">;
  transport: Transport;
};

export type Connection = {
  hydrate: () => Promise<void>;
  connect: () => void;
  scheduleReconnect: (delayMs: number) => void;
};

type WebSocketRuntimeDeps = ConnectionDeps & {
  scheduleReconnect: (delayMs: number) => void;
};

type HealthCheckDeps = {
  domReady: () => boolean;
  renderer: Pick<Renderer, "renderStatus" | "renderSidebar" | "updateInputEnabled">;
  scheduleReconnect: (delayMs: number) => void;
};

type ReconnectDeps = {
  domReady: () => boolean;
  renderer: Pick<Renderer, "renderStatus">;
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
  deps.renderer.renderPromptNick();
  for (const buffer of s.buffers || []) {
    state.buffers.set(buffer.id, {
      ...buffer,
      unread: buffer.unread ?? 0,
      mentions: buffer.mentions ?? 0,
    });
    reconcileAnchor(buffer.id);
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
  deps.renderer.renderSidebar();
  deps.renderer.renderHeader();
  deps.renderer.renderActiveView();
  deps.renderer.renderMembers();
  if (!state.activeId && state.buffers.size > 0) {
    const fromUrl = deps.navigation.bufferFromHash(location.hash);
    // iOS standalone PWAs cold-launch at the manifest start_url ("/") with no
    // hash, so fall back to the last active buffer persisted in localStorage.
    const lastId = loadLastActive();
    const lastActive = lastId ? state.buffers.get(lastId) : null;
    const fallbackId = getStartupFallbackBufferIds()[0];
    const fallback = fallbackId ? state.buffers.get(fallbackId) : undefined;
    const initial = fromUrl ?? lastActive ?? fallback;
    if (initial) deps.navigation.setActive(initial.id, { replaceHash: true });
  }
}

export async function hydrate(deps: HydrateDeps) {
  try {
    state.backendStatus = "connecting";
    deps.renderer.renderStatus();
    await deps.transport.syncState();
    state.reconnectAttempts = 0;
    deps.scheduleReconnect(0);
  } catch (err) {
    state.backendStatus = "offline";
    deps.renderer.renderStatus();
    console.error("hydrate failed", err);
    deps.scheduleReconnect(nextReconnectDelay());
  }
}

export function createConnection(deps: ConnectionDeps): Connection {
  const runtimeDeps: WebSocketRuntimeDeps = { ...deps, scheduleReconnect };

  function connect() {
    connectWS(runtimeDeps);
  }

  function scheduleReconnect(delayMs: number) {
    scheduleReconnectTimer(
      {
        domReady: deps.domReady,
        renderer: { renderStatus: deps.renderer.renderStatus },
        connectWS: connect,
      },
      delayMs,
    );
  }

  return {
    hydrate: () =>
      hydrate({
        renderer: { renderStatus: deps.renderer.renderStatus },
        transport: { syncState: deps.transport.syncState },
        scheduleReconnect,
      }),
    connect,
    scheduleReconnect,
  };
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
  deps.renderer.renderStatus();
  deps.renderer.updateInputEnabled();
  deps.renderer.renderSidebar();
  deps.scheduleReconnect(0);
}

function connectWS(deps: WebSocketRuntimeDeps) {
  if (state.ws && (state.ws.readyState === WebSocket.OPEN || state.ws.readyState === WebSocket.CONNECTING)) return;
  state.backendStatus = "connecting";
  deps.renderer.renderStatus();
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/api/stream`);
  state.ws = ws;
  startWSHealthCheck(deps);
  ws.addEventListener("open", () => {
    state.wsReady = true;
    state.lastWSActivityAt = Date.now();
    state.backendStatus = "connected";
    state.reconnectAttempts = 0;
    deps.renderer.renderStatus();
    deps.renderer.updateInputEnabled();
    deps.navigation.maybeMarkActiveRead();
    deps.renderer.renderSidebar();
    if (state.needsStateSyncOnConnect) {
      deps.transport
        .syncState()
        .then(() => {
          state.needsStateSyncOnConnect = false;
        })
        .catch((err) => console.error("reconnect state sync failed", err));
    }
  });
  ws.addEventListener("message", (ev) => {
    state.lastWSActivityAt = Date.now();
    try {
      deps.transport.handleMessage(JSON.parse(ev.data));
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
    deps.renderer.renderStatus();
    deps.renderer.updateInputEnabled();
    deps.renderer.renderSidebar();
    deps.scheduleReconnect(nextReconnectDelay());
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

function scheduleReconnectTimer(deps: ReconnectDeps, delayMs: number) {
  clearReconnectTimer();
  state.backendStatus = "reconnecting";
  state.reconnectAt = Date.now() + delayMs;
  deps.renderer.renderStatus();
  if (delayMs > 0) {
    state.reconnectTicker = window.setInterval(() => {
      if (!deps.domReady() || state.backendStatus !== "reconnecting") return;
      deps.renderer.renderStatus();
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
