import { activeBuffer, type Buffer, type Network, state } from "./app-state";

function byName(a: Buffer, b: Buffer) {
  return a.name.localeCompare(b.name);
}

export function orderedNetworks(): Network[] {
  return [...state.networks.values()].sort((a, b) => {
    const ao = a.sort_order ?? Number.MAX_SAFE_INTEGER;
    const bo = b.sort_order ?? Number.MAX_SAFE_INTEGER;
    return ao - bo || a.id.localeCompare(b.id);
  });
}

export function groupedBuffers(networkId: string) {
  const netBufs = [...state.buffers.values()].filter((buffer) => buffer.network_id === networkId);
  const status = netBufs.find((buffer) => buffer.kind === "status") ?? null;
  const channels = netBufs.filter((buffer) => buffer.kind === "channel" && buffer.archived !== true).sort(byName);
  const queries = netBufs.filter((buffer) => buffer.kind === "query" && buffer.archived !== true).sort(byName);
  // The Archive section is driven by the persisted archived flag (any kind),
  // not by joined state — a reconnect must not shuffle channels around.
  const parted = netBufs.filter((buffer) => buffer.kind !== "status" && buffer.archived === true).sort(byName);
  return { status, channels, queries, parted };
}

function pushDedup(ids: string[], seen: Set<string>, buffer: Buffer | null | undefined) {
  if (!buffer || seen.has(buffer.id)) return;
  seen.add(buffer.id);
  ids.push(buffer.id);
}

function sortedPinnedChannels(opts: { skipDisabledNetworks: boolean }): Buffer[] {
  return [...state.buffers.values()]
    .filter((buffer) => {
      if (buffer.kind !== "channel" || !buffer.pinned) return false;
      if (opts.skipDisabledNetworks && state.networks.get(buffer.network_id)?.disabled) return false;
      return true;
    })
    .sort((a, b) => {
      const an = state.networks.get(a.network_id)?.sort_order ?? Number.MAX_SAFE_INTEGER;
      const bn = state.networks.get(b.network_id)?.sort_order ?? Number.MAX_SAFE_INTEGER;
      return an - bn || a.name.localeCompare(b.name);
    });
}

export function getVisibleSidebarBufferIds(): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();

  for (const buffer of sortedPinnedChannels({ skipDisabledNetworks: false })) {
    pushDedup(ids, seen, buffer);
  }

  for (const network of orderedNetworks()) {
    if (network.disabled) continue;
    const { status, channels, queries, parted } = groupedBuffers(network.id);
    pushDedup(ids, seen, status);
    if (state.layout.collapsed[network.id]) continue;
    for (const buffer of channels) pushDedup(ids, seen, buffer);
    for (const buffer of queries) pushDedup(ids, seen, buffer);
    if (state.layout.archivesOpen[network.id]) {
      for (const buffer of parted) pushDedup(ids, seen, buffer);
    }
  }

  return ids;
}

export function getStartupFallbackBufferIds(): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();

  for (const buffer of sortedPinnedChannels({ skipDisabledNetworks: true })) {
    pushDedup(ids, seen, buffer);
  }

  for (const network of orderedNetworks()) {
    if (network.disabled) continue;
    const { status, channels, queries, parted } = groupedBuffers(network.id);
    for (const buffer of channels) pushDedup(ids, seen, buffer);
    for (const buffer of queries) pushDedup(ids, seen, buffer);
    for (const buffer of parted) pushDedup(ids, seen, buffer);
    pushDedup(ids, seen, status);
  }

  return ids;
}

export function getAllNavigableBufferIds(): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const network of orderedNetworks()) {
    const { status, channels, queries, parted } = groupedBuffers(network.id);
    pushDedup(ids, seen, status);
    for (const buffer of channels) pushDedup(ids, seen, buffer);
    for (const buffer of queries) pushDedup(ids, seen, buffer);
    for (const buffer of parted) pushDedup(ids, seen, buffer);
  }
  return ids;
}

export function currentNetworkStatusBufferId(): string | null {
  const active = activeBuffer();
  if (!active) return null;
  return groupedBuffers(active.network_id).status?.id ?? null;
}

export type BufferSearchResult = {
  buffer: Buffer;
  network: Network | undefined;
  score: number;
  displayName: string;
};

function scoreMatch(haystack: string, query: string): number {
  if (!query) return 1;
  const idx = haystack.indexOf(query);
  if (idx < 0) return -1;
  if (haystack === query) return 400;
  if (haystack.startsWith(query)) return 300 - Math.min(idx, 50);
  return 200 - Math.min(idx, 100);
}

export function searchBuffers(query: string): BufferSearchResult[] {
  const q = query.trim().toLowerCase();
  const visibleIds = new Set(getVisibleSidebarBufferIds());
  const order = new Map(getAllNavigableBufferIds().map((id, i) => [id, i]));

  const results: BufferSearchResult[] = [];
  for (const id of getAllNavigableBufferIds()) {
    const buffer = state.buffers.get(id);
    if (!buffer) continue;
    const network = state.networks.get(buffer.network_id);
    const displayName = buffer.kind === "status" ? "(status)" : buffer.name;
    const fields = [displayName, network?.name || "", `${displayName} ${network?.name || ""}`].map((s) =>
      s.toLowerCase(),
    );
    const score = q ? Math.max(...fields.map((field) => scoreMatch(field, q))) : 1;
    if (score < 0) continue;
    results.push({ buffer, network, score, displayName });
  }

  results.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    const aHot = (a.buffer.mentions > 0 ? 2 : 0) + (a.buffer.unread > 0 ? 1 : 0);
    const bHot = (b.buffer.mentions > 0 ? 2 : 0) + (b.buffer.unread > 0 ? 1 : 0);
    if (bHot !== aHot) return bHot - aHot;
    const aVisible = visibleIds.has(a.buffer.id) ? 1 : 0;
    const bVisible = visibleIds.has(b.buffer.id) ? 1 : 0;
    if (bVisible !== aVisible) return bVisible - aVisible;
    return (order.get(a.buffer.id) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.buffer.id) ?? Number.MAX_SAFE_INTEGER);
  });

  return results;
}
