import { type Buffer, state } from "./app-state";

const MAX_RESULTS = 12;
const WHITESPACE_RE = /\s/u;
const LEADING_WHITESPACE_RE = /^\s/u;
const LEADING_WHITESPACES_RE = /^\s+/u;

export type NickAutocompleteState = {
  /** Index of highlighted result, -1 when no popup or invalid. */
  selected: number;
  /** Start position of the first-token nick query. */
  tokenStart: number;
  /** End position of the full first token in the input value. */
  tokenEnd: number;
  /** Matched nicks for current query. */
  matches: string[];
};

let stateNick: NickAutocompleteState = { selected: -1, tokenStart: -1, tokenEnd: -1, matches: [] };

function emptyState(): NickAutocompleteState {
  return { selected: -1, tokenStart: -1, tokenEnd: -1, matches: [] };
}

export function getNickAutocompleteState(): NickAutocompleteState {
  return stateNick;
}

export function resetNickAutocomplete() {
  stateNick = emptyState();
}

function tokenAtCursor(value: string, cursor: number): { query: string; start: number; end: number } | null {
  if (cursor <= 0 || LEADING_WHITESPACE_RE.test(value)) return null;
  const firstWhitespace = value.search(WHITESPACE_RE);
  const tokenEnd = firstWhitespace === -1 ? value.length : firstWhitespace;
  if (cursor > tokenEnd) return null;

  const query = value.slice(0, cursor);
  if (!query || query.startsWith("/") || query.startsWith(":")) return null;
  return { query, start: 0, end: tokenEnd };
}

function networkSelfNick(buffer: Buffer): string {
  return state.networks.get(buffer.network_id)?.nick?.toLowerCase() ?? "";
}

function matchNicks(buffer: Buffer, query: string): string[] {
  if (buffer.kind !== "channel") return [];
  const members = state.members.get(buffer.id) || [];
  const lower = query.toLowerCase();
  const selfNick = networkSelfNick(buffer);
  const seen = new Set<string>();
  const prefixes: string[] = [];
  const contains: string[] = [];

  for (const member of members) {
    const nick = member.nick;
    const nickLower = nick.toLowerCase();
    if (!nick || member.self || (selfNick !== "" && nickLower === selfNick) || seen.has(nickLower)) continue;
    seen.add(nickLower);
    if (nickLower.startsWith(lower)) prefixes.push(nick);
    else if (nickLower.includes(lower)) contains.push(nick);
  }

  return [...prefixes, ...contains].slice(0, MAX_RESULTS);
}

export function updateNickPop(inputEl: HTMLInputElement, popEl: HTMLElement, buffer: Buffer | undefined): boolean {
  const cursor = inputEl.selectionStart ?? inputEl.value.length;
  const selectionEnd = inputEl.selectionEnd ?? cursor;
  const tk = cursor === selectionEnd ? tokenAtCursor(inputEl.value, cursor) : null;

  if (!(buffer && tk)) {
    popEl.hidden = true;
    stateNick = emptyState();
    return false;
  }

  const matches = matchNicks(buffer, tk.query);
  if (matches.length === 0) {
    popEl.hidden = true;
    stateNick = emptyState();
    return false;
  }

  popEl.innerHTML = "";
  const frag = document.createDocumentFragment();
  matches.forEach((nick, i) => {
    const row = document.createElement("div");
    row.className = `ni ${i === 0 ? "hl" : ""}`;
    const label = document.createElement("span");
    label.className = "nn";
    label.textContent = nick;
    row.appendChild(label);
    frag.appendChild(row);
  });
  popEl.appendChild(frag);
  popEl.hidden = false;

  stateNick = { selected: 0, tokenStart: tk.start, tokenEnd: tk.end, matches };
  return true;
}

export function replaceNickToken(inputEl: HTMLInputElement, nick: string): boolean {
  if (!nick) return false;
  const before = inputEl.value.slice(0, stateNick.tokenStart);
  const after = inputEl.value.slice(stateNick.tokenEnd).replace(LEADING_WHITESPACES_RE, "");
  const inserted = `${nick}: `;
  inputEl.value = before + inserted + after;
  const caret = before.length + inserted.length;
  inputEl.setSelectionRange(caret, caret);
  return true;
}

export function handleNickKey(
  ev: KeyboardEvent,
  inputEl: HTMLInputElement,
  popEl: HTMLElement,
  buffer: Buffer | undefined,
): boolean {
  if (popEl.hidden || stateNick.matches.length === 0) return false;

  if (ev.key === "Tab" || ev.key === "Enter") {
    const selectedNick = stateNick.matches[stateNick.selected];
    if (!updateNickPop(inputEl, popEl, buffer)) return false;
    const selectedIndex = selectedNick ? stateNick.matches.indexOf(selectedNick) : -1;
    if (selectedIndex >= 0) {
      stateNick.selected = selectedIndex;
      highlightRow(popEl, stateNick.selected);
    }
  }

  if (ev.key === "ArrowDown") {
    ev.preventDefault();
    stateNick.selected = (stateNick.selected + 1) % stateNick.matches.length;
    highlightRow(popEl, stateNick.selected);
    return true;
  }

  if (ev.key === "ArrowUp") {
    ev.preventDefault();
    stateNick.selected = (stateNick.selected - 1 + stateNick.matches.length) % stateNick.matches.length;
    highlightRow(popEl, stateNick.selected);
    return true;
  }

  if (ev.key === "Tab" || ev.key === "Enter") {
    const nick = stateNick.matches[stateNick.selected];
    if (nick) {
      ev.preventDefault();
      replaceNickToken(inputEl, nick);
      popEl.hidden = true;
      stateNick = emptyState();
      return true;
    }
  }

  if (ev.key === "Escape") {
    ev.preventDefault();
    popEl.hidden = true;
    stateNick = emptyState();
    return true;
  }

  return false;
}

function highlightRow(popEl: HTMLElement, index: number) {
  for (const row of popEl.querySelectorAll(".ni")) {
    row.classList.remove("hl");
  }
  popEl.querySelectorAll(".ni")[index]?.classList.add("hl");
}

export function initNickPop(inputEl: HTMLInputElement, popEl: HTMLElement, getActiveBuffer: () => Buffer | undefined) {
  const refreshIfOpen = () => {
    if (!popEl.hidden) updateNickPop(inputEl, popEl, getActiveBuffer());
  };
  inputEl.addEventListener("click", refreshIfOpen);
  inputEl.addEventListener("keyup", (ev) => {
    if (["ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown"].includes(ev.key)) refreshIfOpen();
  });
  inputEl.addEventListener("blur", () =>
    setTimeout(() => {
      popEl.hidden = true;
      stateNick = emptyState();
    }, 100),
  );
}
