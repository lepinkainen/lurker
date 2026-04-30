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
  dom.tailscaleStatusEl.className = "sb-status-item good";
  dom.tailscaleStatusTextEl.textContent = "Connected";

  const showUpdate = state.updateStatus?.enabled === true && state.updateStatus.update_available === true;
  dom.updateStatusEl.hidden = !showUpdate;
  if (showUpdate) {
    dom.updateStatusEl.className = "sb-status-item warn";
    dom.updateStatusTextEl.textContent = state.updateStatus?.remote_version
      ? `Update available (${state.updateStatus.remote_version})`
      : "Update available";
  }
}
