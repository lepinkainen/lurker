export function openSettingsDialog(): void {
  const dialog = document.createElement("dialog");
  dialog.className = "sd-dialog";

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
      const res = await fetch("/api/config/yaml/preview");
      if (!res.ok) throw new Error((await res.text()) || res.statusText);
      const data = (await res.json()) as { current: string; proposed: string };
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
      const res = await fetch("/api/config/yaml/save", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ content: proposedContent }),
      });
      if (!res.ok) throw new Error((await res.text()) || res.statusText);
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
  closeBtn.addEventListener("click", () => dialog.close());

  inner.append(title, sectionTitle, desc, previewBtn, errEl, msgEl, diffArea, saveBtn, closeBtn);
  dialog.appendChild(inner);
  document.body.appendChild(dialog);

  dialog.addEventListener("close", () => dialog.remove());
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });

  dialog.showModal();
}
