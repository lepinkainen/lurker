import type { Preview } from "./preview";

export type LayoutSettings = {
  collapsed: Record<string, boolean>;
  pinned: string[];
  archivesOpen: Record<string, boolean>;
  sidebarHidden: boolean;
};

export type Network = {
  id: string;
  name: string;
  // "irc" or a datasource kind such as "bluesky". Frontend should gate
  // IRC-only actions (send, join, mode, …) on kind === "irc".
  kind?: string;
  host?: string;
  port?: number;
  tls?: boolean;
  nick?: string;
  realname?: string;
  status?: string;
  sort_order?: number;
  disabled?: boolean;
};

export type Buffer = {
  id: string;
  network_id: string;
  name: string;
  kind: string;
  joined?: boolean;
  topic?: string;
  topic_set_by?: string;
  last_seen_id?: string;
  unread: number;
  mentions: number;
  show_embeds: boolean;
  show_presence_events: boolean;
  collapse_presence_events: boolean;
  pinned: boolean;
};

export type Message = {
  id: string;
  buffer_id: string;
  sender?: string;
  target?: string;
  content?: string;
  kind?: string;
  ts?: string;
  previews?: Preview[];
  display_kind?: string;
  is_self?: boolean;
  mentions_me?: boolean;
  counts_as_unread?: boolean;
};

export type Member = {
  nick: string;
  prefix: string;
  away: boolean;
  self: boolean;
};

export type StateResponse = {
  current_nick?: string;
  nick?: string;
  user?: { nick: string };
  networks?: (Network & { nick?: string })[];
  buffers?: Buffer[];
  initial_messages?: Record<string, Message[]>;
  members?: Record<string, Member[]>;
};

export type ReorderResponse = {
  networks?: Network[];
};

export type UpdateStatus = {
  enabled: boolean;
  update_available: boolean;
  remote_version?: string;
};

export type TailscaleStatus = {
  status: "tailscale" | "local" | "external" | "unknown";
  remote_ip: string;
};

export type ChannelListEntry = { name: string; count: number; topic?: string };

export type BufferInputState = {
  entries: string[];
  draft: string;
  index: number | null;
};

export type AppState = {
  networks: Map<string, Network>;
  buffers: Map<string, Buffer>;
  messages: Map<string, Message[]>;
  members: Map<string, Member[]>;
  inputHistory: Map<string, BufferInputState>;
  activeId: string | null;
  ws: WebSocket | null;
  wsReady: boolean;
  backendStatus: "connecting" | "connected" | "offline" | "reconnecting";
  reconnectAttempts: number;
  reconnectTimer: number | null;
  reconnectAt: number | null;
  reconnectTicker: number | null;
  lastWSActivityAt: number;
  wsHealthTimer: number | null;
  needsStateSyncOnConnect: boolean;
  loadingHistory: Set<string>;
  historyExhausted: Set<string>;
  lastMarkedReadId: Map<string, string>;
  me: { nick: string };
  showMemberList: boolean;
  layout: LayoutSettings;
  drag: { id: string | null; over: string | null };
  updateStatus: UpdateStatus | null;
  tailscaleStatus: TailscaleStatus | null;
  channelList: { network_id: string; entries: ChannelListEntry[]; done: boolean } | null;
};

const LAYOUT_KEY = "lurker.layout";
function defaultLayout(): LayoutSettings {
  return { collapsed: {}, pinned: [], archivesOpen: {}, sidebarHidden: false };
}

export function loadLayout(): LayoutSettings {
  try {
    const saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || "{}");
    return { ...defaultLayout(), ...saved };
  } catch {
    return defaultLayout();
  }
}

export function saveLayout(layout: LayoutSettings) {
  try {
    localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
  } catch {
    // Persisting layout is best-effort; ignore unavailable storage/quota failures.
  }
}

export const state: AppState = {
  networks: new Map(),
  buffers: new Map(),
  messages: new Map(),
  members: new Map(),
  inputHistory: new Map(),
  activeId: null,
  ws: null,
  wsReady: false,
  backendStatus: "connecting",
  reconnectAttempts: 0,
  reconnectTimer: null,
  reconnectAt: null,
  reconnectTicker: null,
  lastWSActivityAt: 0,
  wsHealthTimer: null,
  needsStateSyncOnConnect: false,
  loadingHistory: new Set(),
  historyExhausted: new Set(),
  lastMarkedReadId: new Map(),
  me: { nick: "you" },
  showMemberList: true,
  layout: loadLayout(),
  drag: { id: null, over: null },
  updateStatus: null,
  tailscaleStatus: null,
  channelList: null,
};

export function activeBuffer(): Buffer | undefined {
  return state.activeId !== null ? state.buffers.get(state.activeId) : undefined;
}
