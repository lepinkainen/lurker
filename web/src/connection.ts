import {
  loadLastActive,
  type Member,
  type Message,
  type StateResponse,
  state,
  type TailscaleStatus,
  type UpdateStatus,
} from "./app-state";
import { getStartupFallbackBufferIds } from "./buffers";
import { registerMemberNickColors, registerMessageNickColors, registerNetworkNickColor } from "./nick-colors";

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
};

export type Transport = {
  applySnapshot: (snapshot: StateResponse) => void;
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
  scheduleReconnect: (delayMs: number) => void;
};

export type ConnectionDeps = {
  domReady: () => boolean;
  renderer: Pick<Renderer, "renderStatus" | "renderSidebar" | "updateInputEnabled">;
  transport: Transport;
};

export type Connection = {
  hydrate: () => void;
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

// applyStateSnapshot installs the `state` frame the server sends in answer
// to hello: the full picture as of its seq. Everything is replaced; the
// stream then carries only events newer than that seq.
export function applyStateSnapshot(s: StateResponse, deps: StateSyncDeps) {
  state.me.nick = s.current_nick || s.nick || s.user?.nick || s.networks?.[0]?.nick || "you";
  // The snapshot is authoritative: networks/buffers deleted while this tab
  // was offline (their buffer_deleted / network events never reached us)
  // must go, along with everything hanging off them.
  const liveNetworks = new Set((s.networks || []).map((n) => n.id));
  for (const id of [...state.networks.keys()]) {
    if (!liveNetworks.has(id)) state.networks.delete(id);
  }
  const liveBuffers = new Set((s.buffers || []).map((b) => b.id));
  for (const id of [...state.buffers.keys()]) {
    if (liveBuffers.has(id)) continue;
    state.buffers.delete(id);
    state.messages.delete(id);
    state.members.delete(id);
    state.inputHistory.delete(id);
    state.loadingHistory.delete(id);
    state.historyExhausted.delete(id);
    // Fallback selection below picks a replacement when the active one went.
    if (state.activeId === id) state.activeId = null;
  }
  for (const network of s.networks || []) {
    state.networks.set(network.id, network);
    registerNetworkNickColor(network);
  }
  deps.renderer.renderPromptNick();
  // Full replace per buffer: marker_id/marker_ts are omitted server-side when
  // the buffer is caught up, so absent fields land as undefined (no marker).
  for (const buffer of s.buffers || []) {
    state.buffers.set(buffer.id, {
      ...buffer,
      unread: buffer.unread ?? 0,
      mentions: buffer.mentions ?? 0,
    });
  }
  for (const [id, msgs] of Object.entries(s.initial_messages || {})) {
    const existing = state.messages.get(id) || [];
    const byID = new Map(existing.map((msg) => [msg.id, msg]));
    for (const msg of msgs as Message[]) {
      byID.set(msg.id, msg);
      registerMessageNickColors(msg);
    }
    state.messages.set(
      id,
      [...byID.values()].sort((a, b) => a.id.localeCompare(b.id)),
    );
  }
  for (const [id, members] of Object.entries(s.members || {})) {
    state.members.set(id, members as Member[]);
    registerMemberNickColors(members as Member[], state.buffers.get(id)?.network_id);
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

// fetchAuxStatus refreshes the side-channel status endpoints shown in the
// settings dialog. Independent of the stream; failures leave them null.
async function fetchAuxStatus() {
  const [updateRes, tailscaleRes] = await Promise.all([
    fetch("/api/update-status", { headers: { Accept: "application/json" } }).catch(() => null),
    fetch("/api/tailscale-status", { headers: { Accept: "application/json" } }).catch(() => null),
  ]);
  state.updateStatus = updateRes?.ok ? ((await updateRes.json()) as UpdateStatus) : null;
  state.tailscaleStatus = tailscaleRes?.ok ? ((await tailscaleRes.json()) as TailscaleStatus) : null;
}

export function hydrate(deps: HydrateDeps) {
  // Everything comes over the socket: the open handler sends hello and the
  // server answers with the state snapshot (or a replay from our cursor).
  state.backendStatus = "connecting";
  deps.renderer.renderStatus();
  state.reconnectAttempts = 0;
  deps.scheduleReconnect(0);
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
    deps.renderer.renderSidebar();
    // Resume from our cursor: the server replays what we missed when it
    // still has it, otherwise answers with a full `state` snapshot.
    ws.send(JSON.stringify({ type: "hello", since: state.lastSeq }));
    fetchAuxStatus().then(
      () => deps.renderer.renderStatus(),
      () => undefined,
    );
  });
  ws.addEventListener("message", (ev) => {
    state.lastWSActivityAt = Date.now();
    let parsed: { type?: string; seq?: number };
    try {
      parsed = JSON.parse(ev.data);
    } catch {
      console.warn("non-json ws frame", ev.data);
      return;
    }
    // One rule for every sequenced frame (snapshot included): at or below
    // the cursor it is already applied, otherwise apply and advance.
    if (typeof parsed.seq === "number") {
      if (parsed.seq <= state.lastSeq) return;
      state.lastSeq = parsed.seq;
    }
    if (parsed.type === "state") {
      deps.transport.applySnapshot(parsed as StateResponse);
      return;
    }
    deps.transport.handleMessage(parsed);
  });
  ws.addEventListener("close", () => {
    if (state.ws === ws) state.ws = null;
    state.wsReady = false;
    state.backendStatus = "offline";
    // history_result frames are socket-local (no seq, never replayed): a
    // request lost with this socket must not block paging after resume.
    state.loadingHistory.clear();
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
