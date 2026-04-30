import { type Buffer, type Network, saveLayout, state } from "./app-state";
import { patchBufferSettings } from "./buffer-settings-api";
import { orderedNetworks } from "./buffers";
import { dotClass } from "./format";
import { reorderNetworks, setNetworkDisabled } from "./network-api";
import { openNetworkForm } from "./network-form";
import { attachNetworkDragHandlers } from "./sidebar-dnd";
import { prepareSidebarModel, type SidebarNetworkModel } from "./sidebar-model";

export type SidebarDeps = {
  sbScrollEl: HTMLDivElement;
  setActive: (id: number) => void;
  iconEl: (symbolId: string, size: number, opts?: { className?: string; label?: string }) => SVGSVGElement;
};

export function renderSidebar(deps: SidebarDeps) {
  const { sbScrollEl } = deps;
  if (!sbScrollEl) return;
  sbScrollEl.innerHTML = "";

  const model = prepareSidebarModel();
  appendPinnedSection(sbScrollEl, model.pinned, deps);
  for (const network of model.activeNetworks) sbScrollEl.appendChild(networkSection(network, deps));
  appendDisabledSection(sbScrollEl, model.disabledNetworks, deps);
  sbScrollEl.appendChild(addNetworkButton(deps));
}

function rerender(deps: SidebarDeps) {
  renderSidebar(deps);
}

function appendPinnedSection(root: HTMLElement, pinned: Buffer[], deps: SidebarDeps) {
  if (pinned.length === 0) return;
  const sec = document.createElement("div");
  sec.className = "sb-section";
  const hdr = document.createElement("div");
  hdr.className = "sb-hdr pinned-hdr";
  const icon = document.createElement("span");
  icon.className = "pinico";
  icon.textContent = "⚑";
  const title = document.createElement("span");
  title.className = "title";
  title.textContent = "Pinned";
  hdr.append(icon, title);
  sec.appendChild(hdr);
  for (const buffer of pinned) sec.appendChild(bufferRow(buffer, deps, { pinned: true }));
  root.appendChild(sec);
}

function appendDisabledSection(root: HTMLElement, disabled: Network[], deps: SidebarDeps) {
  if (disabled.length === 0) return;
  const disabledSec = document.createElement("div");
  disabledSec.className = "sb-disabled-group";
  const disabledHdr = document.createElement("div");
  disabledHdr.className = "sb-disabled-hdr";
  disabledHdr.textContent = "Disabled";
  disabledSec.appendChild(disabledHdr);
  for (const network of disabled) disabledSec.appendChild(disabledNetworkRow(network, deps));
  root.appendChild(disabledSec);
}

function addNetworkButton(deps: SidebarDeps) {
  const add = document.createElement("button");
  add.type = "button";
  add.className = "sb-add";
  add.innerHTML = "<span>+</span><span>Add a network</span>";
  add.addEventListener("click", () => openNetworkForm(undefined, () => rerender(deps)));
  return add;
}

function networkSection(model: SidebarNetworkModel, deps: SidebarDeps) {
  const { network, collapsed } = model;
  const sec = document.createElement("div");
  sec.className = networkSectionClass(network.id);
  attachNetworkDragHandlers(sec, network, {
    render: () => rerender(deps),
    reorder: (fromId, toId) => {
      reorderNetwork(fromId, toId, deps).catch((err: unknown) => console.error("reorder failed", err));
    },
  });

  sec.appendChild(networkHeader(model, deps));
  if (!collapsed) appendOpenNetworkRows(sec, model, deps);
  return sec;
}

function networkSectionClass(networkId: number) {
  return [
    "sb-section",
    "netsection",
    state.drag.id === networkId && "dragging",
    state.drag.over === networkId && state.drag.id !== networkId && "dragover",
  ]
    .filter(Boolean)
    .join(" ");
}

function networkHeader(model: SidebarNetworkModel, deps: SidebarDeps) {
  const { network, collapsed, headerActive, buffers } = model;
  const hdr = document.createElement("button");
  hdr.type = "button";
  hdr.className = ["sb-hdr", "net-hdr", headerActive && "active", collapsed && "collapsed", dotClass(network.status)]
    .filter(Boolean)
    .join(" ");
  hdr.title = `${network.host || ""} · ${network.status || "offline"} · click to show server log`;

  hdr.append(collapseCaret(network, collapsed, deps), dragGrip(), networkName(network));
  if (network.tls) hdr.appendChild(tlsLockIcon(deps));
  hdr.appendChild(networkActions(model, deps));
  hdr.addEventListener("click", () => {
    if (buffers.status) deps.setActive(buffers.status.id);
  });
  return hdr;
}

