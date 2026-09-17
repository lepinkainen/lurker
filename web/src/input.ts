import { activeBuffer, type Buffer, state } from "./app-state";
import { handleEmojiKey, initEmojiPop, resetEmojiAutocomplete, updateEmojiPop } from "./emoji-autocomplete";
import { initCmdPop, updateCmdPop } from "./input-command-popup";
import { handleHistoryKey, recordSentInput, saveInputDraft } from "./input-history";
import { bindUploadHandlers, type InputUploadDeps } from "./input-upload";
import { handleNickKey, initNickPop, resetNickAutocomplete, updateNickPop } from "./nick-autocomplete";
import { handleSlashCommand } from "./slash-commands";

export type InputDeps = InputUploadDeps & {
  getActiveBuffer: () => Buffer | undefined;
  sendCmd: (cmd: Record<string, unknown>) => void;
  emojiPopEl: HTMLElement;
  nickPopEl: HTMLElement;
};

// Outbound "send" text by req_id, so a server error can hand the text back
// to the composer instead of it silently vanishing. Acks drop entries; the cap
// bounds the map if acks never arrive (old backend).
export type PendingSend = { bufferId: string; text: string };
const pendingSends = new Map<string, PendingSend>();
const MAX_PENDING_SENDS = 50;

export function recordPendingSend(reqId: string, bufferId: string, text: string) {
  pendingSends.set(reqId, { bufferId, text });
  if (pendingSends.size > MAX_PENDING_SENDS) {
    const oldest = pendingSends.keys().next().value;
    if (oldest !== undefined) pendingSends.delete(oldest);
  }
}

export function takePendingSend(reqId: string | undefined): PendingSend | undefined {
  if (!reqId) return undefined;
  const pending = pendingSends.get(reqId);
  pendingSends.delete(reqId);
  return pending;
}

export function updateInputEnabled(inputEl: HTMLInputElement) {
  const buffer = activeBuffer();
  inputEl.disabled = !(state.wsReady && buffer && !(buffer.kind === "channel" && buffer.joined !== true));
}

export function onSubmit(ev: SubmitEvent, deps: InputDeps) {
  ev.preventDefault();
  const text = deps.inputEl.value.trim();
  if (!(text && state.wsReady)) return;
  const buffer = deps.getActiveBuffer();
  if (!buffer) return;
  if (text.startsWith("/")) {
    if (handleSlashCommand(text, buffer, deps.sendCmd)) {
      deps.inputEl.value = "";
      saveInputDraft(buffer.id, "");
      updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl, deps.nickPopEl, buffer);
    }
    return;
  }
  // Status buffer has no channel/query target: only slash commands
  // (/nick, /list, /msg, /raw, NickServ via /msg, …) make sense there.
  // The backend rejects plain "send" to a status buffer, so drop it here.
  if (buffer.kind === "status") return;
  const cmd: Record<string, unknown> = { type: "send", buffer_id: buffer.id, content: text };
  deps.sendCmd(cmd);
  if (typeof cmd.req_id === "string") recordPendingSend(cmd.req_id, buffer.id, text);
  recordSentInput(buffer.id, text);
  deps.inputEl.value = "";
  updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl, deps.nickPopEl, buffer);
  resetEmojiAutocomplete();
  resetNickAutocomplete();
}

const FORMAT_KEYS: Record<string, string> = {
  b: "\x02",
  i: "\x1d",
  u: "\x1f",
  s: "\x1e",
  m: "\x11",
  o: "\x0f",
};

export function handleFormatKey(ev: KeyboardEvent, inputEl: HTMLInputElement): boolean {
  if (!(ev.ctrlKey || ev.metaKey) || ev.altKey || ev.shiftKey) return false;
  const byte = FORMAT_KEYS[ev.key.toLowerCase()];
  if (!byte) return false;
  ev.preventDefault();
  const start = inputEl.selectionStart ?? inputEl.value.length;
  const end = inputEl.selectionEnd ?? start;
  const value = inputEl.value;
  inputEl.value = value.slice(0, start) + byte + value.slice(end);
  const caret = start + byte.length;
  inputEl.setSelectionRange(caret, caret);
  return true;
}

export function bindFormatShortcuts(inputEl: HTMLInputElement) {
  inputEl.addEventListener("keydown", (ev) => handleFormatKey(ev, inputEl));
}

export function updateInputPopups(
  inputEl: HTMLInputElement,
  cmdPopEl: HTMLElement,
  emojiPopEl: HTMLElement,
  nickPopEl: HTMLElement,
  buffer: Buffer | undefined,
) {
  if (updateEmojiPop(inputEl, emojiPopEl)) {
    cmdPopEl.hidden = true;
    nickPopEl.hidden = true;
    resetNickAutocomplete();
    return;
  }

  updateCmdPop(inputEl, cmdPopEl);
  if (!cmdPopEl.hidden) {
    nickPopEl.hidden = true;
    resetNickAutocomplete();
    return;
  }

  updateNickPop(inputEl, nickPopEl, buffer);
}

export function bindInputHandlers(deps: InputDeps) {
  initCmdPop(deps.inputEl, deps.cmdPopEl);
  initEmojiPop(deps.inputEl, deps.emojiPopEl);
  initNickPop(deps.inputEl, deps.nickPopEl, deps.getActiveBuffer);
  deps.inputEl.addEventListener("input", () => {
    const buffer = deps.getActiveBuffer();
    saveInputDraft(buffer?.id ?? null, deps.inputEl.value);
    updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl, deps.nickPopEl, buffer);
  });
  deps.inputEl.addEventListener("keydown", (ev) => {
    if (handleEmojiKey(ev, deps.inputEl, deps.emojiPopEl)) return;
    if (handleNickKey(ev, deps.inputEl, deps.nickPopEl, deps.getActiveBuffer())) return;
    handleHistoryKey(ev, deps.inputEl, deps.cmdPopEl, deps.getActiveBuffer()?.id ?? null);
  });
  bindFormatShortcuts(deps.inputEl);
  bindUploadHandlers(deps);
  deps.inputForm.addEventListener("submit", (ev) => onSubmit(ev, deps));
}
