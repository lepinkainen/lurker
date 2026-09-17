import { getHighlights, putHighlights } from "./highlights-api";
import { jsonFetch, sendJSON } from "./http";
import { buildMediaBrowser } from "./media-browser";
import { applyTheme, loadInitialTheme, persistThemeId, storedThemeId, type Theme } from "./theme";
import { closeAllDrawers } from "./ui-shell";

interface ServerIdentity {
  name: string;
  version: string;
  hash: string;
  build_time: string;
}

export function formatBuildTime(iso: string, now = Date.now()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const minutes = Math.round((date.getTime() - now) / 60_000);
  let relative: string;
  if (Math.abs(minutes) < 60) relative = rtf.format(minutes, "minute");
  else if (Math.abs(minutes) < 60 * 24) relative = rtf.format(Math.round(minutes / 60), "hour");
  else relative = rtf.format(Math.round(minutes / (60 * 24)), "day");
  return `${iso} (${relative})`;
}

function buildServerInfoSection(): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Server";

  const list = document.createElement("dl");
  list.className = "sd-info";

  function addRow(label: string, value: string) {
    const dt = document.createElement("dt");
    dt.textContent = label;
    const dd = document.createElement("dd");
    dd.textContent = value;
    list.append(dt, dd);
  }

  jsonFetch<ServerIdentity>("/whoami")
    .then((info) => {
      addRow("Version", `${info.version} (${info.hash})`);
      addRow("Built", formatBuildTime(info.build_time));
    })
    .catch(() => {
      addRow("Version", "unavailable");
    });

  return [sectionTitle, list];
}

function buildHighlightsSection(handle: SettingsViewHandle): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Notifications";

  const desc = document.createElement("p");
  desc.className = "sd-desc";
  desc.textContent =
    "Messages containing these words are highlighted like nick mentions. " +
    "Matching is case-insensitive on whole words.";

  const errEl = document.createElement("p");
  errEl.className = "sd-error";
  errEl.hidden = true;

  const listEl = document.createElement("ul");
  listEl.className = "sd-highlight-list";

  const form = document.createElement("form");
  form.className = "sd-highlight-form";

  const input = document.createElement("input");
  input.type = "text";
  input.className = "sd-input";
  input.placeholder = "Add word…";
  input.maxLength = 64;

  const addBtn = document.createElement("button");
  addBtn.type = "submit";
  addBtn.className = "sd-btn sd-btn-secondary";
  addBtn.textContent = "Add";

  form.append(input, addBtn);

  let patterns: string[] = [];

  function render() {
    listEl.innerHTML = "";
    for (const pattern of patterns) {
      const li = document.createElement("li");
      li.className = "sd-highlight-item";
      const label = document.createElement("span");
      label.textContent = pattern;
      const removeBtn = document.createElement("button");
      removeBtn.type = "button";
      removeBtn.className = "sd-btn sd-btn-ghost sd-highlight-remove";
      removeBtn.textContent = "✕";
      removeBtn.title = `Remove "${pattern}"`;
      removeBtn.addEventListener("click", () => {
        save(patterns.filter((p) => p !== pattern));
      });
      li.append(label, removeBtn);
      listEl.appendChild(li);
    }
  }

  function save(next: string[]): void {
    errEl.hidden = true;
    putHighlights(next)
      .then((p) => {
        patterns = p;
        render();
      })
      .catch((err: unknown) => {
        errEl.textContent = err instanceof Error ? err.message : "Save failed";
        errEl.hidden = false;
      });
  }

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    const word = input.value.trim();
    if (!word) return;
    input.value = "";
    save([...patterns, word]);
  });

  getHighlights()
    .then((p) => {
      patterns = p;
      render();
    })
    .catch((err: unknown) => {
      errEl.textContent = err instanceof Error ? err.message : "Failed to load highlight words";
      errEl.hidden = false;
    });

  // Another client (or tab) changed the list: refresh from the WS event.
  const onRemoteChange = (e: Event) => {
    patterns = (e as CustomEvent<string[]>).detail;
    render();
  };
  document.addEventListener("lurker:highlights", onRemoteChange);
  handle.onClose(() => document.removeEventListener("lurker:highlights", onRemoteChange));

  return [sectionTitle, desc, listEl, form, errEl];
}

