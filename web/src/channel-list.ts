import { activeBuffer, type ChannelListEntry, state } from "./app-state";

export type ChannelListUpdate = {
  network_id: string;
  entries: ChannelListEntry[];
  done: boolean;
};

export type ChannelListPanelDeps = {
  sendCmd: (cmd: Record<string, unknown>) => void;
  renderActiveView: () => void;
};

export function applyChannelListUpdate(update: ChannelListUpdate): boolean {
  if (!state.channelList || state.channelList.network_id !== update.network_id) {
    state.channelList = { network_id: update.network_id, entries: [], done: false };
  }
  state.channelList.entries.push(...update.entries);
  state.channelList.done = update.done;
  const active = activeBuffer();
  return update.done && active?.network_id === update.network_id;
}

export function tryRenderActiveChannelList(
  messagesEl: HTMLElement,
  statusViewEl: HTMLElement,
  deps: ChannelListPanelDeps,
): boolean {
  if (!state.channelList?.done) return false;
  statusViewEl.hidden = true;
  messagesEl.hidden = false;
  renderChannelListPanel(messagesEl, deps);
  return true;
}

function closeChannelList(deps: ChannelListPanelDeps) {
  state.channelList = null;
  deps.renderActiveView();
}

export function renderChannelListPanel(messagesEl: HTMLElement, deps: ChannelListPanelDeps) {
  if (!state.channelList) return;
  const { network_id, entries } = state.channelList;
  messagesEl.innerHTML = "";
  const panel = document.createElement("div");
  panel.className = "cl-panel";

  const header = document.createElement("div");
  header.className = "cl-header";
  const title = document.createElement("span");
  title.textContent = `${entries.length} channels`;
  const closeBtn = document.createElement("button");
  closeBtn.className = "cl-close";
  closeBtn.textContent = "✕";
  closeBtn.addEventListener("click", () => closeChannelList(deps));
  header.append(title, closeBtn);
  panel.appendChild(header);

  const list = document.createElement("div");
  list.className = "cl-list";
  for (const entry of entries) {
    const row = document.createElement("div");
    row.className = "cl-row";
    const name = document.createElement("span");
    name.className = "cl-name";
    name.textContent = entry.name;
    const count = document.createElement("span");
    count.className = "cl-count";
    count.textContent = String(entry.count);
    const topic = document.createElement("span");
    topic.className = "cl-topic";
    topic.textContent = entry.topic || "";
    const joinBtn = document.createElement("button");
    joinBtn.className = "cl-join";
    joinBtn.textContent = "Join";
    joinBtn.addEventListener("click", () => {
      deps.sendCmd({ type: "join", network_id, channel: entry.name });
      closeChannelList(deps);
    });
    row.append(name, count, topic, joinBtn);
    list.appendChild(row);
  }
  panel.appendChild(list);
  messagesEl.appendChild(panel);
}
