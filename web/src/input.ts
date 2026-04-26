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
  switch ((cmd || "").toLowerCase()) {
    case "join":
      sendCmd({ type: "join", network_id: buffer.network_id, channel: rest.join(" ").trim() });
      return true;
    case "part":
      sendCmd({ type: "part", buffer_id: buffer.id, content: rest.join(" ").trim() });
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

export function bindInputHandlers(deps: InputDeps) {
  initCmdPop(deps.inputEl, deps.cmdPopEl);
  deps.inputForm.addEventListener("submit", (ev) => onSubmit(ev, deps));
}
