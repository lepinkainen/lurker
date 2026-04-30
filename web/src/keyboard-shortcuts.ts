import { saveLayout, state } from "./app-state";
import { currentNetworkStatusBufferId, getVisibleSidebarBufferIds, searchBuffers } from "./buffers";
import { setDesktopSidebarHidden, setMembersDrawer, setSidebarDrawer, toggleSidebarVisibility } from "./ui-shell";

export type KeyboardShortcutsDeps = {
  inputEl: HTMLInputElement;
  setActive: (id: number) => void;
};

let switcherDialog: HTMLDialogElement | null = null;
let helpDialog: HTMLDialogElement | null = null;
let cleanupKeydown: (() => void) | null = null;

function macLabels() {
  const platform = `${navigator.platform || ""} ${navigator.userAgent || ""}`;
  const mac = /Mac|iPhone|iPad|iPod/i.test(platform);
  return {
    mod: mac ? "Cmd" : "Ctrl",
    combo: (key: string) => `${mac ? "Cmd" : "Ctrl"}+${key}`,
  };
}

function isTextEditingElement(target: EventTarget | null): boolean {
  const el = target instanceof HTMLElement ? target : null;
  if (!el) return false;
  if (el.closest("dialog[data-overlay=channel-switcher]")) return false;
  if (el instanceof HTMLTextAreaElement) return true;
  if (el instanceof HTMLInputElement) {
    return !["button", "checkbox", "radio", "submit", "file", "range", "color"].includes(el.type);
  }
  return el.isContentEditable || !!el.closest("[contenteditable=true]");
}

function closeSwitcher() {
  if (!switcherDialog) return false;
  switcherDialog.close();
  return true;
}

function closeHelp() {
  if (!helpDialog) return false;
  helpDialog.close();
  return true;
}

function closeTopOverlay() {
  return closeSwitcher() || closeHelp();
}

function nextMatchingBufferId(
  startId: number | null,
  ids: number[],
  predicate: (id: number) => boolean,
): number | null {
  if (!ids.length) return null;
  const startIndex = startId == null ? -1 : ids.indexOf(startId);
  for (let step = 1; step <= ids.length; step += 1) {
    const id = ids[(Math.max(startIndex, -1) + step) % ids.length];
    if (id !== startId && predicate(id)) return id;
  }
  return null;
}

function previousMatchingBufferId(
  startId: number | null,
  ids: number[],
  predicate: (id: number) => boolean,
): number | null {
  if (!ids.length) return null;
  const startIndex = startId == null ? 0 : ids.indexOf(startId);
  for (let step = 1; step <= ids.length; step += 1) {
    const index = (Math.max(startIndex, 0) - step + ids.length) % ids.length;
    const id = ids[index];
    if (id !== startId && predicate(id)) return id;
  }
  return null;
}

function buildDialog(className: string, overlay: string) {
  const dialog = document.createElement("dialog");
  dialog.className = className;
  dialog.dataset.overlay = overlay;
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });
  dialog.addEventListener("cancel", (e) => {
    e.preventDefault();
    dialog.close();
  });
  return dialog;
}

function canOpenModal() {
  const open = document.querySelector("dialog[open]");
  return !open;
}