function collapseCaret(network: Network, collapsed: boolean, deps: SidebarDeps) {
  const caret = document.createElement("span");
  caret.className = "caret";
  caret.textContent = collapsed ? "▸" : "▾";
  caret.addEventListener("click", (e) => {
    e.stopPropagation();
    state.layout.collapsed[network.id] = !collapsed;
    saveLayout(state.layout);
    rerender(deps);
  });
  return caret;
}

function dragGrip() {
  const grip = document.createElement("span");
  grip.className = "grip";
  grip.title = "Drag to reorder";
  grip.textContent = "⋮⋮";
  return grip;
}

function networkName(network: Network) {
  const name = document.createElement("span");
  name.className = "netname";
  name.textContent = network.name;
  return name;
}

function networkActions(model: SidebarNetworkModel, deps: SidebarDeps) {
  const actions = document.createElement("span");
  actions.className = "netactions";
  if (model.collapsed && model.buffers.mentionTotal > 0) {
    actions.appendChild(badge("mentionbadge", model.buffers.mentionTotal));
  } else if (model.collapsed && model.buffers.unreadTotal > 0) {
    actions.appendChild(badge("unreadbadge", model.buffers.unreadTotal));
  }
  actions.append(networkEditButton(model.network, deps));
  return actions;
}

function networkEditButton(network: Network, deps: SidebarDeps) {
  const editBtn = document.createElement("button");
  editBtn.type = "button";
  editBtn.className = "icbtn small net-edit";
  editBtn.title = "Edit network";
  editBtn.setAttribute("aria-label", "Edit network");
  editBtn.appendChild(deps.iconEl("ic-gear", 11));
  editBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    openNetworkForm(network, () => rerender(deps));
  });
  return editBtn;
}

function appendOpenNetworkRows(sec: HTMLElement, model: SidebarNetworkModel, deps: SidebarDeps) {
  for (const buffer of model.buffers.channels) sec.appendChild(bufferRow(buffer, deps));
  for (const buffer of model.buffers.queries) sec.appendChild(bufferRow(buffer, deps));
  appendArchiveSection(sec, model, deps);
}

function appendArchiveSection(sec: HTMLElement, model: SidebarNetworkModel, deps: SidebarDeps) {
  const { network, buffers } = model;
  if (buffers.parted.length === 0) return;
  const archOpen = Boolean(state.layout.archivesOpen[network.id]);
  const arch = document.createElement("button");
  arch.type = "button";
  arch.className = ["sbrow", "archives", archOpen && "open"].filter(Boolean).join(" ");
  arch.append(archiveCaret(archOpen), span("name", "Archive"), span("archcount", String(buffers.parted.length)));
  arch.addEventListener("click", () => {
    state.layout.archivesOpen[network.id] = !archOpen;
    saveLayout(state.layout);
    rerender(deps);
  });
  sec.appendChild(arch);
  if (archOpen) for (const buffer of buffers.parted) sec.appendChild(bufferRow(buffer, deps));
}

function archiveCaret(open: boolean) {
  return span("caret", open ? "▾" : "▸");
}

function disabledNetworkRow(network: Network, deps: SidebarDeps) {
  const row = document.createElement("div");
  row.className = "sb-disabled-row";
  const actions = document.createElement("span");
  actions.className = "sb-disabled-actions";
  actions.append(networkEditButton(network, deps), enableNetworkButton(network, deps));
  row.append(span("sb-disabled-name", network.name), actions);
  return row;
}

function enableNetworkButton(network: Network, deps: SidebarDeps) {
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
      rerender(deps);
    } catch (err) {
      console.error("enable network", err);
    }
  });
  return enableBtn;
}

function bufferRow(buffer: Buffer, deps: SidebarDeps, opts: { pinned?: boolean } = {}) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = bufferRowClass(buffer, opts.pinned);
  btn.appendChild(span("name", buffer.kind === "status" ? "(status)" : buffer.name));
  if (buffer.mentions > 0) btn.appendChild(badge("mentionbadge", buffer.mentions));
  else if (buffer.unread > 0) btn.appendChild(badge("unreadbadge", buffer.unread));
  if (buffer.kind === "channel") {
    btn.appendChild(channelOptionsToggle(buffer, deps));
    btn.appendChild(pinToggle(buffer, deps));
  }
  btn.addEventListener("click", () => deps.setActive(buffer.id));
  return btn;
}

