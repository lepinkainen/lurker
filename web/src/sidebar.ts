import { type Buffer, type Network, type ReorderResponse, saveLayout, state } from "./app-state";
import { dotClass } from "./format";

export type SidebarDeps = {
  sbScrollEl: HTMLDivElement;
  setActive: (id: number) => void;
  iconEl: (symbolId: string, size: number, opts?: { className?: string; label?: string }) => SVGSVGElement;
};

function byName(a: Buffer, b: Buffer) {
  return a.name.localeCompare(b.name);
}

export function orderedNetworks(): Network[] {
  return [...state.networks.values()].sort((a, b) => {
    const ao = a.sort_order ?? Number.MAX_SAFE_INTEGER;
    const bo = b.sort_order ?? Number.MAX_SAFE_INTEGER;
    return ao - bo || a.id - b.id;
  });
}

export function renderSidebar(deps: SidebarDeps) {
  const { sbScrollEl } = deps;
  if (!sbScrollEl) return;
  sbScrollEl.innerHTML = "";

  const pinned = (state.layout.pinned || []).map((id) => state.buffers.get(id)).filter(Boolean) as Buffer[];
  if (pinned.length) {
    const sec = document.createElement("div");
    sec.className = "sb-section";
    const hdr = document.createElement("div");
    hdr.className = "sb-hdr pinned-hdr";
    hdr.innerHTML = '<span class="pinico">⚑</span><span class="title">Pinned</span>';
    sec.appendChild(hdr);
    for (const buffer of pinned) sec.appendChild(bufferRow(buffer, deps, { pinned: true }));
    sbScrollEl.appendChild(sec);
  }

  for (const network of orderedNetworks()) sbScrollEl.appendChild(networkSection(network, deps));

  const add = document.createElement("button");
  add.type = "button";
  add.className = "sb-add";
  add.innerHTML = "<span>+</span><span>Add a network</span>";
  add.style.cssText =
    "margin:10px 10px 6px;padding:6px 10px;width:calc(100% - 20px);display:flex;align-items:center;gap:8px;color:var(--fg-2);font-family:var(--sans);font-size:12px;border:1px dashed var(--hair-strong);border-radius:5px;";
  sbScrollEl.appendChild(add);
}

function networkSection(network: Network, deps: SidebarDeps) {
  const sec = document.createElement("div");
  const collapsed = !!state.layout.collapsed[network.id];
  sec.className = [
    "sb-section",
    "netsection",
    state.drag.id === network.id && "dragging",
    state.drag.over === network.id && state.drag.id !== network.id && "dragover",
  ]
    .filter(Boolean)
    .join(" ");
  sec.draggable = true;
  sec.addEventListener("dragstart", (e) => {
    state.drag.id = network.id;
    try {
      e.dataTransfer?.setData("text/plain", String(network.id));
      if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    } catch {}
    sec.classList.add("dragging");
  });
  sec.addEventListener("dragend", () => {
    state.drag.id = null;
    state.drag.over = null;
    renderSidebar(deps);
  });
  sec.addEventListener("dragover", (e) => {
    if (state.drag.id == null || state.drag.id === network.id) return;
    e.preventDefault();
    if (state.drag.over !== network.id) {
      state.drag.over = network.id;
      renderSidebar(deps);
    }
  });
  sec.addEventListener("dragleave", () => {
    if (state.drag.over === network.id) state.drag.over = null;
  });
  sec.addEventListener("drop", (e) => {
    e.preventDefault();
    const fromId = state.drag.id;
    if (fromId != null && fromId !== network.id) void reorderNetwork(fromId, network.id, deps);
    state.drag.id = null;
    state.drag.over = null;
    renderSidebar(deps);
  });

  const netBufs = [...state.buffers.values()].filter((buffer) => buffer.network_id === network.id);
  const statusB = netBufs.find((buffer) => buffer.kind === "status");
  const channels = netBufs.filter((buffer) => buffer.kind === "channel" && buffer.joined === true).sort(byName);
  const queries = netBufs.filter((buffer) => buffer.kind === "query").sort(byName);
  const parted = netBufs.filter((buffer) => buffer.kind === "channel" && buffer.joined !== true).sort(byName);
  const headerActive = statusB && state.activeId === statusB.id;
  const dot = dotClass(network.status);
  const unreadTotal = netBufs.reduce((sum, buffer) => sum + (buffer.unread || 0), 0);
  const mentionTotal = netBufs.reduce((sum, buffer) => sum + (buffer.mentions || 0), 0);

  const hdr = document.createElement("button");
  hdr.type = "button";
  hdr.className = ["sb-hdr", "net-hdr", headerActive && "active", collapsed && "collapsed", dot]
    .filter(Boolean)
    .join(" ");
  hdr.title = `${network.host || ""} · ${network.status || "offline"} · click to show server log`;

  const caret = document.createElement("span");
  caret.className = "caret";
  caret.textContent = collapsed ? "▸" : "▾";
  caret.addEventListener("click", (e) => {
    e.stopPropagation();
    state.layout.collapsed[network.id] = !collapsed;
    saveLayout(state.layout);
    renderSidebar(deps);
  });
  const grip = document.createElement("span");
  grip.className = "grip";
  grip.title = "Drag to reorder";
  grip.textContent = "⋮⋮";
  const name = document.createElement("span");
  name.className = "netname";
  name.textContent = network.name;
  const tlsIcon = network.tls ? tlsLockIcon(deps) : null;
  const actions = document.createElement("span");
  actions.className = "netactions";
  if (collapsed && mentionTotal > 0) {
    actions.appendChild(badge("mentionbadge", mentionTotal));
  } else if (collapsed && unreadTotal > 0) {
    actions.appendChild(badge("unreadbadge", unreadTotal));
  }
  hdr.append(caret, grip, name);
  if (tlsIcon) hdr.appendChild(tlsIcon);
  hdr.appendChild(actions);
  hdr.addEventListener("click", () => {
    if (statusB) deps.setActive(statusB.id);
  });
  sec.appendChild(hdr);

  if (!collapsed) {
    for (const buffer of channels) sec.appendChild(bufferRow(buffer, deps));
    for (const buffer of queries) sec.appendChild(bufferRow(buffer, deps));
    if (parted.length) {
      const archOpen = !!state.layout.archivesOpen[network.id];
      const arch = document.createElement("button");
      arch.type = "button";
      arch.className = ["sbrow", "archives", archOpen && "open"].filter(Boolean).join(" ");
      const archCaret = document.createElement("span");
      archCaret.className = "caret";
      archCaret.textContent = archOpen ? "▾" : "▸";
      const nm = document.createElement("span");
      nm.className = "name";
      nm.textContent = "Archive";
      const cnt = document.createElement("span");
      cnt.className = "archcount";
      cnt.textContent = String(parted.length);
      arch.append(archCaret, nm, cnt);
      arch.addEventListener("click", () => {
        state.layout.archivesOpen[network.id] = !archOpen;
        saveLayout(state.layout);
        renderSidebar(deps);
      });
      sec.appendChild(arch);
      if (archOpen) {
        for (const buffer of parted) sec.appendChild(bufferRow(buffer, deps));
      }
    }
  }

  return sec;
}

