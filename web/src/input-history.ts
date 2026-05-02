import { type BufferInputState, state } from "./app-state";
import { updateCmdPop } from "./input-command-popup";

const MAX_INPUT_HISTORY = 100;

function bufferInputState(bufferId: string): BufferInputState {
  let entry = state.inputHistory.get(bufferId);
  if (!entry) {
    entry = { entries: [], draft: "", index: null };
    state.inputHistory.set(bufferId, entry);
  }
  return entry;
}

export function saveInputDraft(bufferId: string | null, draft: string, resetBrowse = false) {
  if (bufferId === null) return;
  const entry = bufferInputState(bufferId);
  if (resetBrowse) entry.index = null;
  if (entry.index === null) entry.draft = draft;
}

export function restoreInputDraft(inputEl: HTMLInputElement, bufferId: string | null) {
  inputEl.value = bufferId === null ? "" : bufferInputState(bufferId).draft;
}

export function recordSentInput(bufferId: string, text: string) {
  const entry = bufferInputState(bufferId);
  entry.entries.push(text);
  if (entry.entries.length > MAX_INPUT_HISTORY) entry.entries.splice(0, entry.entries.length - MAX_INPUT_HISTORY);
  entry.draft = "";
  entry.index = null;
}

export function handleHistoryKey(
  ev: KeyboardEvent,
  inputEl: HTMLInputElement,
  cmdPopEl: HTMLElement,
  bufferId: string | null,
): boolean {
  if ((ev.key !== "ArrowUp" && ev.key !== "ArrowDown") || ev.altKey || ev.ctrlKey || ev.metaKey || ev.shiftKey) {
    return false;
  }
  if (bufferId === null) return false;
  const entry = bufferInputState(bufferId);
  if (ev.key === "ArrowUp") {
    if (entry.entries.length === 0) return false;
    ev.preventDefault();
    if (entry.index === null) {
      entry.draft = inputEl.value;
      entry.index = entry.entries.length - 1;
    } else if (entry.index > 0) {
      entry.index -= 1;
    }
    inputEl.value = entry.entries[entry.index] || "";
    inputEl.setSelectionRange(inputEl.value.length, inputEl.value.length);
    updateCmdPop(inputEl, cmdPopEl);
    return true;
  }
  if (entry.index === null) return false;
  ev.preventDefault();
  if (entry.index < entry.entries.length - 1) {
    entry.index += 1;
    inputEl.value = entry.entries[entry.index] || "";
  } else {
    entry.index = null;
    inputEl.value = entry.draft;
  }
  inputEl.setSelectionRange(inputEl.value.length, inputEl.value.length);
  updateCmdPop(inputEl, cmdPopEl);
  return true;
}
