import "./style.css";
import "./mobile.css";
import { activeBuffer, type Member, type Message, state } from "./app-state";
import { type AppView, createAppView } from "./app-view";
import { applyChannelListUpdate, type ChannelListUpdate } from "./channel-list";
import { createConnection, type StateSyncDeps, syncStateFromServer } from "./connection";
import { captureDom, type DomRefs } from "./dom";
import { bindInputHandlers, updateInputPopups } from "./input";
import { restoreInputDraft, saveInputDraft } from "./input-history";
import { cleanupKeyboardShortcuts, initKeyboardShortcuts } from "./keyboard-routing";
import { populateMembersForActive } from "./members";
import { inferUnreadCounts } from "./messages";
import { onHashChange } from "./navigation";
import { resetAppState } from "./reset";
import { bufferFromHash, bufferHashFor } from "./router";
import { openSettingsDialog } from "./settings-dialog";
import { openHelpOverlay } from "./shortcuts-help";
import { applyThemeDefaults, initThemeSelector } from "./theme-ui";
import { initTouchGestures } from "./touch-gestures";
import { isMobileViewport, onBackdropClick, setMembersDrawer, setSidebarDrawer } from "./ui-shell";

let dom: DomRefs | null = null;
let appView: AppView | null = null;
let domReady = false;

function mustDom(): DomRefs {
  if (!dom) throw new Error("DOM not captured");
  return dom;
}

function mustView(): AppView {
  if (!appView) throw new Error("app view not initialized");
  return appView;
}

export function start() {
  dom = captureDom();
  domReady = true;
  const d = dom;
  const view = createAppView(d, { sendCmd, setActive, maybeMarkActiveRead });
  appView = view;
  view.renderStatus();
  applyThemeDefaults();
  initThemeSelector().catch((err: unknown) => console.error("theme selector", err));
  bindInputHandlers({
    inputEl: d.inputEl,
    inputForm: d.inputForm,
    uploadInputEl: d.uploadInputEl,
    uploadButtonEl: d.uploadButtonEl,
    cmdPopEl: d.cmdPopEl,
    emojiPopEl: d.emojiPopEl,
    nickPopEl: d.nickPopEl,
    getActiveBuffer: activeBuffer,
    sendCmd,
  });
  d.messagesEl.addEventListener("scroll", onMessagesScroll);
  d.toggleMembersEl.addEventListener("click", () => {
    if (isMobileViewport()) {
      const open = document.body.dataset.membersOpen === "true";
      setMembersDrawer(!open);
      return;
    }
    state.showMemberList = !state.showMemberList;
    view.renderMembers();
  });
  d.mobileMenuEl.addEventListener("click", () => {
    setSidebarDrawer(document.body.dataset.sidebarOpen !== "true");
  });
  d.shortcutsHelpBtnEl.addEventListener("click", () => openHelpOverlay());
  document.getElementById("settings-btn")?.addEventListener("click", () => openSettingsDialog());
  document.addEventListener("click", onBackdropClick);
  initTouchGestures({ sidebarEl: d.sidebarEl });
  initKeyboardShortcuts({ inputEl: d.inputEl, setActive: (id: string) => setActive(id) });
  const onHash = () => onHashChange(setActive);
  window.addEventListener("hashchange", onHash);
  window.addEventListener("popstate", onHash);
  const syncStateDeps: StateSyncDeps = {
    renderPromptNick: view.renderPromptNick,
    inferUnreadCounts,
    renderSidebar: view.renderSidebar,
    renderHeader: view.renderHeader,
    renderActiveView: view.renderActiveView,
    renderMembers: view.renderMembers,
    setActive,
    bufferFromHash,
  };
  const connection = createConnection({
    domReady: () => domReady,
    renderStatus: view.renderStatus,
    renderSidebar: view.renderSidebar,
    updateInputEnabled: view.updateInputEnabled,
    maybeMarkActiveRead,
    syncState: () => syncStateFromServer(syncStateDeps),
    handleMessage: handleWSMessage,
  });
  return connection.hydrate();
}