function buildMediaLibrarySection(): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Media library";

  const desc = document.createElement("p");
  desc.className = "sd-desc";
  desc.textContent = "Browse and delete images uploaded through this bouncer.";

  return [sectionTitle, desc, buildMediaBrowser()];
}

function buildConfigSyncSection(_handle: SettingsViewHandle): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Config file sync";

  const desc = document.createElement("p");
  desc.className = "sd-desc";
  desc.textContent =
    "Preview and save the current network configuration back to config.yaml. " +
    "Channels and extra servers defined manually in the file are preserved.";

  const loadingEl = document.createElement("p");
  loadingEl.className = "sd-desc";
  loadingEl.textContent = "Loading…";

  const errEl = document.createElement("p");
  errEl.className = "sd-error";
  errEl.hidden = true;

  const msgEl = document.createElement("p");
  msgEl.className = "sd-msg";
  msgEl.hidden = true;

  const diffArea = document.createElement("div");
  diffArea.className = "sd-diff";
  diffArea.hidden = true;

  const currentLabel = document.createElement("div");
  currentLabel.className = "sd-diff-label";
  currentLabel.textContent = "Current config.yaml";

  const proposedLabel = document.createElement("div");
  proposedLabel.className = "sd-diff-label";
  proposedLabel.textContent = "Proposed config.yaml";

  const currentPre = document.createElement("pre");
  currentPre.className = "sd-pre";

  const proposedPre = document.createElement("pre");
  proposedPre.className = "sd-pre";

  const currentCol = document.createElement("div");
  currentCol.className = "sd-diff-col";
  currentCol.append(currentLabel, currentPre);

  const proposedCol = document.createElement("div");
  proposedCol.className = "sd-diff-col";
  proposedCol.append(proposedLabel, proposedPre);

  diffArea.append(currentCol, proposedCol);

  const saveBtn = document.createElement("button");
  saveBtn.type = "button";
  saveBtn.className = "sd-btn sd-btn-primary";
  saveBtn.textContent = "Save to config.yaml";
  saveBtn.hidden = true;

  let proposedContent = "";

  function renderDiff(current: string, proposed: string) {
    const currentLines = current.split("\n");
    const proposedLines = proposed.split("\n");
    const proposedSet = new Set(proposedLines);
    const currentSet = new Set(currentLines);

    currentPre.innerHTML = "";
    for (const line of currentLines) {
      const span = document.createElement("span");
      span.className = proposedSet.has(line) ? "sd-line" : "sd-line sd-line-remove";
      span.textContent = `${line}\n`;
      currentPre.appendChild(span);
    }

    proposedPre.innerHTML = "";
    for (const line of proposedLines) {
      const span = document.createElement("span");
      span.className = currentSet.has(line) ? "sd-line" : "sd-line sd-line-add";
      span.textContent = `${line}\n`;
      proposedPre.appendChild(span);
    }
  }

  async function refresh(): Promise<void> {
    loadingEl.hidden = false;
    errEl.hidden = true;
    msgEl.hidden = true;
    saveBtn.hidden = true;
    diffArea.hidden = true;

    try {
      const data = await jsonFetch<{ current: string; proposed: string }>("/api/config/yaml/preview");
      proposedContent = data.proposed;
      if (data.current === data.proposed) {
        msgEl.textContent = "No changes — config.yaml already matches current network state.";
        msgEl.hidden = false;
      } else {
        renderDiff(data.current, data.proposed);
        diffArea.hidden = false;
        saveBtn.hidden = false;
      }
    } catch (err) {
      errEl.textContent = err instanceof Error ? err.message : "Request failed";
      errEl.hidden = false;
    } finally {
      loadingEl.hidden = true;
    }
  }

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    saveBtn.textContent = "Saving…";
    errEl.hidden = true;
    msgEl.hidden = true;

    try {
      await sendJSON<unknown>("/api/config/yaml/save", "POST", { content: proposedContent });
      try {
        await refresh();
      } catch (refreshErr) {
        // Save succeeded; only the post-save preview refresh failed. Show
        // both — the confirmation is still true, but the diff/preview may
        // now be stale.
        errEl.textContent =
          "Saved, but failed to refresh preview: " +
          (refreshErr instanceof Error ? refreshErr.message : "request failed");
        errEl.hidden = false;
      }
      // Set after refresh() resolves: refresh() hides msgEl at its start and
      // may repopulate it with a "no changes" message — this overwrite is
      // the stronger, more relevant signal right after a save.
      msgEl.textContent = "Saved to config.yaml.";
      msgEl.hidden = false;
    } catch (err) {
      errEl.textContent = err instanceof Error ? err.message : "Save failed";
      errEl.hidden = false;
    } finally {
      saveBtn.disabled = false;
      saveBtn.textContent = "Save to config.yaml";
    }
  });

  refresh().catch((err: unknown) => console.error("config preview", err));

  return [sectionTitle, desc, loadingEl, errEl, msgEl, diffArea, saveBtn];
}

