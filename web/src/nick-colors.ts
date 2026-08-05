import { activeBuffer, type Member, type Message, type Network } from "./app-state";

// Server-computed nick color indexes (Go nickcolor package). The client
// never hashes nicks itself: it remembers the palette index shipped on every
// nick-bearing payload (messages, member lists, networks) and looks it up at
// render time. Keyed by lowercased nick to match the server's
// case-insensitive hash (the hash is nick-only, so one entry per nick is
// correct across networks).
const indexes = new Map<string, number>();

export function registerNickColor(nick: string | undefined, idx: number | null | undefined) {
  if (!nick || idx === null || idx === undefined) return;
  indexes.set(nick.toLowerCase(), idx);
}

export function nickColorIndex(nick: string | undefined | null): number | undefined {
  if (!nick) return undefined;
  return indexes.get(nick.toLowerCase());
}

export function registerMessageNickColors(m: Message) {
  registerNickColor(m.sender, m.sender_color);
  registerNickColor(m.target, m.target_color);
}

export function registerMemberNickColors(members: Member[], networkId?: string) {
  for (const m of members) {
    registerNickColor(m.nick, m.color);
    registerBotNick(networkId, m.nick, m.bot);
  }
}

// IRCv3 bot-mode nicks (https://ircv3.net/specs/extensions/bot-mode). Only
// member lists carry the flag, but we remember it here so message rows and
// system lines render the robot glyph too. Keyed per network — bot mode is
// per-network state, and the same nick can be a bot on one network and a
// human on another. Member lists are authoritative snapshots of the
// server-side tracker, so an explicit bot=false clears the entry (a human
// taking over a bot's nick stops rendering as a bot).
const bots = new Set<string>();

function botKey(networkId: string, nick: string): string {
  return `${networkId}:${nick.toLowerCase()}`;
}

export function registerBotNick(networkId: string | undefined, nick: string | undefined, bot: boolean | undefined) {
  if (!(networkId && nick)) return;
  if (bot) bots.add(botKey(networkId, nick));
  else bots.delete(botKey(networkId, nick));
}

// isBotNick resolves against the active buffer's network: every call site
// renders content of the active buffer (message rows, member list, input
// nick), so the active network is the right scope.
export function isBotNick(nick: string | undefined | null): boolean {
  if (!nick) return false;
  const networkId = activeBuffer()?.network_id;
  if (!networkId) return false;
  return bots.has(botKey(networkId, nick));
}

export function registerNetworkNickColor(n: Network) {
  registerNickColor(n.nick, n.nick_color);
}

export function resetNickColors() {
  indexes.clear();
  bots.clear();
}
