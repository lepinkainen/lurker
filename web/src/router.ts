import { type Buffer, state } from "./app-state";

function encSeg(s: string): string {
  return encodeURIComponent(s).replace(/%23/g, "#");
}

export function bufferHashFor(buffer: Buffer): string {
  const network = state.networks.get(buffer.network_id);
  if (!network) return "";
  const net = encSeg(network.name);
  if (buffer.kind === "status") return `#net/${net}`;
  return `#net/${net}/${encSeg(buffer.name)}`;
}

export function bufferFromHash(hash: string): Buffer | null {
  if (!hash?.startsWith("#net/")) return null;
  const parts = hash.slice(5).split("/");
  if (parts.length < 1 || !parts[0]) return null;
  const netName = decodeURIComponent(parts[0]);
  const network = [...state.networks.values()].find((n) => n.name === netName);
  if (!network) return null;
  const bufName = parts[1] ? decodeURIComponent(parts.slice(1).join("/")) : null;
  for (const buffer of state.buffers.values()) {
    if (buffer.network_id !== network.id) continue;
    if (bufName == null && buffer.kind === "status") return buffer;
    if (bufName != null && buffer.name === bufName) return buffer;
  }
  return null;
}