// Representative color keys used to render a small preview swatch for each
// theme chip. Picked to give a quick "feel" of the theme: a background, the
// accent, and a foreground tone. Missing keys are skipped gracefully.
const THEME_PREVIEW_KEYS = ["bg-2", "accent", "fg-0"];

function buildAppearanceSection(_handle: SettingsViewHandle): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Appearance";

  const desc = document.createElement("p");
  desc.className = "sd-desc";
  desc.textContent = "Pick a color theme.";

  const grid = document.createElement("div");
  grid.className = "sd-theme-grid";

  function renderChips(themes: Theme[], activeId: string | null) {
    grid.innerHTML = "";
    if (themes.length === 0) {
      const note = document.createElement("p");
      note.className = "sd-desc";
      note.textContent = "No themes available.";
      grid.appendChild(note);
      return;
    }
    for (const theme of themes) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "sd-theme-chip";
      if (theme.id === activeId) chip.classList.add("active");

      const swatches = document.createElement("span");
      swatches.className = "sd-theme-swatches";
      for (const key of THEME_PREVIEW_KEYS) {
        const color = theme.colors?.[key];
        if (!color) continue;
        const block = document.createElement("span");
        block.className = "sd-theme-swatch";
        block.style.background = color;
        swatches.appendChild(block);
      }

      const label = document.createElement("span");
      label.className = "sd-theme-name";
      label.textContent = theme.name;

      chip.append(swatches, label);
      chip.addEventListener("click", () => {
        applyTheme(theme);
        persistThemeId(theme.id);
        for (const el of grid.querySelectorAll(".sd-theme-chip")) el.classList.remove("active");
        chip.classList.add("active");
      });
      grid.appendChild(chip);
    }
  }

  loadInitialTheme()
    .then(({ themes, active }) => {
      renderChips(themes, active?.id ?? storedThemeId());
    })
    .catch(() => {
      renderChips([], null);
    });

  return [sectionTitle, desc, grid];
}

interface SettingsCategory {
  id: string;
  label: string;
  build: (handle: SettingsViewHandle) => HTMLElement[];
}

const SETTINGS_CATEGORIES: SettingsCategory[] = [
  {
    id: "general",
    label: "General",
    build: (handle) => [...buildHighlightsSection(handle), ...buildServerInfoSection()],
  },
  {
    id: "appearance",
    label: "Appearance",
    build: buildAppearanceSection,
  },
  {
    id: "media",
    label: "Media library",
    build: buildMediaLibrarySection,
  },
  {
    id: "config",
    label: "Config file sync",
    build: buildConfigSyncSection,
  },
];

export type SettingsViewHandle = {
  root: HTMLElement;
  onClose(cb: () => void): void;
};

type ActiveSettingsView = {
  root: HTMLElement;
  teardown: () => void;
};

let activeView: ActiveSettingsView | null = null;

/**
 * Opens the in-pane settings view inside #main, covering the buffer view
 * (sidebar stays interactive). Calling this again while the view is open
 * closes it instead — the gear button toggles.
 */
