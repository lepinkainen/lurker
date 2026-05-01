import "./style.css";
import "./mobile.css";
import { activeBuffer, type ChannelListEntry, type Member, type Message, type Network, state } from "./app-state";
import { renderChannelListPanel } from "./channel-list";
import { connectWS, hydrate, nextReconnectDelay, scheduleReconnect, type WebSocketDeps } from "./connection";
import { captureDom, type DomRefs } from "./dom";
import { bindInputHandlers, updateInputEnabled } from "./input";
import { updateCmdPop } from "./input-command-popup";
import { restoreInputDraft, saveInputDraft } from "./input-history";
import { cleanupKeyboardShortcuts, initKeyboardShortcuts } from "./keyboard-routing";
import { populateMembersForActive, renderMembers } from "./members";
import {
  inferUnreadCounts,
  onBufferUpdate,
  onHistoryResult,
  onMessage,
  onPreview,
  renderActiveView,
  renderHeader,
} from "./messages";
import { onHashChange } from "./navigation";
import { nickAvatar } from "./nick";
import { resetAppState } from "./reset";
import { bufferFromHash, bufferHashFor } from "./router";
import { openSettingsDialog } from "./settings-dialog";
import { openHelpOverlay } from "./shortcuts-help";
import { renderSidebar } from "./sidebar";
import { renderSidebarStatus } from "./status";
import { applyThemeDefaults, initThemeSelector } from "./theme-ui";
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
  const view = createAppView(d);
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
  initKeyboardShortcuts({ inputEl: d.inputEl, setActive: (id: number) => setActive(id) });
  const onHash = () => onHashChange(setActive);
  window.addEventListener("hashchange", onHash);
  window.addEventListener("popstate", onHash);
  const connectionDeps = createConnectionDeps(view);
  return hydrate(connectionDeps);
}

type AppView = ReturnType<typeof createAppView>;

function createConnectionDeps(view: AppView): WebSocketDeps {
  const deps: WebSocketDeps = {
    domReady: () => domReady,
    renderSidebarStatus: view.renderStatus,
    renderPromptNick: view.renderPromptNick,
    renderSidebar: view.renderSidebar,
    renderHeader: view.renderHeader,
    renderActiveView: view.renderActiveView,
    renderMembers: view.renderMembers,
    updateInputEnabled: view.updateInputEnabled,
    maybeMarkActiveRead,
    inferUnreadCounts,
    scheduleReconnect: (delayMs: number) =>
      scheduleReconnect(
        {
          domReady: () => domReady,
          renderSidebarStatus: view.renderStatus,
          connectWS: () => connectWS(deps),
        },
        delayMs,
      ),
    nextReconnectDelay,
    setActive,
    bufferFromHash,
    handleWSMessage,
  };
  return deps;
}

function createAppView(d: DomRefs) {
  const messageArea = {
    messagesEl: d.messagesEl,
    statusViewEl: d.statusViewEl,
    bufferNameEl: d.bufferNameEl,
    bufferTopicEl: d.bufferTopicEl,
    inputEl: d.inputEl,
  };
  const view = {
    dom: d,
    renderStatus: () => renderSidebarStatus(d),
    renderPromptNick: () => {
      const nick = activePromptNick();
      d.inputNickEl.replaceChildren(nickAvatar(nick), nick);
    },
    renderHeader: () => renderHeader(messageArea, { renderPromptNick: view.renderPromptNick, iconEl }),
    renderActiveView: () => {
      if (state.channelList?.done) {
        d.statusViewEl.hidden = true;
        d.messagesEl.hidden = false;
        renderChannelListPanel(d.messagesEl, { sendCmd, renderActiveView: view.renderActiveView });
        return;
      }
      renderActiveView(messageArea, { renderPromptNick: view.renderPromptNick, iconEl });
    },
    renderMembers: () =>
      renderMembers({
        memberPaneEl: d.memberPaneEl,
        toggleMembersEl: d.toggleMembersEl,
        memberCountEl: d.memberCountEl,
        memberCountInlineEl: d.memberCountInlineEl,
        bufferMemcountEl: d.bufferMemcountEl,
        memberListEl: d.memberListEl,
      }),
    updateInputEnabled: () => updateInputEnabled(d.inputEl),
    renderSidebar: () => renderSidebar({ sbScrollEl: d.sbScrollEl, setActive, iconEl }),
  };
  return view;
}

