// History search overlay: FTS over stored messages via GET /api/search, with
// a per-hit context window from GET /api/buffers/{id}/history?around=.
// Spec: ai-docs/behaviors/history-search.md.
import { type Message, state } from "./app-state";
import { formatTime, nickColor } from "./format";
import { jsonFetch } from "./http";
import { buildDialog, canOpenModal, detachedRef } from "./keyboard-dialogs";
import { nickColorIndex } from "./nick-colors";

const DEBOUNCE_MS = 250;
const PAGE_SIZE = 50;
const CONTEXT_SIZE = 30;
const REGEX_META = /[.*+?^${}()|[\]\\]/gu;
const WHITESPACE = /\s+/u;

const logErr = (err: unknown) => console.error("history search", err);

type Scope = "buffer" | "all";

type SearchResponse = { results: Message[] };
type HistoryResponse = { messages: Message[] };

let searchDialog: HTMLDialogElement | null = null;

export function isHistorySearchOpen() {
  searchDialog = detachedRef(searchDialog);
  return searchDialog !== null;
}

export function closeHistorySearch() {
  if (!searchDialog) return false;
  searchDialog.close();
  return true;
}

// Split text into plain and matched runs for the query's whitespace tokens,
// case-insensitive. Exported for tests. The server matches on FTS tokens
// (prefix on the last one), so the client marks substrings — close enough,
// and never hides a hit.
export type HighlightRun = { text: string; mark: boolean };

export function highlightRuns(text: string, query: string): HighlightRun[] {
  const tokens = query
    .split(WHITESPACE)
    .filter(Boolean)
    .sort((a, b) => b.length - a.length);
  if (tokens.length === 0 || text === "") return [{ text, mark: false }];
  const re = new RegExp(tokens.map((t) => t.replace(REGEX_META, "\\$&")).join("|"), "giu");
  const runs: HighlightRun[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    const start = m.index ?? 0;
    if (start > last) runs.push({ text: text.slice(last, start), mark: false });
    runs.push({ text: m[0], mark: true });
    last = start + m[0].length;
  }
  if (last < text.length) runs.push({ text: text.slice(last), mark: false });
  return runs;
}

