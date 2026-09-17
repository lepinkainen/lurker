import "./style.css";
import "./mobile.css";
import { createSetActive } from "./active-buffer";
import { activeBuffer, state } from "./app-state";
import { type AppView, createAppView } from "./app-view";
import { createConnection, type StateSyncDeps, syncStateFromServer } from "./connection";
import { captureDom, type DomRefs } from "./dom";
import { bindInputHandlers } from "./input";
import { cleanupKeyboardShortcuts, initKeyboardShortcuts } from "./keyboard-routing";
import { onHashChange } from "./navigation";
import { createReadTracker } from "./read-tracker";
import { resetAppState } from "./reset";
import { bufferFromHash } from "./router";
import { createScrollStick } from "./scroll-stick";
import { openSettingsView } from "./settings-dialog";
import { openHelpOverlay } from "./shortcuts-help";
import { applyThemeDefaults, initTheme } from "./theme-ui";
import { initTouchGestures } from "./touch-gestures";
import { isMobileViewport, onBackdropClick, setMembersDrawer, setSidebarDrawer } from "./ui-shell";
import { createWSRouter } from "./ws-router";

let dom: DomRefs | null = null;
let appView: AppView | null = null;
let routeWSMessage: ((msg: unknown) => void) | null = null;
let domReady = false;

export function start() {
  dom = captureDom();
  domReady = true;
  const d = dom;
  const { ackBufferRead, loadOlderHistory } = createReadTracker({
    getView: () => appView,
    sendCmd,
  });
  const setActive = createSetActive({
    getDom: () => dom,
    getView: () => appView,
  });
  const stick = createScrollStick(d.messagesEl);
  const view = createAppView(d, { sendCmd, setActive, ackBufferRead, stick });
  appView = view;
  routeWSMessage = createWSRouter(view, sendCmd);
  view.renderStatus();
  applyThemeDefaults();
  initTheme().catch((err: unknown) => console.error("theme init", err));
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
  d.messagesEl.addEventListener("scroll", () => {
    if (d.messagesEl.scrollTop <= 40) loadOlderHistory();
  });
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
  d.bufferOptionsBtnEl.addEventListener("click", () => view.openActiveBufferOptions());
  document.getElementById("settings-btn")?.addEventListener("click", () => openSettingsView());
  document.addEventListener("click", onBackdropClick);
  initTouchGestures({ sidebarEl: d.sidebarEl });
  initKeyboardShortcuts({
    inputEl: d.inputEl,
    setActive: (id: string) => setActive(id, { focusInput: true }),
    ackActiveBuffer: () => {
      if (state.activeId !== null) ackBufferRead(state.activeId);
    },
  });
  const onHash = () => onHashChange(setActive);
  window.addEventListener("hashchange", onHash);
  window.addEventListener("popstate", onHash);
  const syncStateDeps: StateSyncDeps = {
    renderer: {
      renderPromptNick: view.renderPromptNick,
      renderSidebar: view.renderSidebar,
      renderHeader: view.renderHeader,
      renderActiveView: view.renderActiveView,
      renderMembers: view.renderMembers,
    },
    navigation: { setActive, bufferFromHash },
  };
  const connection = createConnection({
    domReady: () => domReady,
    renderer: {
      renderStatus: view.renderStatus,
      renderSidebar: view.renderSidebar,
      updateInputEnabled: view.updateInputEnabled,
    },
    transport: {
      syncState: () => syncStateFromServer(syncStateDeps),
      handleMessage: (msg) => routeWSMessage?.(msg),
    },
  });
  return connection.hydrate();
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
  routeWSMessage = null;
  domReady = false;
}

export async function initForTests() {
  resetForTests();
  await start();
}

export function handleWSMessageForTests(msg: unknown) {
  routeWSMessage?.(msg);
}
