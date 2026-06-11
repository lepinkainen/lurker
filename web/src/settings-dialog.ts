import { openDialog } from "./dialog";
import { getHighlights, putHighlights } from "./highlights-api";
import { jsonFetch, sendJSON } from "./http";

function buildHighlightsSection(dialog: HTMLDialogElement): HTMLElement[] {
  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Highlight words";

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
  dialog.addEventListener("close", () => document.removeEventListener("lurker:highlights", onRemoteChange));

  return [sectionTitle, desc, listEl, form, errEl];
}

export function openSettingsDialog(): void {
  const { dialog, close } = openDialog({ className: "sd-dialog" });

  const inner = document.createElement("div");
  inner.className = "sd-inner";

  const title = document.createElement("h2");
  title.className = "sd-title";
  title.textContent = "Settings";

  const sectionTitle = document.createElement("h3");
  sectionTitle.className = "sd-section-title";
  sectionTitle.textContent = "Config file sync";

  const desc = document.createElement("p");
  desc.className = "sd-desc";
  desc.textContent =
    "Preview and save the current network configuration back to config.yaml. " +
    "Channels and extra servers defined manually in the file are preserved.";

  const previewBtn = document.createElement("button");
  previewBtn.type = "button";
  previewBtn.className = "sd-btn sd-btn-secondary";
  previewBtn.textContent = "Preview changes";

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

  diffArea.append(currentLabel, currentPre, proposedLabel, proposedPre);

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

  previewBtn.addEventListener("click", async () => {
    previewBtn.disabled = true;
    previewBtn.textContent = "Loading…";
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
      previewBtn.disabled = false;
      previewBtn.textContent = "Preview changes";
    }
  });

  saveBtn.addEventListener("click", async () => {
    saveBtn.disabled = true;
    saveBtn.textContent = "Saving…";
    errEl.hidden = true;
    msgEl.hidden = true;

    try {
      await sendJSON<unknown>("/api/config/yaml/save", "POST", { content: proposedContent });
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

  const closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "sd-btn sd-btn-ghost sd-close";
  closeBtn.textContent = "Close";
  closeBtn.addEventListener("click", close);

  inner.append(
    title,
    ...buildHighlightsSection(dialog),
    sectionTitle,
    desc,
    previewBtn,
    errEl,
    msgEl,
    diffArea,
    saveBtn,
    closeBtn,
  );
  dialog.appendChild(inner);

  dialog.showModal();
}
