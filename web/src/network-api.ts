import type { Network, ReorderResponse } from "./app-state";
import { sendJSON } from "./http";

export function setNetworkDisabled(id: string, disabled: boolean): Promise<Network> {
  return sendJSON<Network>(`/api/networks/${id}`, "PATCH", { disabled });
}

export function reorderNetworks(ids: string[]): Promise<ReorderResponse> {
  return sendJSON<ReorderResponse>("/api/networks/reorder", "POST", { ids });
}
