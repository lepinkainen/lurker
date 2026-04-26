import { loadLayout, state } from "./app-state";

export function resetAppState() {
  try {
    state.ws?.close();
  } catch {}
  state.networks.clear();
  state.buffers.clear();
  state.messages.clear();
  state.members.clear();
  state.activeId = null;
  state.ws = null;
  state.wsReady = false;
  state.loadingHistory.clear();
  state.historyExhausted.clear();
  state.lastMarkedReadId.clear();
  state.me.nick = "you";
  state.showMemberList = true;
  state.layout = loadLayout();
  state.drag = { id: null, over: null };
}
