import { isUIFocused, state } from "./app-state";

export type FocusDeps = {
  renderSidebar: () => void;
  maybeMarkActiveRead: (opts?: { render?: boolean }) => void;
};

let bound = false;
let handler: (() => void) | null = null;

export { isUIFocused };

export function initFocusTracking(deps: FocusDeps) {
  if (bound) return;
  bound = true;
  handler = () => {
    const focused = isUIFocused();
    const becameFocused = focused && !state.uiFocused;
    state.uiFocused = focused;
    if (becameFocused && state.activeId !== null) {
      state.activeFocusedSinceEnter = true;
      deps.maybeMarkActiveRead({ render: false });
    }
    deps.renderSidebar();
  };
  document.addEventListener("visibilitychange", handler);
  window.addEventListener("focus", handler);
  window.addEventListener("blur", handler);
}

export function cleanupFocusTracking() {
  if (!(bound && handler)) return;
  document.removeEventListener("visibilitychange", handler);
  window.removeEventListener("focus", handler);
  window.removeEventListener("blur", handler);
  handler = null;
  bound = false;
}