type WSMessage =
  | ({ type: "message" } & Message)
  | { type: "buffer_created"; id: number; network_id: number; name: string; kind: string }
  | { type: "buffer_update"; id: number; topic?: string; joined?: boolean; last_seen_id?: number }
  | {
      type: "buffer_settings";
      id: number;
      show_embeds: boolean;
      show_presence_events: boolean;
      collapse_presence_events: boolean;
      pinned: boolean;
    }
  | { type: "network_state"; network_id: number; state: string }
  | { type: "history_result"; buffer_id: number; messages?: Message[] }
  | { type: "preview"; buffer_id: number; message_id: number; previews?: Message["previews"] }
  | { type: "member_list"; buffer_id: number; members?: Member[] }
  | { type: "channel_list"; network_id: number; entries: ChannelListEntry[]; done: boolean }
  | { type: "ignorelist_result"; req_id: string; network_id: number; masks: string[] };

function handleWSMessage(msg: unknown) {
  const m = msg as WSMessage;
  const view = mustView();
  switch (m.type) {
    case "message":
      onMessage(m, {
        renderActiveView: view.renderActiveView,
        maybeMarkActiveRead,
        renderSidebar: view.renderSidebar,
      });
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
        last_seen_id: 0,
        show_embeds: true,
        show_presence_events: true,
        collapse_presence_events: false,
        pinned: false,
      });
      view.renderSidebar();
      break;
    case "buffer_update":
    case "buffer_settings":
      onBufferUpdate(m, { inferUnreadCounts, renderHeader: view.renderHeader, renderSidebar: view.renderSidebar });
      if (m.type === "buffer_settings") view.renderActiveView();
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
      onHistoryResult(m, { renderActiveView: view.renderActiveView }, view.dom.messagesEl);
      break;
    case "preview":
      onPreview(m, view.dom.messagesEl);
      break;
    case "member_list":
      state.members.set(m.buffer_id, m.members || []);
      if (m.buffer_id === state.activeId) {
        view.renderHeader();
        view.renderMembers();
      }
      break;
    case "channel_list":
      if (!state.channelList || state.channelList.network_id !== m.network_id) {
        state.channelList = { network_id: m.network_id, entries: [], done: false };
      }
      state.channelList.entries.push(...m.entries);
      state.channelList.done = m.done;
      if (m.done && m.network_id === state.networks.get(activeBuffer()?.network_id ?? -1)?.id) {
        renderChannelListPanel(view.dom.messagesEl, { sendCmd, renderActiveView: view.renderActiveView });
      }
      break;
    case "ignorelist_result":
      console.log("ignore list:", m.masks);
      break;
  }
}

const SVG_NS = "http://www.w3.org/2000/svg";

function iconEl(symbolId: string, size: number, opts: { className?: string; label?: string } = {}): SVGSVGElement {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("class", opts.className ? `icon ${opts.className}` : "icon");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("focusable", "false");
  if (opts.label) {
    svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", opts.label);
    const title = document.createElementNS(SVG_NS, "title");
    title.textContent = opts.label;
    svg.appendChild(title);
  } else {
    svg.setAttribute("aria-hidden", "true");
  }
  const useEl = document.createElementNS(SVG_NS, "use");
  useEl.setAttribute("href", `#${symbolId}`);
  svg.appendChild(useEl);
  return svg;
}

function activePromptNick(): string {
  const b = activeBuffer();
  if (b) {
    const n = state.networks.get(b.network_id) as (Network & { nick?: string }) | undefined;
    if (n?.nick) return n.nick;
  }
  return state.me.nick || "";
}

function setActive(id: number, opts: { skipHash?: boolean; replaceHash?: boolean } = {}) {
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
  updateCmdPop(view.dom.inputEl, view.dom.cmdPopEl);
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
  const list = state.messages.get(state.activeId ?? -1) || [];
  if (!(state.wsReady && b) || list.length === 0) return;
  const lastId = list[list.length - 1]?.id;
  if (lastId === undefined) return;
  const current = b.last_seen_id || 0;
  const sent = state.lastMarkedReadId.get(b.id) || 0;
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
