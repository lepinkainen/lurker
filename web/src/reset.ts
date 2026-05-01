import { loadLayout, state } from "./app-state";

export function resetAppState() {
  try {
    state.ws?.close();
  } catch {
    // Reset should continue even if the browser rejects a close attempt.
  }
  state.networks.clear();
  state.buffers.clear();
  state.messages.clear();
  state.members.clear();
  state.inputHistory.clear();
  state.activeId = null;
  state.ws = null;
  state.wsReady = false;
  if (state.wsHealthTimer !== null) {
    window.clearInterval(state.wsHealthTimer);
    state.wsHealthTimer = null;
  }
  state.lastWSActivityAt = 0;
  state.needsStateSyncOnConnect = false;
  state.loadingHistory.clear();
  state.historyExhausted.clear();
  state.lastMarkedReadId.clear();
  state.me.nick = "you";
  state.showMemberList = true;
  state.layout = loadLayout();
  state.drag = { id: null, over: null };
}
