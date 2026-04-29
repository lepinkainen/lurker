import type { Preview } from "./preview";

export type LayoutSettings = {
  collapsed: Record<number, boolean>;
  pinned: number[];
  archivesOpen: Record<number, boolean>;
  sidebarHidden: boolean;
};

export type Network = {
  id: number;
  name: string;
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

export type Message = {
  id: number;
  buffer_id: number;
  sender?: string;
  target?: string;
  content?: string;
  kind?: string;
  ts?: string;
  previews?: Preview[];
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
  buffers?: Omit<Buffer, "unread" | "mentions">[];
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

export type ChannelListEntry = { name: string; count: number; topic?: string };

export type BufferInputState = {
  entries: string[];
  draft: string;
  index: number | null;
};

export type AppState = {
  networks: Map<number, Network>;
  buffers: Map<number, Buffer>;
  messages: Map<number, Message[]>;
  members: Map<number, Member[]>;
  inputHistory: Map<number, BufferInputState>;
  activeId: number | null;
  ws: WebSocket | null;
  wsReady: boolean;
  backendStatus: "connecting" | "connected" | "offline" | "reconnecting";
  reconnectAttempts: number;
  reconnectTimer: number | null;
  reconnectAt: number | null;
  reconnectTicker: number | null;
  loadingHistory: Set<number>;
  historyExhausted: Set<number>;
  lastMarkedReadId: Map<number, number>;
  me: { nick: string };
  showMemberList: boolean;
  layout: LayoutSettings;
  drag: { id: number | null; over: number | null };
  updateStatus: UpdateStatus | null;
  channelList: { network_id: number; entries: ChannelListEntry[]; done: boolean } | null;
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
  } catch {}
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
  loadingHistory: new Set(),
  historyExhausted: new Set(),
  lastMarkedReadId: new Map(),
  me: { nick: "you" },
  showMemberList: true,
  layout: loadLayout(),
  drag: { id: null, over: null },
  updateStatus: null,
  channelList: null,
};
