import type { Network, ReorderResponse } from "./app-state";
import { sendJSON } from "./http";

export function setNetworkDisabled(id: number, disabled: boolean): Promise<Network> {
  return sendJSON<Network>(`/api/networks/${id}`, "PATCH", { disabled });
}

export function reorderNetworks(ids: number[]): Promise<ReorderResponse> {
  return sendJSON<ReorderResponse>("/api/networks/reorder", "POST", { ids });
}