export function openSettingsView(): void {
  if (activeView) {
    closeSettingsView();
    return;
  }

  const mainEl = document.getElementById("main");
  if (!mainEl) return;

  // Hide the member pane while settings are open. Inline style (rather than
  // a class) is used because it needs to beat the unlayered mobile.css
  // `.memberpane { display: flex }` rules; the previous inline value is
  // saved and restored on teardown so we don't clobber other state.
  const memberPane = document.getElementById("member-pane");
  const prevMemberPaneDisplay = memberPane?.style.display ?? "";
  if (memberPane) memberPane.style.display = "none";

  // Settings is a full-pane view, not a drawer: collapse any open mobile
  // sidebar/members drawer so it isn't buried under their fixed z-50 layer.
  closeAllDrawers();

  const root = document.createElement("div");
  root.className = "settings-view";
  root.tabIndex = -1;
  root.setAttribute("role", "dialog");
  root.setAttribute("aria-label", "Settings");

  // Modeless: sidebar stays interactive intentionally, so no aria-modal and
  // no focus trap. We just move focus onto the view on open and restore it
  // to whatever had focus before on close.
  const previouslyFocused = document.activeElement;

  const closeCallbacks: Array<() => void> = [];
  const handle: SettingsViewHandle = {
    root,
    onClose(cb) {
      closeCallbacks.push(cb);
    },
  };

  const header = document.createElement("div");
  header.className = "sd-view-header";

  const title = document.createElement("h2");
  title.className = "sd-title";
  title.textContent = "Settings";

  const closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "sd-btn sd-btn-ghost sd-close";
  closeBtn.textContent = "Close";
  closeBtn.addEventListener("click", closeSettingsView);

  header.append(title, closeBtn);

  const body = document.createElement("div");
  body.className = "sd-body";

  const nav = document.createElement("nav");
  nav.className = "sd-nav";

  const content = document.createElement("div");
  content.className = "sd-content";

  // Built panels are cached per category and reused across nav clicks within
  // this view instance: rebuilding on every click was re-registering
  // document listeners (highlights), re-fetching (media list, themes,
  // config preview) and re-running side effects (applyTheme) each time a
  // category was revisited. closeCallbacks still flush once, at view close.
  const panels = new Map<string, HTMLElement[]>();

  function selectCategory(category: SettingsCategory) {
    for (const btn of nav.querySelectorAll(".sd-nav-item")) {
      const isActive = btn.getAttribute("data-category-id") === category.id;
      btn.classList.toggle("active", isActive);
      if (isActive) btn.setAttribute("aria-current", "true");
      else btn.removeAttribute("aria-current");
    }
    let built = panels.get(category.id);
    if (!built) {
      built = category.build(handle);
      panels.set(category.id, built);
    }
    content.replaceChildren(...built);
  }

  for (const category of SETTINGS_CATEGORIES) {
    const navBtn = document.createElement("button");
    navBtn.type = "button";
    navBtn.className = "sd-nav-item";
    navBtn.textContent = category.label;
    navBtn.setAttribute("data-category-id", category.id);
    navBtn.addEventListener("click", () => selectCategory(category));
    nav.appendChild(navBtn);
  }

  const firstCategory = SETTINGS_CATEGORIES[0];
  if (firstCategory) selectCategory(firstCategory);

  body.append(nav, content);
  root.append(header, body);
  mainEl.appendChild(root);

  // Make the buffer view underneath (topicbar/messages/composer) inert while
  // settings are open: removes it from the tab order and a11y tree. This is
  // modeless (sidebar/member pane are separate elements and untouched), so
  // inert only applies to whatever sibling content #main already had.
  const covered = [...mainEl.children].filter((el) => el !== root && !el.hasAttribute("inert"));
  for (const el of covered) el.setAttribute("inert", "");

  root.focus();

  // Capture-phase Esc handler: closes the view and stops the key from ever
  // reaching keyboard-routing's bubble-phase document listener (which would
  // otherwise treat a bare Esc as a read-ack for the active buffer). A real
  // <dialog> (e.g. openNetworkForm, or the buffer options dialog) stacked on
  // top of the modeless settings view must keep its native Esc-to-close
  // behavior, so this handler backs off whenever one is open.
  const onKeydown = (e: KeyboardEvent) => {
    if (e.key !== "Escape") return;
    if (document.querySelector("dialog[open]")) return;
    e.preventDefault();
    e.stopPropagation();
    closeSettingsView();
  };
  document.addEventListener("keydown", onKeydown, true);

  activeView = {
    root,
    teardown: () => {
      document.removeEventListener("keydown", onKeydown, true);
      for (const cb of closeCallbacks) cb();
      if (memberPane) memberPane.style.display = prevMemberPaneDisplay;
      for (const el of covered) el.removeAttribute("inert");
      root.remove();
      if (previouslyFocused instanceof HTMLElement && previouslyFocused.isConnected) {
        previouslyFocused.focus();
      }
    },
  };
}

function closeSettingsView(): void {
  if (!activeView) return;
  const view = activeView;
  activeView = null;
  view.teardown();
}
