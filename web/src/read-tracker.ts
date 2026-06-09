import { activeBuffer, state } from "./app-state";
import type { AppView } from "./app-view";

export type MarkReadOpts = { render?: boolean };

export type ReadTracker = {
  maybeMarkActiveRead: (opts?: MarkReadOpts) => void;
  markBufferReadOnExit: (bufferId: string | null, opts?: MarkReadOpts) => void;
  loadOlderHistory: () => void;
};

export type ReadTrackerDeps = {
  getView: () => AppView | null;
  sendCmd: (cmd: Record<string, unknown>) => void;
};

export function createReadTracker(deps: ReadTrackerDeps): ReadTracker {
  function markBufferRead(bufferId: string, opts: MarkReadOpts = {}) {
    const render = opts.render ?? true;
    const b = state.buffers.get(bufferId);
    if (!(state.wsReady && b)) return;
    const list = state.messages.get(bufferId) || [];
    if (list.length === 0) return;
    const lastId = list[list.length - 1]?.id;
    if (lastId === undefined) return;
    // Drop any stale anchor unconditionally — a markBufferRead call means we
    // consider this buffer caught up, even when the no-op guard below skips
    // the network send.
    const hadAnchor = state.markerAnchorId.delete(b.id);
    const view = deps.getView();
    const current = b.last_seen_id || "";
    const sent = state.lastMarkedReadId.get(b.id) || "";
    if (lastId <= current || lastId <= sent) {
      if (hadAnchor && render) {
        view?.renderSidebar();
        if (bufferId === state.activeId) view?.renderActiveView?.();
      }
      return;
    }
    b.last_seen_id = lastId;
    b.unread = 0;
    b.mentions = 0;
    state.lastMarkedReadId.set(b.id, lastId);
    if (render) {
      view?.renderSidebar();
      if (hadAnchor && bufferId === state.activeId) view?.renderActiveView?.();
    }
    deps.sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
  }

  function maybeMarkActiveRead(opts?: MarkReadOpts) {
    if (!state.uiFocused) return;
    const b = activeBuffer();
    if (!b) return;
    markBufferRead(b.id, opts);
  }

  function markBufferReadOnExit(bufferId: string | null, opts?: MarkReadOpts) {
    if (!bufferId) return;
    if (!state.activeFocusedSinceEnter) return;
    markBufferRead(bufferId, opts);
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

  return { maybeMarkActiveRead, markBufferReadOnExit, loadOlderHistory };
}
