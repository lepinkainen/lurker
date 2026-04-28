import { type Buffer, SLASH_COMMANDS, state } from "./app-state";
import { escapeHTML } from "./format";

export type InputDeps = {
  inputEl: HTMLInputElement;
  inputForm: HTMLFormElement;
  uploadInputEl: HTMLInputElement;
  uploadButtonEl: HTMLButtonElement;
  cmdPopEl: HTMLElement;
  getActiveBuffer: () => Buffer | undefined;
  sendCmd: (cmd: Record<string, unknown>) => void;
};

export function updateInputEnabled(inputEl: HTMLInputElement) {
  const buffer = state.buffers.get(state.activeId);
  inputEl.disabled = !(
    state.wsReady &&
    buffer &&
    buffer.kind !== "status" &&
    !(buffer.kind === "channel" && buffer.joined !== true)
  );
}

export function handleSlashCommand(text: string, buffer: Buffer, sendCmd: InputDeps["sendCmd"]): boolean {
  const [cmd, ...rest] = text.slice(1).split(/\s+/);
  const args = rest.join(" ").trim();
  const networkId = buffer.network_id;

  switch ((cmd || "").toLowerCase()) {
    case "join":
      sendCmd({ type: "join", network_id: networkId, channel: args });
      return true;
    case "part":
      sendCmd({ type: "part", buffer_id: buffer.id, content: args });
      return true;
    case "msg": {
      const [target, ...msgRest] = rest;
      if (target) sendCmd({ type: "msg", network_id: networkId, target, content: msgRest.join(" ") });
      return true;
    }
    case "me":
      sendCmd({ type: "me", buffer_id: buffer.id, content: args });
      return true;
    case "nick":
      sendCmd({ type: "nick", network_id: networkId, content: args });
      return true;
    case "topic":
      sendCmd({ type: "topic", buffer_id: buffer.id, content: args });
      return true;
    case "whois":
      sendCmd({ type: "whois", network_id: networkId, target: args });
      return true;
    case "invite": {
      const [nick, chan] = rest;
      sendCmd({ type: "invite", network_id: networkId, target: nick, channel: chan || buffer.name });
      return true;
    }
    case "kick": {
      const [nick, ...reason] = rest;
      sendCmd({ type: "kick", buffer_id: buffer.id, target: nick, content: reason.join(" ") });
      return true;
    }
    case "mode":
      sendCmd({ type: "mode", buffer_id: buffer.id, content: args });
      return true;
    case "raw":
      sendCmd({ type: "raw", network_id: networkId, content: args });
      return true;
    case "away":
      sendCmd({ type: "away", network_id: networkId, content: args });
      return true;
    case "back":
      sendCmd({ type: "back", network_id: networkId });
      return true;
    case "quit":
      sendCmd({ type: "quit", network_id: networkId, content: args });
      return true;
    case "rejoin":
    case "cycle":
      sendCmd({ type: "rejoin", buffer_id: buffer.id });
      return true;
    case "notice": {
      const [target, ...msgRest] = rest;
      if (target) sendCmd({ type: "notice", network_id: networkId, target, content: msgRest.join(" ") });
      return true;
    }
    case "ctcp": {
      const [nick, ctcpCmd, ...ctcpArgs] = rest;
      if (nick && ctcpCmd)
        sendCmd({
          type: "ctcp",
          network_id: networkId,
          target: nick,
          content: ctcpCmd + (ctcpArgs.length ? ` ${ctcpArgs.join(" ")}` : ""),
        });
      return true;
    }
    case "query":
      sendCmd({ type: "query", network_id: networkId, target: args });
      return true;
    case "list":
      sendCmd({ type: "list", network_id: networkId, content: args });
      return true;
    case "op":
      sendCmd({ type: "op", buffer_id: buffer.id, target: args });
      return true;
    case "deop":
      sendCmd({ type: "deop", buffer_id: buffer.id, target: args });
      return true;
    case "voice":
      sendCmd({ type: "voice", buffer_id: buffer.id, target: args });
      return true;
    case "devoice":
      sendCmd({ type: "devoice", buffer_id: buffer.id, target: args });
      return true;
    case "ban":
      sendCmd({ type: "ban", buffer_id: buffer.id, target: args });
      return true;
    case "unban":
      sendCmd({ type: "unban", buffer_id: buffer.id, target: args });
      return true;
    case "banlist":
      sendCmd({ type: "banlist", buffer_id: buffer.id });
      return true;
    case "kickban": {
      const [nick, ...reason] = rest;
      if (nick) sendCmd({ type: "kickban", buffer_id: buffer.id, target: nick, content: reason.join(" ") });
      return true;
    }
    case "ignore":
      sendCmd({ type: "ignore", network_id: networkId, target: args });
      return true;
    case "unignore":
      sendCmd({ type: "unignore", network_id: networkId, target: args });
      return true;
    case "ignorelist":
      sendCmd({ type: "ignorelist", network_id: networkId });
      return true;
    default:
      return false;
  }
}

