import { type Buffer, type Network, type ReorderResponse, saveLayout, state } from "./app-state";
import { groupedBuffers, orderedNetworks } from "./buffers";
import { dotClass } from "./format";
import { openNetworkForm } from "./network-form";

async function setNetworkDisabled(id: number, disabled: boolean): Promise<Network> {
  const res = await fetch(`/api/networks/${id}`, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ disabled }),
  });
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  return (await res.json()) as Network;
}

export type SidebarDeps = {
  sbScrollEl: HTMLDivElement;
  setActive: (id: number) => void;
  iconEl: (symbolId: string, size: number, opts?: { className?: string; label?: string }) => SVGSVGElement;
};

export { orderedNetworks } from "./buffers";

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

  const all = orderedNetworks();
  const active = all.filter((n) => !n.disabled);
  const disabled = all.filter((n) => n.disabled);

  for (const network of active) sbScrollEl.appendChild(networkSection(network, deps));

  if (disabled.length > 0) {
    const disabledSec = document.createElement("div");
    disabledSec.className = "sb-disabled-group";
    const disabledHdr = document.createElement("div");
    disabledHdr.className = "sb-disabled-hdr";
    disabledHdr.textContent = "Disabled";
    disabledSec.appendChild(disabledHdr);
    for (const network of disabled) disabledSec.appendChild(disabledNetworkRow(network, deps));
    sbScrollEl.appendChild(disabledSec);
  }

  const add = document.createElement("button");
  add.type = "button";
  add.className = "sb-add";
  add.innerHTML = "<span>+</span><span>Add a network</span>";
  add.addEventListener("click", () => openNetworkForm(undefined, () => renderSidebar(deps)));
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

  const { status: statusB, channels, queries, parted } = groupedBuffers(network.id);
  const netBufs = [statusB, ...channels, ...queries, ...parted].filter(Boolean) as Buffer[];
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
  const editBtn = document.createElement("button");
  editBtn.type = "button";
  editBtn.className = "icbtn small net-edit";
  editBtn.title = "Edit network";
  editBtn.setAttribute("aria-label", "Edit network");
  editBtn.appendChild(deps.iconEl("ic-gear", 11));
  editBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    openNetworkForm(network, () => renderSidebar(deps));
  });
  actions.append(editBtn);
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

function disabledNetworkRow(network: Network, deps: SidebarDeps) {
  const row = document.createElement("div");
  row.className = "sb-disabled-row";

  const name = document.createElement("span");
  name.className = "sb-disabled-name";
  name.textContent = network.name;

  const actions = document.createElement("span");
  actions.className = "sb-disabled-actions";

  const editBtn = document.createElement("button");
  editBtn.type = "button";
  editBtn.className = "icbtn small net-edit";
  editBtn.title = "Edit network";
  editBtn.setAttribute("aria-label", "Edit network");
  editBtn.appendChild(deps.iconEl("ic-gear", 11));
  editBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    openNetworkForm(network, () => renderSidebar(deps));
  });

  const enableBtn = document.createElement("button");
  enableBtn.type = "button";
  enableBtn.className = "icbtn small net-enable";
  enableBtn.title = "Enable network";
  enableBtn.setAttribute("aria-label", "Enable network");
  enableBtn.textContent = "Enable";
  enableBtn.addEventListener("click", async (e) => {
    e.stopPropagation();
    try {
      const updated = await setNetworkDisabled(network.id, false);
      state.networks.set(updated.id, updated);
      renderSidebar(deps);
    } catch (err) {
      console.error("enable network", err);
    }
  });

  actions.append(editBtn, enableBtn);
  row.append(name, actions);
  return row;
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
