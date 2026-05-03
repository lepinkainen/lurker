import emojiMap from "./emoji-map.json";

const EMOJI_NAMES = Object.keys(emojiMap) as (keyof typeof emojiMap)[];
const MAX_RESULTS = 12;
const KEYWORD_CHAR_RE = /^[-+\w]$/u;
const KEYWORD_RE = /^[-+\w]{2,}$/u;
const TOKEN_PREFIX_RE = /\s/u;

export type EmojiAutocompleteState = {
  /** Index of highlighted result, -1 when no popup or invalid. */
  selected: number;
  /** Start position of the :keyword token in the input value. */
  tokenStart: number;
  /** End position of the :keyword token in the input value. */
  tokenEnd: number;
  /** Matched emoji names for current query. */
  matches: string[];
};

let state: EmojiAutocompleteState = { selected: -1, tokenStart: -1, tokenEnd: -1, matches: [] };

function emptyState(): EmojiAutocompleteState {
  return { selected: -1, tokenStart: -1, tokenEnd: -1, matches: [] };
}

export function getEmojiAutocompleteState(): EmojiAutocompleteState {
  return state;
}

export function resetEmojiAutocomplete() {
  state = emptyState();
}

function matchEmoji(query: string): string[] {
  const lower = query.toLowerCase();
  const results: string[] = [];
  for (const name of EMOJI_NAMES) {
    if (results.length >= MAX_RESULTS) break;
    if (name.startsWith(lower)) {
      results.push(name);
    }
  }
  if (results.length < MAX_RESULTS) {
    for (const name of EMOJI_NAMES) {
      if (results.length >= MAX_RESULTS) break;
      if (!name.startsWith(lower) && name.includes(lower)) {
        results.push(name);
      }
    }
  }
  return results;
}

function tokenAtCursor(value: string, cursor: number): { token: string; start: number; end: number } | null {
  // Walk backwards from cursor to find a ':' preceded by whitespace or start-of-string.
  const before = value.slice(0, cursor);
  const colonIdx = before.lastIndexOf(":");
  if (colonIdx === -1) return null;
  if (colonIdx > 0 && !TOKEN_PREFIX_RE.test(value[colonIdx - 1] ?? "")) return null;

  // Cursor must still be inside the keyword. If whitespace/punctuation sits between
  // the colon and cursor, an old token should not keep the popup active.
  for (let i = colonIdx + 1; i < cursor; i++) {
    const ch = value[i];
    if (ch === undefined || !KEYWORD_CHAR_RE.test(ch)) return null;
  }

  const keyword = value.slice(colonIdx + 1, cursor);
  if (!KEYWORD_RE.test(keyword)) return null;

  // If the caret is inside a token, accept should replace the whole token.
  let tokenEnd = cursor;
  while (tokenEnd < value.length) {
    const ch = value[tokenEnd];
    if (ch === undefined || !KEYWORD_CHAR_RE.test(ch)) break;
    tokenEnd++;
  }

  return { token: keyword, start: colonIdx, end: tokenEnd };
}

export function updateEmojiPop(inputEl: HTMLInputElement, popEl: HTMLElement): boolean {
  const cursor = inputEl.selectionStart ?? inputEl.value.length;
  const tk = tokenAtCursor(inputEl.value, cursor);

  if (!tk) {
    popEl.hidden = true;
    state = emptyState();
    return false;
  }

  const matches = matchEmoji(tk.token);
  if (matches.length === 0) {
    popEl.hidden = true;
    state = emptyState();
    return false;
  }

  popEl.innerHTML = "";
  const frag = document.createDocumentFragment();
  matches.forEach((name, i) => {
    const row = document.createElement("div");
    row.className = `ei ${i === 0 ? "hl" : ""}`;
    row.innerHTML = `<span class="ec">${emojiMap[name as keyof typeof emojiMap]}</span><span class="en">:${name}:</span>`;
    frag.appendChild(row);
  });
  popEl.appendChild(frag);
  popEl.hidden = false;

  state = { selected: 0, tokenStart: tk.start, tokenEnd: tk.end, matches };
  return true;
}

export function replaceEmojiToken(inputEl: HTMLInputElement, name: string): boolean {
  const before = inputEl.value.slice(0, state.tokenStart);
  const after = inputEl.value.slice(state.tokenEnd);
  const emoji = emojiMap[name as keyof typeof emojiMap];
  if (!emoji) return false;

  inputEl.value = before + emoji + after;
  const caret = before.length + emoji.length;
  inputEl.setSelectionRange(caret, caret);
  return true;
}

export function handleEmojiKey(ev: KeyboardEvent, inputEl: HTMLInputElement, popEl: HTMLElement): boolean {
  if (popEl.hidden || state.matches.length === 0) return false;

  if (ev.key === "Enter" || ev.key === "Tab") {
    const selectedName = state.matches[state.selected];
    if (!updateEmojiPop(inputEl, popEl)) return false;
    const selectedIndex = selectedName ? state.matches.indexOf(selectedName) : -1;
    if (selectedIndex >= 0) {
      state.selected = selectedIndex;
      highlightRow(popEl, state.selected);
    }
  }

  if (ev.key === "ArrowDown") {
    ev.preventDefault();
    state.selected = (state.selected + 1) % state.matches.length;
    highlightRow(popEl, state.selected);
    return true;
  }

  if (ev.key === "ArrowUp") {
    ev.preventDefault();
    state.selected = (state.selected - 1 + state.matches.length) % state.matches.length;
    highlightRow(popEl, state.selected);
    return true;
  }

  if (ev.key === "Enter" || ev.key === "Tab") {
    const name = state.matches[state.selected];
    if (name) {
      ev.preventDefault();
      replaceEmojiToken(inputEl, name);
      popEl.hidden = true;
      state = emptyState();
      return true;
    }
  }

  if (ev.key === "Escape") {
    ev.preventDefault();
    popEl.hidden = true;
    state = emptyState();
    return true;
  }

  return false;
}

function highlightRow(popEl: HTMLElement, index: number) {
  for (const row of popEl.querySelectorAll(".ei")) {
    row.classList.remove("hl");
  }
  popEl.querySelectorAll(".ei")[index]?.classList.add("hl");
}

export function initEmojiPop(inputEl: HTMLInputElement, popEl: HTMLElement) {
  const refreshIfOpen = () => {
    if (!popEl.hidden) updateEmojiPop(inputEl, popEl);
  };
  inputEl.addEventListener("click", refreshIfOpen);
  inputEl.addEventListener("keyup", (ev) => {
    if (["ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown"].includes(ev.key)) refreshIfOpen();
  });
  inputEl.addEventListener("blur", () =>
    setTimeout(() => {
      popEl.hidden = true;
      state = emptyState();
    }, 100),
  );
}
