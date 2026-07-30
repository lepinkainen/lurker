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
  nick_color?: number;
  realname?: string;
  status?: string;
  sort_order?: number;
  disabled?: boolean;
  // Whether the network is defined in config.yaml. Config is the source of
  // truth on backend restart: networks not in it get disabled, and runtime
  // toggles (like disabled) on config networks are reset from YAML.
  in_config?: boolean;
};

export type Buffer = {
  id: string;
  network_id: string;
  name: string;
  kind: string;
  joined?: boolean;
  topic?: string;
  topic_set_by?: string;
  topic_set_at?: string;
  last_seen_id?: string;
  // Server-derived "New messages" marker: id/ts of the first unread message
  // that counts (skipping non-unread kinds and self-authored). Absent when the
  // buffer is caught up. Clients hold no marker state of their own — the
  // divider and unread bar are pure functions of these fields.
  marker_id?: string | undefined;
  marker_ts?: string | undefined;
  unread: number;
  mentions: number;
  show_embeds: boolean;
  show_presence_events: boolean;
  collapse_presence_events: boolean;
  pinned: boolean;
  // Manual ordering within the network's active-channel group. Channels sort
  // by (sort_order, name); queries and archived buffers remain alphabetical.
  sort_order?: number;
  // Manual pinned-section ordering; pinned channels sort (pin_order, name).
  // Absent on pre-pin_order backends, treated as 0.
  pin_order?: number;
  // Persisted server-side flag driving the Archive section. Channels are
  // archived automatically on part/kick and unarchived on join; queries are
  // archived manually and unarchived by new activity.
  archived?: boolean;
};

// One run of uniformly-formatted text, parsed server-side from mIRC control
// codes (Go mirc package). fg/bg are mIRC palette indexes 0-15.
export type MircSegment = {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  mono?: boolean;
  fg?: number;
  bg?: number;
};

// Server-computed netsplit annotation: quit/join messages in the same
// collapsed netsplit share an id (see irc/netsplit_tracker.go).
export type NetsplitInfo = {
  id: string;
  server_a: string;
  server_b: string;
};

export type Message = {
  id: string;
  buffer_id: string;
  sender?: string;
  userhost?: string;
  target?: string;
  content?: string;
  kind?: string;
  ts?: string;
  previews?: Preview[];
  display_kind?: string;
  is_self?: boolean;
  mentions_me?: boolean;
  counts_as_unread?: boolean;
  sender_color?: number;
  target_color?: number;
  highlight?: boolean;
  highlight_pattern?: string;
  netsplit?: NetsplitInfo;
  segments?: MircSegment[];
};

export type Member = {
  nick: string;
  prefix: string;
  realname?: string;
  away: boolean;
  self: boolean;
  color?: number;
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

// Response of POST /api/buffers/pinned/reorder, identical in shape to the
// pinned_reorder broadcast the same call publishes.
export type PinnedReorderResponse = {
  buffers?: { id: string; pin_order: number }[];
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
  me: { nick: string };
  showMemberList: boolean;
  layout: LayoutSettings;
  drag: { id: string | null; over: string | null };
  // Pinned-section row drag, kept separate from the network drag so the two
  // never highlight each other's drop targets. `over` is the buffer id whose
  // row shows the insertion bar, or PINNED_DROP_END for the end-of-list zone.
  pinDrag: { id: string | null; over: string | null };
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

const LAST_ACTIVE_KEY = "lurker.lastActive";

export function loadLastActive(): string | null {
  try {
    return localStorage.getItem(LAST_ACTIVE_KEY);
  } catch {
    return null;
  }
}

export function saveLastActive(id: string) {
  try {
    localStorage.setItem(LAST_ACTIVE_KEY, id);
  } catch {
    // Best-effort; ignore unavailable storage/quota failures.
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
  me: { nick: "you" },
  showMemberList: true,
  layout: loadLayout(),
  drag: { id: null, over: null },
  pinDrag: { id: null, over: null },
  updateStatus: null,
  tailscaleStatus: null,
  channelList: null,
};

export function activeBuffer(): Buffer | undefined {
  return state.activeId !== null ? state.buffers.get(state.activeId) : undefined;
}