// "14:41" today, "Mon 14:41" within the last week, "28 Sep" older.
export function hitTimeLabel(iso: string | undefined, now = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return formatTime(iso);
  const ageDays = (now.getTime() - d.getTime()) / 86_400_000;
  if (ageDays < 7) return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${formatTime(iso)}`;
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

function highlightedContent(text: string, query: string) {
  const span = document.createElement("span");
  span.className = "hs-content";
  for (const run of highlightRuns(text, query)) {
    if (run.mark) {
      const mark = document.createElement("mark");
      mark.textContent = run.text;
      span.appendChild(mark);
    } else {
      span.appendChild(document.createTextNode(run.text));
    }
  }
  return span;
}

function nickSpan(nick: string, className = "hs-nick") {
  const el = document.createElement("span");
  el.className = className;
  el.textContent = nick;
  el.style.color = nickColor(nickColorIndex(nick));
  return el;
}

function bufferLabel(bufferId: string) {
  const buffer = state.buffers.get(bufferId);
  if (!buffer) return "";
  const network = state.networks.get(buffer.network_id);
  return network ? `${network.name} · ${buffer.name}` : buffer.name;
}

function contextRow(message: Message, hitId: string, query: string) {
  const row = document.createElement("div");
  row.className = `hs-ctx-row${message.id === hitId ? " hit" : ""}`;
  const ts = document.createElement("span");
  ts.className = "hs-ctx-ts";
  ts.textContent = formatTime(message.ts);
  const nick = nickSpan(message.sender || "", "hs-ctx-nick");
  const body = message.id === hitId ? highlightedContent(message.content || "", query) : document.createElement("span");
  if (message.id !== hitId) {
    body.className = "hs-content";
    body.textContent =
      message.kind === "action" ? `* ${message.sender || ""} ${message.content || ""}` : message.content || "";
  }
  row.append(ts, nick, body);
  return row;
}

export function openHistorySearch(initialQuery = "") {
  searchDialog = detachedRef(searchDialog);
  if (searchDialog) {
    const input = searchDialog.querySelector<HTMLInputElement>(".hs-input");
    if (input && initialQuery) {
      input.value = initialQuery;
      input.dispatchEvent(new Event("input"));
    }
    input?.focus();
    input?.select();
    return;
  }
  if (!canOpenModal()) return;

  const activeBuffer = state.activeId ? state.buffers.get(state.activeId) : undefined;
  const scopedBuffer = activeBuffer && activeBuffer.kind !== "status" ? activeBuffer : undefined;

  const dialog = buildDialog("ks-dialog hs-dialog", "history-search");
  searchDialog = dialog;

  const card = document.createElement("div");
  card.className = "hs-card";

  // Input row --------------------------------------------------------------
  const inputRow = document.createElement("div");
  inputRow.className = "hs-input-row";
  const input = document.createElement("input");
  input.type = "text";
  input.className = "hs-input";
  input.placeholder = "Search messages";
  input.setAttribute("aria-label", "Search messages");
  input.autocomplete = "off";
  input.value = initialQuery;

  const scopeGroup = document.createElement("div");
  scopeGroup.className = "hs-scope";
  scopeGroup.setAttribute("role", "group");
  scopeGroup.setAttribute("aria-label", "Search scope");
  const scopeBuffer = document.createElement("button");
  scopeBuffer.type = "button";
  scopeBuffer.className = "hs-scope-btn";
  scopeBuffer.textContent = scopedBuffer?.name ?? "This channel";
  scopeBuffer.disabled = scopedBuffer === undefined;
  const scopeAll = document.createElement("button");
  scopeAll.type = "button";
  scopeAll.className = "hs-scope-btn";
  scopeAll.textContent = "All channels";
  scopeGroup.append(scopeBuffer, scopeAll);
  inputRow.append(input, scopeGroup);

  // Meta row ---------------------------------------------------------------
  const meta = document.createElement("div");
  meta.className = "hs-meta";
  const metaText = document.createElement("span");
  const metaKeys = document.createElement("span");
  metaKeys.className = "hs-keys";
  metaKeys.textContent = "↑↓ move · ↵ context · esc close";
  meta.append(metaText, metaKeys);

  // Results ----------------------------------------------------------------
  const list = document.createElement("div");
  list.className = "hs-results";
  list.setAttribute("role", "listbox");
  list.setAttribute("aria-label", "Search results");

  card.append(inputRow, meta, list);
  dialog.appendChild(card);
  document.body.appendChild(dialog);

  // State ------------------------------------------------------------------
  let scope: Scope = scopedBuffer ? "buffer" : "all";
  let results: Message[] = [];
  let selected = 0;
  let expandedId: string | null = null;
  let exhausted = false;
  let loadingMore = false;
  let controller: AbortController | null = null;
  let debounce: ReturnType<typeof setTimeout> | null = null;

  const rows = new Map<string, HTMLElement>();

  function searchURL(before?: string) {
    const params = new URLSearchParams({ q: input.value.trim(), limit: String(PAGE_SIZE) });
    if (scope === "buffer" && scopedBuffer) params.set("buffer", scopedBuffer.id);
    if (before) params.set("before", before);
    return `/api/search?${params.toString()}`;
  }

  function renderScope() {
    scopeBuffer.setAttribute("aria-pressed", scope === "buffer" ? "true" : "false");
    scopeAll.setAttribute("aria-pressed", scope === "all" ? "true" : "false");
  }

  function renderMeta(status?: string) {
    if (status !== undefined) {
      metaText.textContent = status;
      return;
    }
    const where = scope === "buffer" && scopedBuffer ? scopedBuffer.name : "all channels";
    metaText.textContent =
      results.length === 0 ? "" : `${results.length}${exhausted ? "" : "+"} matches in ${where} · newest first`;
  }

  function applySelection(index: number) {
    const prev = results[selected];
    if (prev) rows.get(prev.id)?.classList.remove("selected");
    rows.get(prev?.id ?? "")?.setAttribute("aria-selected", "false");
    selected = index;
    const curr = results[selected];
    const row = curr ? rows.get(curr.id) : undefined;
    if (row) {
      row.classList.add("selected");
      row.setAttribute("aria-selected", "true");
      row.scrollIntoView({ block: "nearest" });
    }
  }

  function buildRow(message: Message, index: number) {
    const row = document.createElement("div");
    row.className = `hs-result${index === selected ? " selected" : ""}`;
    row.setAttribute("role", "option");
    row.setAttribute("aria-selected", index === selected ? "true" : "false");
    row.dataset.id = message.id;

    const head = document.createElement("button");
    head.type = "button";
    head.className = "hs-result-head";
    const ts = document.createElement("span");
    ts.className = "hs-ts";
    ts.textContent = hitTimeLabel(message.ts);
    const nick = nickSpan(message.sender || "");
    const content = highlightedContent(
      message.kind === "action" ? `* ${message.content || ""}` : message.content || "",
      input.value,
    );
    head.append(ts, nick, content);
    if (scope === "all") {
      const where = document.createElement("span");
      where.className = "hs-where";
      where.textContent = bufferLabel(message.buffer_id);
      head.appendChild(where);
    }
    head.addEventListener("mousemove", () => {
      if (selected !== index) applySelection(index);
    });
    head.addEventListener("click", () => {
      applySelection(index);
      toggleExpand(message).catch(logErr);
    });
    row.appendChild(head);
    rows.set(message.id, row);
    return row;
  }

  function renderResults(empty: string | null) {
    rows.clear();
    if (results.length === 0) {
      const emptyEl = document.createElement("div");
      emptyEl.className = "ks-empty";
      emptyEl.textContent = empty ?? "";
      list.replaceChildren(emptyEl);
      selected = -1;
      renderMeta();
      return;
    }
    if (selected < 0 || selected >= results.length) selected = 0;
    list.replaceChildren(...results.map((m, i) => buildRow(m, i)));
    if (!exhausted) {
      const more = document.createElement("div");
      more.className = "hs-more";
      more.textContent = "scroll for older matches";
      list.appendChild(more);
    }
    renderMeta();
  }

  async function toggleExpand(message: Message) {
    const row = rows.get(message.id);
    if (!row) return;
    if (expandedId === message.id) {
      row.querySelector(".hs-ctx")?.remove();
      row.classList.remove("expanded");
      expandedId = null;
      return;
    }
    if (expandedId) {
      const prevRow = rows.get(expandedId);
      prevRow?.querySelector(".hs-ctx")?.remove();
      prevRow?.classList.remove("expanded");
    }
    expandedId = message.id;
    row.classList.add("expanded");
    const panel = document.createElement("div");
    panel.className = "hs-ctx";
    const header = document.createElement("div");
    header.className = "hs-ctx-head";
    const d = message.ts ? new Date(message.ts) : null;
    const day =
      d && !Number.isNaN(d.getTime())
        ? d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" })
        : "";
    header.textContent = `${day} · ${bufferLabel(message.buffer_id)} · ±${CONTEXT_SIZE / 2} messages`;
    const body = document.createElement("div");
    body.className = "hs-ctx-body";
    body.textContent = "Loading…";
    panel.append(header, body);
    row.appendChild(panel);
    try {
      const params = new URLSearchParams({ around: message.id, limit: String(CONTEXT_SIZE) });
      const res = await jsonFetch<HistoryResponse>(`/api/buffers/${message.buffer_id}/history?${params.toString()}`);
      if (expandedId !== message.id) return;
      body.replaceChildren(...res.messages.map((m) => contextRow(m, message.id, input.value)));
      body.querySelector(".hit")?.scrollIntoView({ block: "center" });
    } catch (err) {
      body.textContent = `Could not load context: ${err instanceof Error ? err.message : String(err)}`;
    }
  }

  async function runSearch() {
    controller?.abort();
    const q = input.value.trim();
    expandedId = null;
    if (q === "") {
      results = [];
      exhausted = true;
      renderResults(null);
      return;
    }
    controller = new AbortController();
    const { signal } = controller;
    renderMeta("Searching…");
    try {
      const res = await jsonFetch<SearchResponse>(searchURL(), { signal });
      if (signal.aborted) return;
      results = res.results;
      exhausted = results.length < PAGE_SIZE;
      selected = 0;
      renderResults("No matches");
    } catch (err) {
      if (signal.aborted) return;
      results = [];
      exhausted = true;
      renderResults(`Search failed: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  async function loadMore() {
    if (loadingMore || exhausted || results.length === 0) return;
    loadingMore = true;
    const last = results[results.length - 1];
    try {
      const res = await jsonFetch<SearchResponse>(searchURL(last?.id));
      if (res.results.length < PAGE_SIZE) exhausted = true;
      results = results.concat(res.results);
      const keepExpanded = expandedId;
      const scrollTop = list.scrollTop;
      renderResults("No matches");
      list.scrollTop = scrollTop;
      const reopen = keepExpanded ? results.find((m) => m.id === keepExpanded) : undefined;
      expandedId = null;
      if (reopen) toggleExpand(reopen).catch(logErr);
    } finally {
      loadingMore = false;
    }
  }

  function scheduleSearch() {
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => runSearch().catch(logErr), DEBOUNCE_MS);
  }

  function setScope(next: Scope) {
    if (scope === next) return;
    scope = next;
    renderScope();
    runSearch().catch(logErr);
  }

  // Events -----------------------------------------------------------------
  input.addEventListener("input", scheduleSearch);
  scopeBuffer.addEventListener("click", () => setScope("buffer"));
  scopeAll.addEventListener("click", () => setScope("all"));
  list.addEventListener("scroll", () => {
    if (list.scrollTop + list.clientHeight >= list.scrollHeight - 80) loadMore().catch(logErr);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (results.length === 0) return;
      const delta = e.key === "ArrowDown" ? 1 : -1;
      applySelection((selected + delta + results.length) % results.length);
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const picked = results[selected];
      if (picked) toggleExpand(picked).catch(logErr);
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (expandedId) {
        const open = results.find((m) => m.id === expandedId);
        if (open) toggleExpand(open).catch(logErr);
        return;
      }
      closeHistorySearch();
    }
  });

  dialog.addEventListener("close", () => {
    controller?.abort();
    if (debounce) clearTimeout(debounce);
    searchDialog = null;
    dialog.remove();
  });

  renderScope();
  renderResults(null);
  dialog.showModal();
  input.focus();
  input.select();
  if (initialQuery) runSearch().catch(logErr);
}
