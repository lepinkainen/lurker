import { state } from "./app-state";
import type { DomRefs } from "./dom";

export function renderSidebarStatus(dom: DomRefs) {
  let backend: { cls: string; text: string };
  if (state.backendStatus === "connected") {
    backend = { cls: "good", text: "Connected" };
  } else if (state.backendStatus === "connecting") {
    backend = { cls: "warn", text: "Connecting…" };
  } else if (state.backendStatus === "reconnecting") {
    const secs = state.reconnectAt === null ? null : Math.max(1, Math.ceil((state.reconnectAt - Date.now()) / 1000));
    backend = { cls: "warn", text: secs === null ? "Reconnecting…" : `Reconnecting in ${secs}s` };
  } else {
    backend = { cls: "bad", text: "Offline" };
  }

  dom.backendStatusEl.className = `sb-status-item ${backend.cls}`;
  dom.backendStatusTextEl.textContent = backend.text;
  const ts = state.tailscaleStatus;
  let tsClass = "warn";
  let tsText = "Unknown";
  let tsTitle = "Tailscale connectivity status";
  if (ts) {
    tsTitle = `Connected from ${ts.remote_ip}`;
    switch (ts.status) {
      case "tailscale":
        tsClass = "good";
        tsText = "Tailscale";
        break;
      case "local":
        tsClass = "good";
        tsText = "Local";
        break;
      case "external":
        tsClass = "bad";
        tsText = "Public";
        break;
      default:
        tsClass = "warn";
        tsText = "Unknown";
    }
  }
  dom.tailscaleStatusEl.className = `sb-status-item ${tsClass}`;
  dom.tailscaleStatusEl.title = tsTitle;
  dom.tailscaleStatusTextEl.textContent = tsText;

  const showUpdate = state.updateStatus?.enabled === true && state.updateStatus.update_available === true;
  dom.updateStatusEl.hidden = !showUpdate;
  if (showUpdate) {
    dom.updateStatusEl.className = "sb-status-item warn";
    dom.updateStatusTextEl.textContent = state.updateStatus?.remote_version ?? "available";
  }
}