export function updateCmdPop(inputEl: HTMLInputElement, cmdPopEl: HTMLElement) {
  const value = inputEl.value;
  if (!value.startsWith("/")) {
    cmdPopEl.hidden = true;
    return;
  }
  const query = value.slice(1).split(/\s+/)[0].toLowerCase();
  const matches = SLASH_COMMANDS.filter((command) => command.cmd.slice(1).startsWith(query));
  if (!matches.length) {
    cmdPopEl.hidden = true;
    return;
  }
  cmdPopEl.innerHTML = "";
  matches.forEach((command, i) => {
    const row = document.createElement("div");
    row.className = `ci ${i === 0 ? "hl" : ""}`;
    row.innerHTML = `<span class="c">${command.cmd} <span style="color:var(--fg-2);font-weight:400">${escapeHTML(command.args)}</span></span><span class="d">${escapeHTML(command.desc)}</span>`;
    cmdPopEl.appendChild(row);
  });
  cmdPopEl.hidden = false;
}

export function onSubmit(ev: SubmitEvent, deps: InputDeps) {
  ev.preventDefault();
  const text = deps.inputEl.value.trim();
  if (!text || !state.wsReady) return;
  const buffer = deps.getActiveBuffer();
  if (!buffer) return;
  if (text.startsWith("/")) {
    if (handleSlashCommand(text, buffer, deps.sendCmd)) {
      deps.inputEl.value = "";
      updateCmdPop(deps.inputEl, deps.cmdPopEl);
    }
    return;
  }
  deps.sendCmd({ type: "send", buffer_id: buffer.id, content: text });
  deps.inputEl.value = "";
  updateCmdPop(deps.inputEl, deps.cmdPopEl);
}

export function initCmdPop(inputEl: HTMLInputElement, cmdPopEl: HTMLElement) {
  inputEl.addEventListener("input", () => updateCmdPop(inputEl, cmdPopEl));
  inputEl.addEventListener("blur", () =>
    setTimeout(() => {
      cmdPopEl.hidden = true;
    }, 100),
  );
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

export function insertTextAtCursor(inputEl: HTMLInputElement, text: string) {
  const start = inputEl.selectionStart ?? inputEl.value.length;
  const end = inputEl.selectionEnd ?? start;
  const before = inputEl.value.slice(0, start);
  const after = inputEl.value.slice(end);
  const prefix = before && !/\s$/.test(before) ? " " : "";
  const suffix = after && !/^\s/.test(after) ? " " : "";
  const inserted = `${prefix}${text}${suffix}`;
  inputEl.value = before + inserted + after;
  const caret = before.length + inserted.length;
  inputEl.setSelectionRange(caret, caret);
}

export async function uploadFile(file: File): Promise<string> {
  const form = new FormData();
  form.set("file", file);
  const res = await fetch("/api/upload", { method: "POST", body: form });
  if (!res.ok) {
    const detail = (await res.text()).trim();
    throw new Error(detail || `upload failed (${res.status})`);
  }
  const data = (await res.json()) as { url?: string };
  if (!data.url) throw new Error("upload response missing url");
  return data.url;
}

async function uploadAndInsert(file: File, deps: InputDeps) {
  deps.uploadButtonEl.disabled = true;
  deps.inputForm.classList.add("uploading");
  try {
    const url = await uploadFile(file);
    insertTextAtCursor(deps.inputEl, url);
    deps.inputEl.focus();
    updateCmdPop(deps.inputEl, deps.cmdPopEl);
  } catch (err) {
    console.error("upload failed", err);
  } finally {
    deps.uploadButtonEl.disabled = false;
    deps.inputForm.classList.remove("uploading");
  }
}

function bindUploadHandlers(deps: InputDeps) {
  deps.uploadButtonEl.addEventListener("click", () => deps.uploadInputEl.click());
  deps.uploadInputEl.addEventListener("change", () => {
    const file = deps.uploadInputEl.files?.[0];
    deps.uploadInputEl.value = "";
    if (file) void uploadAndInsert(file, deps);
  });
  deps.inputForm.addEventListener("dragover", (ev) => {
    if (!ev.dataTransfer?.files?.length) return;
    ev.preventDefault();
    deps.inputForm.classList.add("upload-dragover");
  });
  deps.inputForm.addEventListener("dragleave", () => deps.inputForm.classList.remove("upload-dragover"));
  deps.inputForm.addEventListener("drop", (ev) => {
    deps.inputForm.classList.remove("upload-dragover");
    const file = ev.dataTransfer?.files?.[0];
    if (!file) return;
    ev.preventDefault();
    void uploadAndInsert(file, deps);
  });
}

export function bindInputHandlers(deps: InputDeps) {
  initCmdPop(deps.inputEl, deps.cmdPopEl);
  bindFormatShortcuts(deps.inputEl);
  bindUploadHandlers(deps);
  deps.inputForm.addEventListener("submit", (ev) => onSubmit(ev, deps));
}
