import { closeChannelSwitcher } from "./channel-switcher";
import { buildDialog, canOpenModal, detachedRef } from "./keyboard-dialogs";

let helpDialog: HTMLDialogElement | null = null;

const APPLE_PLATFORM_RE = /Mac|iPhone|iPad|iPod/iu;

function macLabels() {
  const platform = `${navigator.platform || ""} ${navigator.userAgent || ""}`;
  const mac = APPLE_PLATFORM_RE.test(platform);
  return {
    mod: mac ? "Cmd" : "Ctrl",
    combo: (key: string) => `${mac ? "Cmd" : "Ctrl"}+${key}`,
  };
}

export function isHelpOverlayOpen() {
  helpDialog = detachedRef(helpDialog);
  return helpDialog !== null;
}

export function closeHelpOverlay() {
  if (!helpDialog) return false;
  helpDialog.close();
  return true;
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
  helpDialog = detachedRef(helpDialog);
  if (helpDialog) {
    helpDialog.focus();
    return;
  }
  closeChannelSwitcher();
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
  close.addEventListener("click", () => closeHelpOverlay());

  const header = document.createElement("div");
  header.className = "kh-header";
  header.append(title, close);

  const altShift = (key: string) => ["Alt", "Shift", key].join("+");

  card.append(
    header,
    helpSection("General", [
      ["?", "Open keyboard shortcuts"],
      [combo("K"), "Open channel switcher"],
      ["Esc", "Close active overlay / clear new-messages marker"],
    ]),
    helpSection("Buffer navigation", [
      ["Alt+↑", "Previous buffer"],
      ["Alt+↓", "Next buffer"],
      [altShift("↑"), "Previous unread buffer"],
      [altShift("↓"), "Next unread buffer"],
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
