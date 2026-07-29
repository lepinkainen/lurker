import { type Buffer, type Network, state } from "./app-state";
import { groupedBuffers, orderedNetworks, sortedPinnedChannels } from "./buffers";

export type NetworkBuffers = ReturnType<typeof groupedBuffers> & {
  all: Buffer[];
  unreadTotal: number;
  mentionTotal: number;
};

export type SidebarNetworkModel = {
  network: Network;
  collapsed: boolean;
  headerActive: boolean;
  buffers: NetworkBuffers;
};

export type SidebarModel = {
  pinned: Buffer[];
  activeNetworks: SidebarNetworkModel[];
  disabledNetworks: Network[];
};

export function pinnedBuffers(): Buffer[] {
  return sortedPinnedChannels({ skipDisabledNetworks: false });
}

export function networkBuffers(networkId: string): NetworkBuffers {
  const grouped = groupedBuffers(networkId);
  const all = [grouped.status, ...grouped.channels, ...grouped.queries, ...grouped.parted].filter(Boolean) as Buffer[];
  return {
    ...grouped,
    all,
    unreadTotal: all.reduce((sum, buffer) => sum + (buffer.unread || 0), 0),
    mentionTotal: all.reduce((sum, buffer) => sum + (buffer.mentions || 0), 0),
  };
}

export function prepareSidebarModel(): SidebarModel {
  const networks = orderedNetworks();
  return {
    pinned: pinnedBuffers(),
    activeNetworks: networks
      .filter((network) => !network.disabled)
      .map((network) => {
        const buffers = networkBuffers(network.id);
        return {
          network,
          collapsed: Boolean(state.layout.collapsed[network.id]),
          headerActive: buffers.status !== null && state.activeId === buffers.status.id,
          buffers,
        };
      }),
    disabledNetworks: networks.filter((network) => network.disabled),
  };
}