function renderSwitcherResults(
  listEl: HTMLElement,
  emptyEl: HTMLElement,
  query: string,
  selection: { index: number },
  choose: (bufferId: number) => void,
) {
  const results = searchBuffers(query);
  if (!results.length) {
    listEl.replaceChildren();
    listEl.hidden = true;
    emptyEl.hidden = false;
    selection.index = -1;
    return results;
  }
  listEl.hidden = false;
  emptyEl.hidden = true;
  if (selection.index < 0 || selection.index >= results.length) selection.index = 0;
  listEl.replaceChildren(
    ...results.map((result, index) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `ks-result${index === selection.index ? " selected" : ""}`;
      row.setAttribute("role", "option");
      row.setAttribute("aria-selected", index === selection.index ? "true" : "false");

      const label = document.createElement("span");
      label.className = "ks-result-main";
      label.textContent = result.displayName;

      const meta = document.createElement("span");
      meta.className = "ks-result-meta";
      meta.textContent = result.network?.name || "";

      const flags = document.createElement("span");
      flags.className = "ks-result-flags";
      if (result.buffer.mentions > 0) {
        const mention = document.createElement("span");
        mention.className = "ks-flag mention";
        mention.textContent = `${result.buffer.mentions} mention`;
        flags.appendChild(mention);
      } else if (result.buffer.unread > 0) {
        const unread = document.createElement("span");
        unread.className = "ks-flag unread";
        unread.textContent = `${result.buffer.unread} unread`;
        flags.appendChild(unread);
      }
      if (result.buffer.kind === "status") {
        const status = document.createElement("span");
        status.className = "ks-flag";
        status.textContent = "status";
        flags.appendChild(status);
      }

      row.append(label, meta, flags);
      row.addEventListener("mousemove", () => {
        if (selection.index === index) return;
        selection.index = index;
        renderSwitcherResults(listEl, emptyEl, query, selection, choose);
      });
      row.addEventListener("click", () => choose(result.buffer.id));
      return row;
    }),
  );
  return results;
}

function openChannelSwitcher(deps: KeyboardShortcutsDeps) {
  if (switcherDialog) {
    const input = switcherDialog.querySelector<HTMLInputElement>(".ks-input");
    input?.focus();
    input?.select();
    return;
  }
  closeHelp();
  if (!canOpenModal()) return;

  const dialog = buildDialog("ks-dialog", "channel-switcher");
  switcherDialog = dialog;

  const card = document.createElement("div");
  card.className = "ks-card";

  const title = document.createElement("h2");
  title.className = "ks-title";
  title.textContent = "Jump to buffer";

  const input = document.createElement("input");
  input.type = "text";
  input.className = "ks-input";
  input.placeholder = "Search buffers";
  input.setAttribute("aria-label", "Search buffers");
  input.autocomplete = "off";

  const list = document.createElement("div");
  list.className = "ks-results";
  list.setAttribute("role", "listbox");

  const empty = document.createElement("div");
  empty.className = "ks-empty";
  empty.textContent = "No matching buffers";
  empty.hidden = true;

  card.append(title, input, list, empty);
  dialog.appendChild(card);
  document.body.appendChild(dialog);

  const selection = { index: 0 };
  const choose = (bufferId: number) => {
    closeSwitcher();
    deps.setActive(bufferId);
  };
  let results = renderSwitcherResults(list, empty, "", selection, choose);

  input.addEventListener("input", () => {
    selection.index = 0;
    results = renderSwitcherResults(list, empty, input.value, selection, choose);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (!results.length) return;
      selection.index = (selection.index + 1 + results.length) % results.length;
      results = renderSwitcherResults(list, empty, input.value, selection, choose);
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (!results.length) return;
      selection.index = (selection.index - 1 + results.length) % results.length;
      results = renderSwitcherResults(list, empty, input.value, selection, choose);
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const picked = results[selection.index];
      if (picked) choose(picked.buffer.id);
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      closeSwitcher();
    }
  });

  dialog.addEventListener("close", () => {
    switcherDialog = null;
    dialog.remove();
  });

  dialog.showModal();
  input.focus();
  input.select();
}

function helpRow(keys: string, desc: string) {
  const row = document.createElement("div");
  row.className = "kh-row";
  const keyEl = document.createElement("span");
  keyEl.className = "kh-keys";
  keyEl.textContent = keys;
  const descEl = document.createElement("span");
  descEl.className = "kh-desc";
  descEl.textContent = desc;
  row.append(keyEl, descEl);
  return row;
}

function helpSection(titleText: string, rows: Array<[string, string]>) {
  const section = document.createElement("section");
  section.className = "kh-section";
  const title = document.createElement("h3");
  title.className = "kh-title";
  title.textContent = titleText;
  section.append(title, ...rows.map(([keys, desc]) => helpRow(keys, desc)));
  return section;
}

