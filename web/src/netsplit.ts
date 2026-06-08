import type { Message } from "./app-state";

// Mirror of irc/netsplit.go consts. Keep in sync.
export const NETSPLIT_QUIT_WINDOW_MS = 60_000;
export const NETSPLIT_REJOIN_WINDOW_MS = 30 * 60_000;
export const NETSPLIT_MIN_QUITS = 2;

// RFC1459 casemapping (mirror of irc/casemap.go caseFoldNick). IRC networks
// advertise CASEMAPPING=rfc1459 (or rfc1459-strict) which treats {}|~ as
// lowercase of []\^ — use this anywhere nicks are compared as map keys.
export function caseFoldNick(s: string): string {
  if (!s) return s;
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 65 && c <= 90) out += String.fromCharCode(c + 32);
    else if (c === 91) out += "{";
    else if (c === 93) out += "}";
    else if (c === 92) out += "|";
    else if (c === 94) out += "~";
    else out += s.charAt(i);
  }
  return out;
}

export type NetsplitGroup = {
  kind: "netsplit";
  serverA: string;
  serverB: string;
  quits: Message[];
  rejoins: Message[];
  splitTs: number;
  healTs: number; // 0 if no rejoins
};

export type PlainPresenceGroup = { kind: "plain"; items: Message[] };
export type PresenceGroup = NetsplitGroup | PlainPresenceGroup;

export function parseNetsplitReason(content: string): { a: string; b: string } | null {
  if (!content) return null;
  const parts = content.split(" ");
  if (parts.length !== 2) return null;
  const [a, b] = parts;
  if (!(a && b) || a === b) return null;
  if (!(looksLikeHost(a) && looksLikeHost(b))) return null;
  return { a, b };
}

function looksLikeHost(s: string): boolean {
  if (!s || s.length > 253) return false;
  if (!s.includes(".")) return false;
  let hasAlpha = false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if ((c >= 97 && c <= 122) || (c >= 65 && c <= 90)) {
      hasAlpha = true;
      continue;
    }
    if ((c >= 48 && c <= 57) || c === 46 || c === 45 || c === 58) continue;
    return false;
  }
  return hasAlpha;
}

function netsplitKey(a: string, b: string): string {
  return a < b ? `${a} ${b}` : `${b} ${a}`;
}

function tsMs(m: Message): number {
  return Date.parse(m.ts || "") || 0;
}

type Cluster = {
  serverA: string;
  serverB: string;
  key: string;
  quitIdx: number[];
  startTs: number;
  nicks: Set<string>;
  rejoinIdx: number[];
  healTs: number;
};

export function partitionPresence(messages: Message[]): PresenceGroup[] {
  if (messages.length === 0) return [];
  const clusters: Cluster[] = [];
  const openByKey = new Map<string, Cluster>();
  const clusterFor = new Array<number>(messages.length).fill(-1);

  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    if (!m || m.kind !== "quit") continue;
    const parsed = parseNetsplitReason(m.content || "");
    if (!parsed) continue;
    const key = netsplitKey(parsed.a, parsed.b);
    const t = tsMs(m);
    let c = openByKey.get(key);
    if (c && t - c.startTs > NETSPLIT_QUIT_WINDOW_MS) {
      openByKey.delete(key);
      c = undefined;
    }
    if (!c) {
      c = {
        serverA: parsed.a,
        serverB: parsed.b,
        key,
        quitIdx: [],
        startTs: t,
        nicks: new Set<string>(),
        rejoinIdx: [],
        healTs: 0,
      };
      clusters.push(c);
      openByKey.set(key, c);
    }
    c.quitIdx.push(i);
    c.nicks.add(caseFoldNick(m.sender || ""));
    clusterFor[i] = clusters.length - 1;
  }

  const keep = clusters.map((c) => c.quitIdx.length >= NETSPLIT_MIN_QUITS);
  for (let i = 0; i < clusterFor.length; i++) {
    const ci = clusterFor[i] ?? -1;
    if (ci >= 0 && !keep[ci]) clusterFor[i] = -1;
  }

  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    if (!m || m.kind !== "join") continue;
    const nick = caseFoldNick(m.sender || "");
    const t = tsMs(m);
    let bestCi = -1;
    let bestStart = -1;
    for (let ci = 0; ci < clusters.length; ci++) {
      if (!keep[ci]) continue;
      const c = clusters[ci];
      if (!c) continue;
      if (t < c.startTs || t - c.startTs > NETSPLIT_REJOIN_WINDOW_MS) continue;
      if (!c.nicks.has(nick)) continue;
      if (c.startTs > bestStart) {
        bestStart = c.startTs;
        bestCi = ci;
      }
    }
    if (bestCi >= 0) {
      const cb = clusters[bestCi];
      if (!cb) continue;
      cb.nicks.delete(nick);
      cb.rejoinIdx.push(i);
      if (t > cb.healTs) cb.healTs = t;
      clusterFor[i] = bestCi;
    }
  }

  const out: PresenceGroup[] = [];
  let plain: Message[] = [];
  const flushPlain = () => {
    if (plain.length > 0) {
      out.push({ kind: "plain", items: plain });
      plain = [];
    }
  };
  const emitted = new Array<boolean>(clusters.length).fill(false);
  for (let i = 0; i < messages.length; i++) {
    const ci = clusterFor[i] ?? -1;
    const m = messages[i];
    if (!m) continue;
    if (ci < 0) {
      plain.push(m);
      continue;
    }
    if (emitted[ci]) continue;
    flushPlain();
    const c = clusters[ci];
    if (!c) continue;
    const quits = c.quitIdx.map((j) => messages[j]).filter((m): m is Message => Boolean(m));
    const rejoins = c.rejoinIdx.map((j) => messages[j]).filter((m): m is Message => Boolean(m));
    out.push({
      kind: "netsplit",
      serverA: c.serverA,
      serverB: c.serverB,
      quits,
      rejoins,
      splitTs: c.startTs,
      healTs: c.healTs,
    });
    emitted[ci] = true;
  }
  flushPlain();
  return out;
}
