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
  port?: number;
  tls?: boolean;
  nick?: string;
  realname?: string;
  status?: string;
  sort_order?: number;
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

export type UpdateStatus = {
  enabled: boolean;
  update_available: boolean;
  remote_version?: string;
};

export type ChannelListEntry = { name: string; count: number; topic?: string };

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
  updateStatus: UpdateStatus | null;
  channelList: { network_id: number; entries: ChannelListEntry[]; done: boolean } | null;
};

const LAYOUT_KEY = "lurker.layout";
function defaultLayout(): LayoutSettings {
  return { collapsed: {}, pinned: [], archivesOpen: {} };
}

export const SLASH_COMMANDS: SlashCommand[] = [
  { cmd: "/join", args: "<channel>", desc: "Join a channel" },
  { cmd: "/part", args: "[reason]", desc: "Leave current channel" },
  { cmd: "/msg", args: "<nick> <text>", desc: "Send private message" },
  { cmd: "/me", args: "<action>", desc: "Send /me action" },
  { cmd: "/nick", args: "<newnick>", desc: "Change your nick" },
  { cmd: "/topic", args: "[new topic]", desc: "View or set channel topic" },
  { cmd: "/whois", args: "<nick>", desc: "Query user info" },
  { cmd: "/invite", args: "<nick> [channel]", desc: "Invite nick to channel" },
  { cmd: "/kick", args: "<nick> [reason]", desc: "Kick nick from channel" },
  { cmd: "/mode", args: "<modes> [params]", desc: "Set channel modes" },
  { cmd: "/raw", args: "<line>", desc: "Send raw IRC line" },
  { cmd: "/away", args: "[message]", desc: "Set away status" },
  { cmd: "/back", args: "", desc: "Clear away status" },
  { cmd: "/quit", args: "[message]", desc: "Disconnect from network" },
  { cmd: "/rejoin", args: "", desc: "Rejoin current channel" },
  { cmd: "/cycle", args: "", desc: "Rejoin current channel (alias)" },
  { cmd: "/notice", args: "<target> <text>", desc: "Send a NOTICE" },
  { cmd: "/ctcp", args: "<nick> <cmd> [args]", desc: "Send CTCP request" },
  { cmd: "/query", args: "<nick>", desc: "Open PM buffer without messaging" },
  { cmd: "/list", args: "[filter]", desc: "List channels on server" },
  { cmd: "/op", args: "<nick>", desc: "Give channel op (+o)" },
  { cmd: "/deop", args: "<nick>", desc: "Remove channel op (-o)" },
  { cmd: "/voice", args: "<nick>", desc: "Give voice (+v)" },
  { cmd: "/devoice", args: "<nick>", desc: "Remove voice (-v)" },
  { cmd: "/ban", args: "<mask>", desc: "Ban mask (+b)" },
  { cmd: "/unban", args: "<mask>", desc: "Unban mask (-b)" },
  { cmd: "/banlist", args: "", desc: "Show channel ban list" },
  { cmd: "/kickban", args: "<nick> [reason]", desc: "Kick and ban nick" },
  { cmd: "/ignore", args: "<nick>", desc: "Ignore nick or mask" },
  { cmd: "/unignore", args: "<nick>", desc: "Remove ignore" },
  { cmd: "/ignorelist", args: "", desc: "List ignored masks" },
];

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
