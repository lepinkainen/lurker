import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@12/dist/mermaid.esm.min.mjs";

const $ = (elId) => document.getElementById(elId);
const els = {
  projectName: $("project-name"), generatedAt: $("generated-at"), commit: $("commit"),
  tabs: $("tabs"), banner: $("banner"), viewport: $("viewport"), canvas: $("canvas"), source: $("source"),
  btnTheme: $("btn-theme"), btnSource: $("btn-source"), btnCopy: $("btn-copy"), btnReset: $("btn-reset"),
};

const state = {
  manifest: null, currentId: null, currentText: "", showingSource: false,
  theme: "default", scale: 1, x: 0, y: 0,
};

const HTTP_HINT_CMD = "cd /path/to/this/directory && python3 -m http.server 8000";

function banner(html) {
  els.banner.innerHTML = html;
  els.banner.hidden = false;
}

function hideBanner() {
  els.banner.hidden = true;
  els.banner.innerHTML = "";
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function fileGuardMessage() {
  return `<div>Cannot load diagrams over <code>file://</code> — browsers block <code>fetch()</code> of local files. Serve this directory instead:</div><pre>${escapeHtml(HTTP_HINT_CMD)}</pre><div>then open <code>http://localhost:8000/</code>.</div>`;
}

function loadFailedMessage(what, detail) {
  return `<div>Could not load <code>${escapeHtml(what)}</code>${detail ? ` — ${escapeHtml(detail)}` : ""}.</div>` +
    (what === "diagrams.json"
      ? `<div>Run the <code>mermaid-architecture</code> skill in this repo to generate the diagrams and manifest.</div>`
      : `<div>The manifest references a file that is not in this directory.</div>`);
}

// ---------- theme ----------

function initTheme() {
  const stored = localStorage.getItem("mermaid-viewer-theme");
  const preferred = stored || (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  applyTheme(preferred, { persist: false, rerender: false });
}

function applyTheme(mode, { persist = true, rerender = true } = {}) {
  document.documentElement.setAttribute("data-theme", mode);
  state.theme = mode === "dark" ? "dark" : "default";
  els.btnTheme.classList.toggle("active", mode === "dark");
  if (persist) localStorage.setItem("mermaid-viewer-theme", mode);
  mermaid.initialize({
    startOnLoad: false,
    theme: state.theme,
    securityLevel: "loose",
    maxTextSize: 500000,
    flowchart: { useMaxWidth: false },
    class: { useMaxWidth: false },
    er: { useMaxWidth: false },
    sequence: { useMaxWidth: false },
  });
  if (rerender && state.currentId) selectDiagram(state.currentId, { updateHash: false });
}

els.btnTheme.addEventListener("click", () => {
  const next = document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark";
  applyTheme(next);
});

// ---------- manifest + tabs ----------

async function loadManifest() {
  if (location.protocol === "file:") {
    banner(fileGuardMessage());
    return null;
  }
  let res;
  try {
    res = await fetch("diagrams.json", { cache: "no-store" });
  } catch (err) {
    banner(loadFailedMessage("diagrams.json", err.message));
    return null;
  }
  if (!res.ok) {
    banner(loadFailedMessage("diagrams.json", `HTTP ${res.status}`));
    return null;
  }
  return res.json();
}

function buildTabs(manifest) {
  els.projectName.textContent = manifest.project || "(project)";
  els.generatedAt.textContent = manifest.generated_at || "";
  els.commit.textContent = manifest.commit || "";
  els.tabs.innerHTML = "";
  for (const d of manifest.diagrams) {
    const btn = document.createElement("button");
    btn.textContent = d.title || d.id;
    btn.dataset.id = d.id;
    btn.title = d.description || "";
    btn.addEventListener("click", () => selectDiagram(d.id));
    els.tabs.appendChild(btn);
  }
}

function setActiveTab(id) {
  for (const btn of els.tabs.children) {
    btn.classList.toggle("active", btn.dataset.id === id);
  }
}

// ---------- rendering ----------

let renderCounter = 0;

// Bumped on every selection. Both the fetch and the mermaid render are async,
// so a slow earlier selection can finish after a newer one; anything holding a
// stale generation must not touch shared state or the canvas.
let selectionGeneration = 0;

async function selectDiagram(id, { updateHash = true } = {}) {
  const entry = state.manifest?.diagrams.find((d) => d.id === id);
  if (!entry) return;
  const gen = ++selectionGeneration;
  state.currentId = id;
  setActiveTab(id);
  if (updateHash) history.replaceState(null, "", `#${id}`);

  let text;
  try {
    const res = await fetch(entry.file, { cache: "no-store" });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    text = await res.text();
  } catch (err) {
    if (gen !== selectionGeneration) return;
    banner(loadFailedMessage(entry.file, err.message));
    return;
  }
  if (gen !== selectionGeneration) return;
  state.currentText = text;
  els.source.textContent = text;
  hideBanner();

  if (state.showingSource) return;
  await renderCurrent(gen);
}

async function renderCurrent(gen = selectionGeneration) {
  const id = `mmd-${renderCounter++}`;
  try {
    const { svg } = await mermaid.render(id, state.currentText);
    if (gen !== selectionGeneration) return;
    els.canvas.innerHTML = svg;
    resetZoom();
    attachHighlight(els.canvas.querySelector("svg"));
  } catch (err) {
    if (gen !== selectionGeneration) return;
    els.canvas.innerHTML = "";
    banner(`<div>Mermaid failed to render <code>${state.currentId}</code>:</div><pre>${escapeHtml(err.message || String(err))}</pre>`);
  }
}

// ---------- pan / zoom ----------

function applyTransform() {
  els.canvas.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.scale})`;
}

function resetZoom() {
  state.scale = 1;
  state.x = 0;
  state.y = 0;
  applyTransform();
}

els.btnReset.addEventListener("click", resetZoom);

// wheel zooms around the cursor: keep the point under it fixed in canvas space
els.viewport.addEventListener("wheel", (e) => {
  e.preventDefault();
  const rect = els.viewport.getBoundingClientRect();
  const cx = e.clientX - rect.left, cy = e.clientY - rect.top;
  const next = Math.min(8, Math.max(0.1, state.scale * Math.exp(-e.deltaY * 0.001)));
  state.x = cx - ((cx - state.x) / state.scale) * next;
  state.y = cy - ((cy - state.y) / state.scale) * next;
  state.scale = next;
  applyTransform();
}, { passive: false });

let dragging = false;
let dragStart = { x: 0, y: 0, ox: 0, oy: 0 };

els.viewport.addEventListener("mousedown", (e) => {
  dragging = true;
  els.viewport.classList.add("dragging");
  dragStart = { x: e.clientX, y: e.clientY, ox: state.x, oy: state.y };
});
window.addEventListener("mousemove", (e) => {
  if (!dragging) return;
  state.x = dragStart.ox + (e.clientX - dragStart.x);
  state.y = dragStart.oy + (e.clientY - dragStart.y);
  applyTransform();
});
window.addEventListener("mouseup", () => {
  dragging = false;
  els.viewport.classList.remove("dragging");
});

// ---------- click-to-highlight ----------

function nodeIdOf(el) {
  // mermaid 11 node ids: "<renderId>-flowchart-<name>-<n>"
  const m = (el.id || "").match(/flowchart-(.+)-\d+$/);
  return m ? m[1] : null;
}

// mermaid 11 edge ids: "<renderId>-L_<src>_<tgt>_<n>" (older builds use "-").
// Node names contain "_", so resolve the split against known node names
// instead of trusting the delimiter.
function edgeEndpointsOf(el, names) {
  const m = (el.id || "").match(/L[-_](.+)[-_]\d+$/);
  if (!m) return null;
  const body = m[1];
  for (const src of names) {
    if (!body.startsWith(src)) continue;
    const rest = body.slice(src.length);
    if (rest.length < 2) continue;
    const tgt = rest.slice(1);
    if (names.has(tgt)) return [src, tgt];
  }
  return null; // can't determine identity — caller no-ops rather than throwing
}

function attachHighlight(svg) {
  if (!svg) return;
  const nodes = Array.from(svg.querySelectorAll(".node"));
  const edges = Array.from(svg.querySelectorAll(".edgePath, .flowchart-link"));
  if (!nodes.length) return;
  const names = new Set(nodes.map(nodeIdOf).filter(Boolean));

  function clear() {
    nodes.forEach((n) => n.classList.remove("focus", "dim"));
    edges.forEach((e) => e.classList.remove("focus", "dim"));
  }

  function neighborsOf(name) {
    const out = new Set();
    for (const e of edges) {
      const ends = edgeEndpointsOf(e, names);
      if (!ends) continue;
      if (ends[0] === name) out.add(ends[1]);
      if (ends[1] === name) out.add(ends[0]);
    }
    return out;
  }

  nodes.forEach((n) => {
    n.addEventListener("click", (e) => {
      e.stopPropagation();
      const name = nodeIdOf(n);
      if (!name) return; // can't determine identity — no-op rather than throw
      clear();
      const keep = neighborsOf(name);
      keep.add(name);
      nodes.forEach((other) => {
        const otherName = nodeIdOf(other);
        if (otherName && keep.has(otherName)) other.classList.add("focus");
        else other.classList.add("dim");
      });
      edges.forEach((edge) => {
        const ends = edgeEndpointsOf(edge, names);
        if (ends && (ends[0] === name || ends[1] === name)) edge.classList.add("focus");
        else edge.classList.add("dim");
      });
    });
  });

  svg.addEventListener("click", clear);
}

// ---------- source view / copy ----------

function setSourceVisible(visible) {
  state.showingSource = visible;
  els.source.hidden = !visible;
  els.viewport.hidden = visible;
  els.btnSource.classList.toggle("active", visible);
  if (!visible) renderCurrent();
}

els.btnSource.addEventListener("click", () => setSourceVisible(!state.showingSource));

els.btnCopy.addEventListener("click", async () => {
  const text = state.currentText;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const ta = document.createElement("textarea");
    Object.assign(ta.style, { position: "fixed", opacity: "0" });
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
  }
  const original = els.btnCopy.textContent;
  els.btnCopy.textContent = "copied";
  setTimeout(() => (els.btnCopy.textContent = original), 900);
});

// ---------- keyboard + deep links ----------

window.addEventListener("keydown", (e) => {
  const tag = document.activeElement?.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA") return;
  if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
  const ids = state.manifest?.diagrams.map((d) => d.id) || [];
  if (!ids.length) return;
  const idx = ids.indexOf(state.currentId);
  const next = e.key === "ArrowLeft" ? Math.max(0, idx - 1) : Math.min(ids.length - 1, idx + 1);
  if (ids[next] !== state.currentId) selectDiagram(ids[next]);
});

window.addEventListener("hashchange", () => {
  const id = location.hash.slice(1);
  if (id && id !== state.currentId) selectDiagram(id, { updateHash: false });
});

// ---------- boot ----------

async function main() {
  initTheme();
  const manifest = await loadManifest();
  if (!manifest) return;
  state.manifest = manifest;
  buildTabs(manifest);
  const requested = location.hash.slice(1);
  const initial = manifest.diagrams.find((d) => d.id === requested)?.id || manifest.diagrams[0]?.id;
  if (initial) await selectDiagram(initial, { updateHash: false });
}

main();