function bufferRowClass(buffer: Buffer, pinned = false) {
  return [
    "sbrow",
    "chan",
    buffer.kind,
    pinned && "pinned",
    buffer.id === state.activeId && "active",
    buffer.unread > 0 && "unread",
    buffer.mentions > 0 && "mention",
    buffer.kind === "channel" && buffer.joined !== true && "parted",
  ]
    .filter(Boolean)
    .join(" ");
}

function pinToggle(buffer: Buffer, deps: SidebarDeps) {
  const pin = document.createElement("span");
  pin.className = ["pin-toggle", buffer.pinned && "active"].filter(Boolean).join(" ");
  pin.role = "button";
  pin.tabIndex = 0;
  pin.title = buffer.pinned ? "Unpin channel" : "Pin channel";
  pin.setAttribute("aria-label", pin.title);
  pin.textContent = buffer.pinned ? "⚑" : "⚐";
  const toggle = (e: Event) => {
    e.stopPropagation();
    updateBufferSettings(buffer, { pinned: !buffer.pinned }, deps).catch((err: unknown) =>
      console.error("buffer settings", err),
    );
  };
  pin.addEventListener("click", toggle);
  pin.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    toggle(e);
  });
  return pin;
}

function channelOptionsToggle(buffer: Buffer, deps: SidebarDeps) {
  const opt = document.createElement("span");
  opt.className = "chan-options";
  opt.role = "button";
  opt.tabIndex = 0;
  opt.title = "Channel display options";
  opt.setAttribute("aria-label", opt.title);
  opt.textContent = "⋯";
  const open = (e: Event) => {
    e.stopPropagation();
    openChannelOptions(buffer, deps);
  };
  opt.addEventListener("click", open);
  opt.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    open(e);
  });
  return opt;
}

function openChannelOptions(buffer: Buffer, deps: SidebarDeps) {
  const dialog = document.createElement("dialog");
  dialog.className = "settings-dialog channel-options-dialog";
  const title = document.createElement("h2");
  title.textContent = `${buffer.name} display`;
  const form = document.createElement("div");
  form.className = "settings-form";
  form.append(
    settingCheckbox("Pin", buffer.pinned, (checked) => updateBufferSettings(buffer, { pinned: checked }, deps)),
    settingCheckbox("Show embeds", buffer.show_embeds, (checked) =>
      updateBufferSettings(buffer, { show_embeds: checked }, deps),
    ),
    settingCheckbox("Show nick changes, joins and parts", buffer.show_presence_events, (checked) =>
      updateBufferSettings(buffer, { show_presence_events: checked }, deps),
    ),
  );
  if (buffer.show_presence_events) {
    form.append(
      settingCheckbox("Collapse presence events", buffer.collapse_presence_events, (checked) =>
        updateBufferSettings(buffer, { collapse_presence_events: checked }, deps),
      ),
    );
  }
  const close = document.createElement("button");
  close.type = "button";
  close.textContent = "Close";
  close.addEventListener("click", () => dialog.close());
  dialog.append(title, form, close);
  dialog.addEventListener("close", () => dialog.remove());
  document.body.appendChild(dialog);
  dialog.showModal();
}

function settingCheckbox(labelText: string, checked: boolean, onChange: (checked: boolean) => Promise<void>) {
  const label = document.createElement("label");
  label.className = "setting-row";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = checked;
  input.addEventListener("change", () => {
    onChange(input.checked).catch((err: unknown) => console.error("setting change", err));
  });
  label.append(input, document.createTextNode(labelText));
  return label;
}

async function updateBufferSettings(
  buffer: Buffer,
  patch: Partial<Pick<Buffer, "show_embeds" | "show_presence_events" | "collapse_presence_events" | "pinned">>,
  deps: SidebarDeps,
) {
  const previous = { ...buffer };
  Object.assign(buffer, patch);
  rerender(deps);
  try {
    const updated = await patchBufferSettings(buffer.id, patch);
    Object.assign(buffer, updated);
    rerender(deps);
  } catch (err) {
    console.error("buffer settings", err);
    Object.assign(buffer, previous);
    rerender(deps);
  }
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

function span(cls: string, text: string) {
  const el = document.createElement("span");
  el.className = cls;
  el.textContent = text;
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
  rerender(deps);

  try {
    const data = await reorderNetworks(ids);
    for (const network of data.networks || []) state.networks.set(network.id, network);
    rerender(deps);
  } catch (err) {
    console.error("reorder failed", err);
    for (const prev of previous) {
      const net = state.networks.get(prev.id);
      if (net) net.sort_order = prev.sort_order;
    }
    rerender(deps);
  }
}
