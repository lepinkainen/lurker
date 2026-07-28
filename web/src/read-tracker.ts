import { state } from "./app-state";
import type { AppView } from "./app-view";

export type ReadTracker = {
  ackBufferRead: (bufferId: string) => void;
  loadOlderHistory: () => void;
};

export type ReadTrackerDeps = {
  getView: () => AppView | null;
  sendCmd: (cmd: Record<string, unknown>) => void;
};

export function createReadTracker(deps: ReadTrackerDeps): ReadTracker {
  // The ONLY path that clears the "New messages" marker (badge + divider +
  // unread bar share one lifecycle; see ai-docs/behaviors/new-messages-marker.md).
  // Triggered exclusively by explicit user acks: Esc or clicking the unread
  // bar. Never call this implicitly (buffer switch, scroll, focus, reconnect).
  function ackBufferRead(bufferId: string) {
    const b = state.buffers.get(bufferId);
    // With the WS down this is a no-op: server state stays authoritative and
    // the resync on reconnect restores the truthful marker.
    if (!(state.wsReady && b)) return;
    const list = state.messages.get(bufferId) || [];
    const lastId = list[list.length - 1]?.id;
    if (lastId === undefined) return;
    // Optimistic local clear; the server persists last_seen_id, recomputes,
    // and broadcasts buffer_update to every client (including this one).
    b.last_seen_id = lastId;
    b.marker_id = undefined;
    b.marker_ts = undefined;
    b.unread = 0;
    b.mentions = 0;
    deps.sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
    const view = deps.getView();
    view?.renderSidebar();
    if (bufferId === state.activeId) view?.renderActiveView();
  }

  function loadOlderHistory() {
    const bufferId = state.activeId;
    if (!(bufferId && state.wsReady) || state.loadingHistory.has(bufferId) || state.historyExhausted.has(bufferId))
      return;
    const list = state.messages.get(bufferId) || [];
    if (list.length === 0) return;
    state.loadingHistory.add(bufferId);
    deps.sendCmd({ type: "history", buffer_id: bufferId, before: list[0]?.id, limit: 100 });
  }

  return { ackBufferRead, loadOlderHistory };
}
