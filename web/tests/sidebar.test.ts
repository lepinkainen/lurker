import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Network, state } from "../src/app-state";
import { resetAppState } from "../src/reset";
import { orderedNetworks, renderSidebar, type SidebarDeps } from "../src/sidebar";

function buf(overrides: Partial<Buffer>): Buffer {
  return {
    id: 0,
    network_id: 10,
    name: "",
    kind: "channel",
    joined: true,
    unread: 0,
    mentions: 0,
    ...overrides,
  };
}

function net(overrides: Partial<Network> & { id: number }): Network {
  return { id: overrides.id, name: `net${overrides.id}`, status: "connected", ...overrides };
}

function deps(): SidebarDeps {
  return {
    sbScrollEl: document.createElement("div"),
    setActive: vi.fn(),
    iconEl: () => document.createElementNS("http://www.w3.org/2000/svg", "svg"),
  };
}

describe("orderedNetworks", () => {
  beforeEach(() => resetAppState());

  it("sorts by sort_order then id", () => {
    state.networks.set(3, net({ id: 3, sort_order: 1 }));
    state.networks.set(1, net({ id: 1, sort_order: 0 }));
    state.networks.set(2, net({ id: 2, sort_order: 1 }));
    expect(orderedNetworks().map((n) => n.id)).toEqual([1, 2, 3]);
  });

  it("places networks without sort_order last, sorted by id", () => {
    state.networks.set(5, net({ id: 5, sort_order: 0 }));
    state.networks.set(3, net({ id: 3 }));
    state.networks.set(2, net({ id: 2 }));
    expect(orderedNetworks().map((n) => n.id)).toEqual([5, 2, 3]);
  });
});

describe("renderSidebar", () => {
  beforeEach(() => resetAppState());

  it("clears scroll element when state empty", () => {
    const d = deps();
    d.sbScrollEl.innerHTML = "<div>old</div>";
    renderSidebar(d);
    expect(d.sbScrollEl.querySelector(".netsection")).toBeNull();
    expect(d.sbScrollEl.querySelector(".sb-add")).not.toBeNull();
  });

  it("renders network sections in order with channels and queries", () => {
    state.networks.set(1, net({ id: 1, name: "alpha", sort_order: 0 }));
    state.networks.set(2, net({ id: 2, name: "beta", sort_order: 1 }));
    state.buffers.set(10, buf({ id: 10, network_id: 1, name: "#aaa", kind: "channel", joined: true }));
    state.buffers.set(11, buf({ id: 11, network_id: 1, name: "alice", kind: "query" }));
    state.buffers.set(12, buf({ id: 12, network_id: 1, name: "(status)", kind: "status" }));
    state.buffers.set(13, buf({ id: 13, network_id: 2, name: "#zzz", kind: "channel", joined: true }));
    const d = deps();
    renderSidebar(d);
    const names = [...d.sbScrollEl.querySelectorAll(".netname")].map((n) => n.textContent);
    expect(names).toEqual(["alpha", "beta"]);
    const rows = [...d.sbScrollEl.querySelectorAll(".sbrow.chan .name")].map((n) => n.textContent);
    expect(rows).toContain("#aaa");
    expect(rows).toContain("alice");
    expect(rows).toContain("#zzz");
  });

  it("renders pinned section at top when pinned ids present", () => {
    state.networks.set(1, net({ id: 1 }));
    state.buffers.set(10, buf({ id: 10, network_id: 1, name: "#aaa", joined: true }));
    state.layout.pinned = [10];
    const d = deps();
    renderSidebar(d);
    const first = d.sbScrollEl.firstElementChild;
    expect(first?.querySelector(".pinned-hdr")).not.toBeNull();
  });

  it("collapses network when collapsed flag set and shows unread badge", () => {
    state.networks.set(1, net({ id: 1 }));
    state.buffers.set(10, buf({ id: 10, network_id: 1, name: "#x", joined: true, unread: 3 }));
    state.layout.collapsed[1] = true;
    const d = deps();
    renderSidebar(d);
    expect(d.sbScrollEl.querySelector(".net-hdr.collapsed")).not.toBeNull();
    expect(d.sbScrollEl.querySelector(".sbrow.chan")).toBeNull();
    expect(d.sbScrollEl.querySelector(".unreadbadge")?.textContent).toBe("3");
  });

  it("buffers a click row that triggers setActive", () => {
    state.networks.set(1, net({ id: 1 }));
    state.buffers.set(10, buf({ id: 10, network_id: 1, name: "#x", joined: true }));
    const d = deps();
    renderSidebar(d);
    const row = d.sbScrollEl.querySelector<HTMLButtonElement>(".sbrow.chan");
    expect(row).not.toBeNull();
    row?.click();
    expect(d.setActive).toHaveBeenCalledWith(10);
  });

  it("renders archive fold for parted channels", () => {
    state.networks.set(1, net({ id: 1 }));
    state.buffers.set(10, buf({ id: 10, network_id: 1, name: "#left", kind: "channel", joined: false }));
    const d = deps();
    renderSidebar(d);
    const arch = d.sbScrollEl.querySelector(".sbrow.archives");
    expect(arch).not.toBeNull();
    expect(arch?.querySelector(".archcount")?.textContent).toBe("1");
    expect(d.sbScrollEl.querySelectorAll(".sbrow.parted").length).toBe(0);
    state.layout.archivesOpen[1] = true;
    renderSidebar(d);
    expect(d.sbScrollEl.querySelectorAll(".sbrow.parted").length).toBe(1);
  });
});
