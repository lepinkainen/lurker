import type { Preview } from "./preview";

export type LayoutSettings = {
  collapsed: Record<number, boolean>;
  pinned: number[];
  archivesOpen: Record<number, boolean>;
};

export type Network = {
  id: number;
  name: string;
  host?: string;
  status?: string;
  sort_order?: number;
  tls?: boolean;
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

export type SlashCommand = {
  cmd: string;
  args: string;
  desc: string;
};

export type AppState = {
  networks: Map<number, Network>;
  buffers: Map<number, Buffer>;
  messages: Map<number, Message[]>;
  members: Map<number, Member[]>;
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
};

const LAYOUT_KEY = "lurker.layout";
const DEFAULT_LAYOUT: LayoutSettings = { collapsed: {}, pinned: [], archivesOpen: {} };

export const SLASH_COMMANDS: SlashCommand[] = [
  { cmd: "/join", args: "<channel>", desc: "Join a channel" },
  { cmd: "/part", args: "[reason]", desc: "Leave the current channel" },
  { cmd: "/msg", args: "<nick> <text>", desc: "Send a private message" },
  { cmd: "/me", args: "<action>", desc: "Send a /me action" },
  { cmd: "/nick", args: "<newnick>", desc: "Change your nick" },
  { cmd: "/topic", args: "[new topic]", desc: "View or set channel topic" },
  { cmd: "/whois", args: "<nick>", desc: "Query user info" },
  { cmd: "/invite", args: "<nick> [channel]", desc: "Invite nick to channel" },
  { cmd: "/kick", args: "<nick> [reason]", desc: "Kick nick from channel" },
  { cmd: "/mode", args: "<modes> [params]", desc: "Set channel modes" },
  { cmd: "/raw", args: "<line>", desc: "Send raw IRC line" },
];

export function loadLayout(): LayoutSettings {
  try {
    const saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || "{}");
    return { ...DEFAULT_LAYOUT, ...saved };
  } catch {
    return { ...DEFAULT_LAYOUT };
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
};
