import { activeBuffer, state } from "./app-state";
import type { AppView } from "./app-view";

export type ReadTracker = {
  maybeMarkActiveRead: () => void;
  loadOlderHistory: () => void;
};

export type ReadTrackerDeps = {
  getView: () => AppView | null;
  sendCmd: (cmd: Record<string, unknown>) => void;
};

export function createReadTracker(deps: ReadTrackerDeps): ReadTracker {
  function maybeMarkActiveRead() {
    const b = activeBuffer();
    const list = state.messages.get(state.activeId ?? "") || [];
    if (!(state.wsReady && b) || list.length === 0) return;
    const lastId = list[list.length - 1]?.id;
    if (lastId === undefined) return;
    const current = b.last_seen_id || "";
    const sent = state.lastMarkedReadId.get(b.id) || "";
    if (lastId <= current || lastId <= sent) return;
    b.last_seen_id = lastId;
    b.unread = 0;
    b.mentions = 0;
    state.lastMarkedReadId.set(b.id, lastId);
    deps.getView()?.renderSidebar();
    deps.sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
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

  return { maybeMarkActiveRead, loadOlderHistory };
}
