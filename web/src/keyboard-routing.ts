import { saveLayout, state } from "./app-state";
import { currentNetworkStatusBufferId } from "./buffers";
import { closeChannelSwitcher, isChannelSwitcherOpen, openChannelSwitcher } from "./channel-switcher";
import {
  focusInput,
  navigateBuffer,
  navigateFirstUnread,
  navigateMention,
  navigateUnread,
} from "./keyboard-navigation";
import { closeHelpOverlay, isHelpOverlayOpen, openHelpOverlay } from "./shortcuts-help";
import { setDesktopSidebarHidden, setMembersDrawer, setSidebarDrawer, toggleSidebarVisibility } from "./ui-shell";

export type KeyboardShortcutsDeps = {
  inputEl: HTMLInputElement;
  // Keyboard-driven buffer switches focus the input so typing continues
  // uninterrupted, even on touch devices with a hardware keyboard (e.g. iPad).
  setActive: (id: string) => void;
};

let cleanupKeydown: (() => void) | null = null;

function isTextEditingElement(target: EventTarget | null): boolean {
  const el = target instanceof HTMLElement ? target : null;
  if (!el) return false;
  if (el.closest("dialog[data-overlay=channel-switcher]")) return false;
  if (el instanceof HTMLTextAreaElement) return true;
  if (el instanceof HTMLInputElement) {
    return !["button", "checkbox", "radio", "submit", "file", "range", "color"].includes(el.type);
  }
  return el.isContentEditable || Boolean(el.closest("[contenteditable=true]"));
}

function closeTopOverlay() {
  return closeChannelSwitcher() || closeHelpOverlay();
}

function isOverlayOpen() {
  return isChannelSwitcherOpen() || isHelpOverlayOpen();
}

export function initKeyboardShortcuts(deps: KeyboardShortcutsDeps) {
  cleanupKeyboardShortcuts();
  setDesktopSidebarHidden(state.layout.sidebarHidden);
  const onKeydown = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      if (closeTopOverlay()) {
        e.preventDefault();
        return;
      }
      setSidebarDrawer(false);
      setMembersDrawer(false);
      return;
    }

    if (isOverlayOpen()) return;

    const editing = isTextEditingElement(e.target);
    if (!(editing || e.ctrlKey || e.metaKey || e.altKey) && e.key === "?") {
      e.preventDefault();
      openHelpOverlay();
      return;
    }

    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
      e.preventDefault();
      openChannelSwitcher(deps, closeHelpOverlay);
      return;
    }
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "b") {
      e.preventDefault();
      state.layout.sidebarHidden = toggleSidebarVisibility(state.layout.sidebarHidden);
      saveLayout(state.layout);
      return;
    }
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "l") {
      e.preventDefault();
      focusInput(deps.inputEl);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key === "ArrowUp") {
      e.preventDefault();
      navigateBuffer(deps.setActive, true);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key === "ArrowDown") {
      e.preventDefault();
      navigateBuffer(deps.setActive);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && e.shiftKey && e.key === "ArrowUp") {
      e.preventDefault();
      navigateUnread(deps.setActive, true);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && e.shiftKey && e.key === "ArrowDown") {
      e.preventDefault();
      navigateUnread(deps.setActive);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key.toLowerCase() === "a") {
      e.preventDefault();
      navigateFirstUnread(deps.setActive);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key.toLowerCase() === "m") {
      e.preventDefault();
      navigateMention(deps.setActive);
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.key.toLowerCase() === "s") {
      const statusId = currentNetworkStatusBufferId();
      if (statusId !== null && statusId !== state.activeId) {
        e.preventDefault();
        deps.setActive(statusId);
      }
    }
  };
  document.addEventListener("keydown", onKeydown);
  cleanupKeydown = () => document.removeEventListener("keydown", onKeydown);
}

export function cleanupKeyboardShortcuts() {
  cleanupKeydown?.();
  cleanupKeydown = null;
  closeChannelSwitcher();
  closeHelpOverlay();
}
