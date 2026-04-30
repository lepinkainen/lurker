import type { Network, ReorderResponse } from "./app-state";

async function parseNetworkResponse(res: Response): Promise<Network> {
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  return (await res.json()) as Network;
}

export async function setNetworkDisabled(id: number, disabled: boolean): Promise<Network> {
  const res = await fetch(`/api/networks/${id}`, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ disabled }),
  });
  return parseNetworkResponse(res);
}

export async function reorderNetworks(ids: number[]): Promise<ReorderResponse> {
  const res = await fetch("/api/networks/reorder", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ ids }),
  });
  if (!res.ok) throw new Error(`reorder ${res.status}`);
  return (await res.json()) as ReorderResponse;
}
