import type { Member, Message, Network } from "./app-state";

// Server-computed nick color indexes (Go nickcolor package). The client
// never hashes nicks itself: it remembers the palette index shipped on every
// nick-bearing payload (messages, member lists, networks) and looks it up at
// render time. Keyed by lowercased nick to match the server's
// case-insensitive hash.
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

export function registerMemberNickColors(members: Member[]) {
  for (const m of members) registerNickColor(m.nick, m.color);
}

export function registerNetworkNickColor(n: Network) {
  registerNickColor(n.nick, n.nick_color);
}

export function resetNickColors() {
  indexes.clear();
}