function bufferRow(buffer: Buffer, deps: SidebarDeps, opts: { pinned?: boolean } = {}) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = [
    "sbrow",
    "chan",
    buffer.kind,
    opts.pinned && "pinned",
    buffer.id === state.activeId && "active",
    buffer.unread > 0 && "unread",
    buffer.mentions > 0 && "mention",
    buffer.kind === "channel" && buffer.joined !== true && "parted",
  ]
    .filter(Boolean)
    .join(" ");
  const display = buffer.kind === "status" ? "(status)" : buffer.name;
  const nm = document.createElement("span");
  nm.className = "name";
  nm.textContent = display;
  btn.appendChild(nm);
  if (buffer.mentions > 0) btn.appendChild(badge("mentionbadge", buffer.mentions));
  else if (buffer.unread > 0) btn.appendChild(badge("unreadbadge", buffer.unread));
  btn.addEventListener("click", () => deps.setActive(buffer.id));
  return btn;
}

function tlsLockIcon(deps: SidebarDeps) {
  return deps.iconEl("ic-lock", 12, { className: "tls-lock", label: "TLS enabled" });
}

function badge(cls: string, n: number) {
  const el = document.createElement("span");
  el.className = `badge ${cls}`;
  el.textContent = n > 99 ? "99+" : String(n);
  return el;
}

async function reorderNetwork(fromId: number, toId: number, deps: SidebarDeps) {
  const ids = orderedNetworks().map((network) => network.id);
  const fromIdx = ids.indexOf(fromId);
  const toIdx = ids.indexOf(toId);
  if (fromIdx < 0 || toIdx < 0) return;
  const [moved] = ids.splice(fromIdx, 1);
  ids.splice(toIdx, 0, moved);

  const previous = orderedNetworks().map((network) => ({ id: network.id, sort_order: network.sort_order }));
  ids.forEach((id, idx) => {
    const net = state.networks.get(id);
    if (net) net.sort_order = idx;
  });
  renderSidebar(deps);

  try {
    const res = await fetch("/api/networks/reorder", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ids }),
    });
    if (!res.ok) throw new Error(`reorder ${res.status}`);
    const data: ReorderResponse = await res.json();
    for (const network of data.networks || []) state.networks.set(network.id, network);
    renderSidebar(deps);
  } catch (err) {
    console.error("reorder failed", err);
    for (const prev of previous) {
      const net = state.networks.get(prev.id);
      if (net) net.sort_order = prev.sort_order;
    }
    renderSidebar(deps);
  }
}
