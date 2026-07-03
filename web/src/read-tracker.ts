import { activeBuffer, state } from "./app-state";
import type { AppView } from "./app-view";

export type MarkReadOpts = { render?: boolean };

export type ReadTracker = {
  maybeMarkActiveRead: (opts?: MarkReadOpts) => void;
  markBufferReadOnExit: (bufferId: string | null, opts?: MarkReadOpts) => void;
  clearActiveMarker: () => void;
  loadOlderHistory: () => void;
};

export type ReadTrackerDeps = {
  getView: () => AppView | null;
  sendCmd: (cmd: Record<string, unknown>) => void;
};

export function createReadTracker(deps: ReadTrackerDeps): ReadTracker {
  // Advances the read position (sidebar badge + server mark_read) but leaves
  // the marker anchor alone: the "New messages" line stays visible while the
  // user views the buffer and only clears on exit or explicit Esc.
  function markBufferRead(bufferId: string, opts: MarkReadOpts = {}) {
    const render = opts.render ?? true;
    const b = state.buffers.get(bufferId);
    if (!(state.wsReady && b)) return;
    const list = state.messages.get(bufferId) || [];
    if (list.length === 0) return;
    const lastId = list[list.length - 1]?.id;
    if (lastId === undefined) return;
    const current = b.last_seen_id || "";
    const sent = state.lastMarkedReadId.get(b.id) || "";
    if (lastId <= current || lastId <= sent) return;
    b.last_seen_id = lastId;
    b.unread = 0;
    b.mentions = 0;
    state.lastMarkedReadId.set(b.id, lastId);
    if (render) deps.getView()?.renderSidebar();
    deps.sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
  }

  function clearMarkerAnchor(bufferId: string, render: boolean) {
    if (!state.markerAnchorId.delete(bufferId)) return;
    if (render && bufferId === state.activeId) deps.getView()?.renderActiveView?.();
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
    // With the WS down the read position was NOT reported above; deleting
    // the anchor now would lose the read boundary forever (anchors are only
    // recreated by live arrivals). Keep it until we can actually mark read.
    if (!(state.wsReady && state.buffers.get(bufferId))) return;
    clearMarkerAnchor(bufferId, opts?.render ?? true);
  }

  function clearActiveMarker() {
    const b = activeBuffer();
    if (!b) return;
    markBufferRead(b.id);
    clearMarkerAnchor(b.id, true);
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

  return { maybeMarkActiveRead, markBufferReadOnExit, clearActiveMarker, loadOlderHistory };
}
