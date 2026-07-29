import type { Network, PinnedReorderResponse, ReorderResponse } from "./app-state";
import { sendJSON } from "./http";

export function setNetworkDisabled(id: string, disabled: boolean): Promise<Network> {
  return sendJSON<Network>(`/api/networks/${id}`, "PATCH", { disabled });
}

export function reorderNetworks(ids: string[]): Promise<ReorderResponse> {
  return sendJSON<ReorderResponse>("/api/networks/reorder", "POST", { ids });
}

// Full ordered list of pinned buffer ids. 404s against a backend predating
// pin_order — callers must roll their optimistic order back on failure.
export function reorderPinnedBuffers(ids: string[]): Promise<PinnedReorderResponse> {
  return sendJSON<PinnedReorderResponse>("/api/buffers/pinned/reorder", "POST", { ids });
}
