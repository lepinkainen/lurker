import { type Buffer, SLASH_COMMANDS, state } from "./app-state";
import { escapeHTML } from "./format";

export type InputDeps = {
  inputEl: HTMLInputElement;
  inputForm: HTMLFormElement;
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
          content: ctcpCmd + (ctcpArgs.length ? " " + ctcpArgs.join(" ") : ""),
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

export function bindInputHandlers(deps: InputDeps) {
  initCmdPop(deps.inputEl, deps.cmdPopEl);
  bindFormatShortcuts(deps.inputEl);
  deps.inputForm.addEventListener("submit", (ev) => onSubmit(ev, deps));
}
