import { searchBuffers } from "./buffers";
import { buildDialog, canOpenModal } from "./keyboard-dialogs";
import type { KeyboardShortcutsDeps } from "./keyboard-routing";

let switcherDialog: HTMLDialogElement | null = null;

export function isChannelSwitcherOpen() {
  return switcherDialog !== null;
}

export function closeChannelSwitcher() {
  if (!switcherDialog) return false;
  switcherDialog.close();
  return true;
}

function renderSwitcherResults(
  listEl: HTMLElement,
  emptyEl: HTMLElement,
  query: string,
  selection: { index: number },
  choose: (bufferId: number) => void,
) {
  const results = searchBuffers(query);
  if (results.length === 0) {
    listEl.replaceChildren();
    listEl.hidden = true;
    emptyEl.hidden = false;
    selection.index = -1;
    return results;
  }
  listEl.hidden = false;
  emptyEl.hidden = true;
  if (selection.index < 0 || selection.index >= results.length) selection.index = 0;
  const rows: HTMLButtonElement[] = [];
  const applySelection = (newIndex: number) => {
    const prev = rows[selection.index];
    if (prev) {
      prev.classList.remove("selected");
      prev.setAttribute("aria-selected", "false");
    }
    selection.index = newIndex;
    const curr = rows[newIndex];
    if (curr) {
      curr.classList.add("selected");
      curr.setAttribute("aria-selected", "true");
    }
  };
  listEl.replaceChildren(
    ...results.map((result, index) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `ks-result${index === selection.index ? " selected" : ""}`;
      row.setAttribute("role", "option");
      row.setAttribute("aria-selected", index === selection.index ? "true" : "false");
      rows.push(row);

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
        applySelection(index);
      });
      row.addEventListener("click", () => choose(result.buffer.id));
      return row;
    }),
  );
  return results;
}

export function openChannelSwitcher(deps: KeyboardShortcutsDeps, beforeOpen?: () => void) {
  if (switcherDialog) {
    const input = switcherDialog.querySelector<HTMLInputElement>(".ks-input");
    input?.focus();
    input?.select();
    return;
  }
  beforeOpen?.();
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
    closeChannelSwitcher();
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
      if (results.length === 0) return;
      selection.index = (selection.index + 1 + results.length) % results.length;
      results = renderSwitcherResults(list, empty, input.value, selection, choose);
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (results.length === 0) return;
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
      closeChannelSwitcher();
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