export function openHelpOverlay() {
  if (helpDialog) {
    helpDialog.focus();
    return;
  }
  closeSwitcher();
  if (!canOpenModal()) return;

  const { combo } = macLabels();
  const dialog = buildDialog("kh-dialog", "keyboard-help");
  helpDialog = dialog;

  const card = document.createElement("div");
  card.className = "kh-card";
  const title = document.createElement("h2");
  title.className = "kh-heading";
  title.textContent = "Keyboard shortcuts";
  const close = document.createElement("button");
  close.type = "button";
  close.className = "kh-close";
  close.textContent = "Close";
  close.addEventListener("click", () => closeHelp());

  const header = document.createElement("div");
  header.className = "kh-header";
  header.append(title, close);

  card.append(
    header,
    helpSection("General", [
      ["?", "Open keyboard shortcuts"],
      [combo("K"), "Open channel switcher"],
      ["Esc", "Close active overlay"],
    ]),
    helpSection("Buffer navigation", [
      ["Alt+↑", "Previous buffer"],
      ["Alt+↓", "Next buffer"],
      ["Alt+Shift+↑", "Previous unread buffer"],
      ["Alt+Shift+↓", "Next unread buffer"],
      ["Alt+A", "Jump to first unread buffer"],
      ["Alt+M", "Next mention buffer"],
      ["Alt+S", "Jump to status buffer"],
    ]),
    helpSection("Layout and input", [
      [combo("B"), "Toggle sidebar"],
      [combo("L"), "Focus message input"],
    ]),
    helpSection("Channel switcher", [["↑ / ↓ / Enter / Esc", "Move, jump, and close"]]),
  );

  dialog.appendChild(card);
  document.body.appendChild(dialog);
  dialog.addEventListener("close", () => {
    helpDialog = null;
    dialog.remove();
  });
  dialog.showModal();
  close.focus();
}

function navigateBuffer(setActive: (id: number) => void, previous = false) {
  const ids = getVisibleSidebarBufferIds();
  const next = previous
    ? previousMatchingBufferId(state.activeId, ids, () => true)
    : nextMatchingBufferId(state.activeId, ids, () => true);
  if (next != null) setActive(next);
}

function isUnreadMessageBuffer(id: number): boolean {
  const buffer = state.buffers.get(id);
  return !!buffer && buffer.kind !== "status" && buffer.unread > 0;
}

function navigateUnread(setActive: (id: number) => void, previous = false) {
  const ids = getVisibleSidebarBufferIds();
  const next = previous
    ? previousMatchingBufferId(state.activeId, ids, isUnreadMessageBuffer)
    : nextMatchingBufferId(state.activeId, ids, isUnreadMessageBuffer);
  if (next != null) setActive(next);
}

function navigateMention(setActive: (id: number) => void) {
  const ids = getVisibleSidebarBufferIds();
  const next = nextMatchingBufferId(state.activeId, ids, (id) => (state.buffers.get(id)?.mentions || 0) > 0);
  if (next != null) setActive(next);
}

function navigateFirstUnread(setActive: (id: number) => void) {
  const firstUnread = getVisibleSidebarBufferIds().find(isUnreadMessageBuffer);
  if (firstUnread != null && firstUnread !== state.activeId) setActive(firstUnread);
}

function focusInput(inputEl: HTMLInputElement) {
  if (inputEl.disabled) return;
  inputEl.focus();
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

    if (switcherDialog || helpDialog) return;

    const editing = isTextEditingElement(e.target);
    if (!editing && !e.ctrlKey && !e.metaKey && !e.altKey && e.key === "?") {
      e.preventDefault();
      openHelpOverlay();
      return;
    }

    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
      e.preventDefault();
      openChannelSwitcher(deps);
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
      if (statusId != null && statusId !== state.activeId) {
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
  closeSwitcher();
  closeHelp();
}