type WSMessage =
  | ({ type: "message" } & Message)
  | { type: "buffer_created"; id: string; network_id: string; name: string; kind: string }
  | { type: "buffer_update"; id: string; topic?: string; joined?: boolean; last_seen_id?: string }
  | {
      type: "buffer_settings";
      id: string;
      show_embeds: boolean;
      show_presence_events: boolean;
      collapse_presence_events: boolean;
      pinned: boolean;
    }
  | { type: "network_state"; network_id: string; state: string }
  | { type: "history_result"; buffer_id: string; messages?: Message[] }
  | { type: "preview"; buffer_id: string; message_id: string; previews?: Message["previews"] }
  | { type: "member_list"; buffer_id: string; members?: Member[] }
  | ({ type: "channel_list" } & ChannelListUpdate)
  | { type: "ignorelist_result"; req_id: string; network_id: string; masks: string[] };

function handleWSMessage(msg: unknown) {
  const m = msg as WSMessage;
  const view = mustView();
  switch (m.type) {
    case "message":
      view.appendMessage(m);
      break;
    case "buffer_created":
      state.buffers.set(m.id, {
        id: m.id,
        network_id: m.network_id,
        name: m.name,
        kind: m.kind,
        joined: false,
        topic: "",
        unread: 0,
        mentions: 0,
        last_seen_id: "",
        show_embeds: true,
        show_presence_events: true,
        collapse_presence_events: false,
        pinned: false,
      });
      view.renderSidebar();
      break;
    case "buffer_update":
    case "buffer_settings":
      view.updateBuffer(m, { rerenderActive: m.type === "buffer_settings" });
      break;
    case "network_state": {
      const n = state.networks.get(m.network_id);
      if (n && n.status !== m.state) {
        n.status = m.state;
        view.renderSidebar();
        view.renderHeader();
      }
      break;
    }
    case "history_result":
      view.prependHistory(m);
      break;
    case "preview":
      view.patchPreview(m);
      break;
    case "member_list":
      view.setMembers(m.buffer_id, m.members || []);
      break;
    case "channel_list":
      if (applyChannelListUpdate(m)) view.renderChannelList();
      break;
    case "ignorelist_result":
      console.log("ignore list:", m.masks);
      break;
  }
}

function setActive(id: string, opts: { skipHash?: boolean; replaceHash?: boolean } = {}) {
  if (dom && state.activeId !== null) saveInputDraft(state.activeId, dom.inputEl.value, true);
  state.activeId = id;
  state.channelList = null;
  setSidebarDrawer(false);
  if (!opts.skipHash) {
    const b = state.buffers.get(id);
    if (b) {
      const hash = bufferHashFor(b);
      if (hash && hash !== location.hash) {
        if (opts.replaceHash) history.replaceState(null, "", hash);
        else history.pushState(null, "", hash);
      }
    }
  }
  const view = mustView();
  inferUnreadCounts();
  populateMembersForActive();
  view.renderSidebar();
  view.renderHeader();
  view.renderActiveView();
  view.renderMembers();
  view.updateInputEnabled();
  restoreInputDraft(view.dom.inputEl, id);
  updateInputPopups(view.dom.inputEl, view.dom.cmdPopEl, view.dom.emojiPopEl, view.dom.nickPopEl, activeBuffer());
  maybeMarkActiveRead();
  view.dom.inputEl.focus();
}

function onMessagesScroll() {
  const messagesEl = mustDom().messagesEl;
  if (messagesEl.scrollTop <= 40) loadOlderHistory();
  maybeMarkActiveRead();
}

function loadOlderHistory() {
  const bufferId = state.activeId;
  if (!(bufferId && state.wsReady) || state.loadingHistory.has(bufferId) || state.historyExhausted.has(bufferId))
    return;
  const list = state.messages.get(bufferId) || [];
  if (list.length === 0) return;
  state.loadingHistory.add(bufferId);
  sendCmd({ type: "history", buffer_id: bufferId, before: list[0]?.id, limit: 100 });
}

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
  appView?.renderSidebar();
  sendCmd({ type: "mark_read", buffer_id: b.id, message_id: lastId });
}

let reqSeq = 0;
function sendCmd(cmd: Record<string, unknown>) {
  if (!state.ws) return;
  cmd.req_id = cmd.req_id || `r${++reqSeq}`;
  state.ws.send(JSON.stringify(cmd));
}

export function resetForTests() {
  cleanupKeyboardShortcuts();
  resetAppState();
  if (domReady) appView?.renderPromptNick();
  dom = null;
  appView = null;
  domReady = false;
}

export async function initForTests() {
  resetForTests();
  await start();
}

export { handleWSMessage as handleWSMessageForTests };
