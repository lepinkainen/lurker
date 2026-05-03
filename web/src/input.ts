import { activeBuffer, type Buffer, state } from "./app-state";
import { handleEmojiKey, initEmojiPop, resetEmojiAutocomplete, updateEmojiPop } from "./emoji-autocomplete";
import { initCmdPop, updateCmdPop } from "./input-command-popup";
import { handleHistoryKey, recordSentInput, saveInputDraft } from "./input-history";
import { bindUploadHandlers, type InputUploadDeps } from "./input-upload";
import { handleSlashCommand } from "./slash-commands";

export type InputDeps = InputUploadDeps & {
  getActiveBuffer: () => Buffer | undefined;
  sendCmd: (cmd: Record<string, unknown>) => void;
  emojiPopEl: HTMLElement;
};

export function updateInputEnabled(inputEl: HTMLInputElement) {
  const buffer = activeBuffer();
  inputEl.disabled = !(
    state.wsReady &&
    buffer &&
    buffer.kind !== "status" &&
    !(buffer.kind === "channel" && buffer.joined !== true)
  );
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
      updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl);
    }
    return;
  }
  deps.sendCmd({ type: "send", buffer_id: buffer.id, content: text });
  recordSentInput(buffer.id, text);
  deps.inputEl.value = "";
  updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl);
  resetEmojiAutocomplete();
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

function updateInputPopups(inputEl: HTMLInputElement, cmdPopEl: HTMLElement, emojiPopEl: HTMLElement) {
  updateCmdPop(inputEl, cmdPopEl);
  if (updateEmojiPop(inputEl, emojiPopEl)) cmdPopEl.hidden = true;
}

export function bindInputHandlers(deps: InputDeps) {
  initCmdPop(deps.inputEl, deps.cmdPopEl);
  initEmojiPop(deps.inputEl, deps.emojiPopEl);
  deps.inputEl.addEventListener("input", () => {
    const bufferId = deps.getActiveBuffer()?.id ?? null;
    saveInputDraft(bufferId, deps.inputEl.value);
    updateInputPopups(deps.inputEl, deps.cmdPopEl, deps.emojiPopEl);
  });
  deps.inputEl.addEventListener("keydown", (ev) => {
    if (handleEmojiKey(ev, deps.inputEl, deps.emojiPopEl)) return;
    handleHistoryKey(ev, deps.inputEl, deps.cmdPopEl, deps.getActiveBuffer()?.id ?? null);
  });
  bindFormatShortcuts(deps.inputEl);
  bindUploadHandlers(deps);
  deps.inputForm.addEventListener("submit", (ev) => onSubmit(ev, deps));
}
