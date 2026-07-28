import { activeBuffer, saveLastActive, state } from "./app-state";
import type { AppView } from "./app-view";
import type { DomRefs } from "./dom";
import { updateInputPopups } from "./input";
import { restoreInputDraft, saveInputDraft } from "./input-history";
import { populateMembersForActive } from "./members";
import { bufferHashFor } from "./router";
import { prefersNoAutoFocus, setSidebarDrawer } from "./ui-shell";

export type SetActiveOpts = { skipHash?: boolean; replaceHash?: boolean; focusInput?: boolean };
export type SetActive = (id: string, opts?: SetActiveOpts) => void;

export type SetActiveDeps = {
  getDom: () => DomRefs | null;
  getView: () => AppView | null;
};

export function createSetActive(deps: SetActiveDeps): SetActive {
  return (id: string, opts: SetActiveOpts = {}) => {
    const view = deps.getView();
    if (!view) throw new Error("app view not initialized");
    const dom = deps.getDom();
    if (dom && state.activeId !== null) saveInputDraft(state.activeId, dom.inputEl.value, true);
    // Switching buffers never sends mark_read: the marker clears only on an
    // explicit user ack (Esc / unread bar click). See
    // ai-docs/behaviors/new-messages-marker.md.
    state.activeId = id;
    saveLastActive(id);
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
    populateMembersForActive();
    view.renderSidebar();
    view.renderHeader();
    view.renderActiveView();
    view.renderMembers();
    view.updateInputEnabled();
    restoreInputDraft(view.dom.inputEl, id);
    updateInputPopups(view.dom.inputEl, view.dom.cmdPopEl, view.dom.emojiPopEl, view.dom.nickPopEl, activeBuffer());
    // Keyboard-driven switches (focusInput) always focus so the user can keep
    // typing. Pointer-driven switches skip focus on touch devices to avoid
    // summoning the on-screen keyboard.
    const shouldFocus = opts.focusInput ?? !prefersNoAutoFocus();
    if (shouldFocus) view.dom.inputEl.focus();
  };
}
