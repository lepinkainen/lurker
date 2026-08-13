import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Network, state } from "../src/app-state";
import { orderedNetworks } from "../src/buffers";
import { resetAppState } from "../src/reset";
import { openBufferOptions, renderSidebar, type SidebarDeps } from "../src/sidebar";

function buf(overrides: Partial<Buffer>): Buffer {
  return {
    id: "0",
    network_id: "10",
    name: "",
    kind: "channel",
    joined: true,
    unread: 0,
    mentions: 0,
    ...overrides,
  };
}

function net(overrides: Partial<Network> & { id: string }): Network {
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
    state.networks.set("3", net({ id: "3", sort_order: 1 }));
    state.networks.set("1", net({ id: "1", sort_order: 0 }));
    state.networks.set("2", net({ id: "2", sort_order: 1 }));
    expect(orderedNetworks().map((n) => n.id)).toEqual(["1", "2", "3"]);
  });

  it("places networks without sort_order last, sorted by id", () => {
    state.networks.set("5", net({ id: "5", sort_order: 0 }));
    state.networks.set("3", net({ id: "3" }));
    state.networks.set("2", net({ id: "2" }));
    expect(orderedNetworks().map((n) => n.id)).toEqual(["5", "2", "3"]);
  });
});

describe("renderSidebar", () => {
  beforeEach(() => {
    localStorage.clear();
    resetAppState();
  });

  it("clears scroll element when state empty", () => {
    const d = deps();
    d.sbScrollEl.innerHTML = "<div>old</div>";
    renderSidebar(d);
    expect(d.sbScrollEl.querySelector(".netsection")).toBeNull();
    expect(d.sbScrollEl.querySelector(".sb-add")).not.toBeNull();
  });

  it("renders network sections in order with channels and queries", () => {
    state.networks.set("1", net({ id: "1", name: "alpha", sort_order: 0 }));
    state.networks.set("2", net({ id: "2", name: "beta", sort_order: 1 }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#aaa", kind: "channel", joined: true }));
    state.buffers.set("11", buf({ id: "11", network_id: "1", name: "alice", kind: "query" }));
    state.buffers.set("12", buf({ id: "12", network_id: "1", name: "(status)", kind: "status" }));
    state.buffers.set("13", buf({ id: "13", network_id: "2", name: "#zzz", kind: "channel", joined: true }));
    const d = deps();
    renderSidebar(d);
    const names = [...d.sbScrollEl.querySelectorAll(".netname")].map((n) => n.textContent);
    expect(names).toEqual(["alpha", "beta"]);
    const rows = [...d.sbScrollEl.querySelectorAll(".sbrow.chan .name")].map((n) => n.textContent);
    expect(rows).toContain("#aaa");
    expect(rows).toContain("alice");
    expect(rows).toContain("#zzz");
  });

  it("sorts active channels by sort_order then name", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#alpha", sort_order: 2 }));
    state.buffers.set("11", buf({ id: "11", network_id: "1", name: "#beta", sort_order: 1 }));
    state.buffers.set("12", buf({ id: "12", network_id: "1", name: "#gamma", sort_order: 0 }));
    const d = deps();
    renderSidebar(d);
    const rows = [...d.sbScrollEl.querySelectorAll(".sbrow.channel .name")].map((n) => n.textContent);
    expect(rows).toEqual(["#gamma", "#beta", "#alpha"]);
  });

  it("renders pinned section at top when pinned buffers are present", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#aaa", joined: true, pinned: true }));
    const d = deps();
    renderSidebar(d);
    const first = d.sbScrollEl.firstElementChild;
    expect(first?.querySelector(".pinned-hdr")).not.toBeNull();
  });

  it("sorts pinned section by pin_order then name and keeps pinned channels under their network", () => {
    state.networks.set("1", net({ id: "1", sort_order: 0 }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#aaa", joined: true, pinned: true, pin_order: 1 }));
    state.buffers.set("11", buf({ id: "11", network_id: "1", name: "#zzz", joined: true, pinned: true, pin_order: 0 }));
    const d = deps();
    renderSidebar(d);
    const pinnedNames = [...d.sbScrollEl.querySelectorAll(".sbrow.pinned .name")].map((n) => n.textContent);
    expect(pinnedNames).toEqual(["#zzz", "#aaa"]);
    // Pinned channels also render under their network group (name-sorted).
    const chanNames = [...d.sbScrollEl.querySelectorAll(".sbrow.chan:not(.pinned) .name")].map((n) => n.textContent);
    expect(chanNames).toEqual(["#aaa", "#zzz"]);
  });

  it("toggles pinned buffers from the options dialog Pin checkbox", async () => {
    state.networks.set("1", net({ id: "1" }));
    const buffer = buf({ id: "10", network_id: "1", name: "#aaa", joined: true });
    state.buffers.set("10", buffer);
    const d = deps();
    renderSidebar(d);

    // Sidebar rows carry no inline pin control anymore — pinning lives in
    // the per-buffer options dialog (topicbar gear).
    expect(d.sbScrollEl.querySelector(".pin-toggle")).toBeNull();

    openBufferOptions(buffer, d);
    const dialog = document.body.querySelector("dialog");
    expect(dialog).not.toBeNull();
    const pinInput = [...(dialog?.querySelectorAll<HTMLElement>(".nf-checkbox-wrap") ?? [])]
      .find((w) => w.querySelector(".nf-checkbox-label")?.textContent === "Pin")
      ?.querySelector<HTMLInputElement>(".nf-checkbox");
    expect(pinInput).not.toBeNull();

    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ ...buffer, pinned: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    pinInput?.click();
    // Optimistic update applies synchronously, before the PATCH settles.
    expect(state.buffers.get("10")?.pinned).toBe(true);
    expect(d.sbScrollEl.firstElementChild?.querySelector(".pinned-hdr")).not.toBeNull();

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(fetchMock.mock.calls[0][0]).toBe("/api/buffers/10/settings");
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("PATCH");

    // No rollback: the PATCH succeeded, so the optimistic state persists.
    expect(state.buffers.get("10")?.pinned).toBe(true);

    dialog?.remove();
    vi.unstubAllGlobals();
  });

  it("collapses network when collapsed flag set and shows unread badge", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#x", joined: true, unread: 3 }));
    state.layout.collapsed[1] = true;
    const d = deps();
    renderSidebar(d);
    expect(d.sbScrollEl.querySelector(".net-hdr.collapsed")).not.toBeNull();
    expect(d.sbScrollEl.querySelector(".sbrow.chan")).toBeNull();
    expect(d.sbScrollEl.querySelector(".unreadbadge")?.textContent).toBe("3");
  });

  it("buffers a click row that triggers setActive", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#x", joined: true }));
    const d = deps();
    renderSidebar(d);
    const row = d.sbScrollEl.querySelector<HTMLButtonElement>(".sbrow.chan");
    expect(row).not.toBeNull();
    row?.click();
    expect(d.setActive).toHaveBeenCalledWith("10");
  });

  it("renders archive fold for archived buffers of any kind", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set(
      "10",
      buf({ id: "10", network_id: "1", name: "#left", kind: "channel", joined: false, archived: true }),
    );
    state.buffers.set("11", buf({ id: "11", network_id: "1", name: "spammer", kind: "query", archived: true }));
    const d = deps();
    renderSidebar(d);
    const arch = d.sbScrollEl.querySelector(".sbrow.archives");
    expect(arch).not.toBeNull();
    expect(arch?.querySelector(".archcount")?.textContent).toBe("2");
    expect(d.sbScrollEl.querySelectorAll(".sbrow.parted").length).toBe(0);
    state.layout.archivesOpen[1] = true;
    renderSidebar(d);
    expect(d.sbScrollEl.querySelectorAll(".sbrow.parted").length).toBe(2);
  });

  it("does not archive merely-parted channels (reconnect flicker guard)", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#down", kind: "channel", joined: false }));
    const d = deps();
    renderSidebar(d);
    expect(d.sbScrollEl.querySelector(".sbrow.archives")).toBeNull();
    // Still rendered in the normal channel list, dimmed via the parted class.
    expect(d.sbScrollEl.querySelectorAll(".sbrow.parted").length).toBe(1);
  });

  // openBufferOptions is triggered from the topicbar gear button
  // (app-view.ts's openActiveBufferOptions) rather than a sidebar row
  // control, so these exercise it directly with the same SidebarDeps the
  // sidebar rows use.
  it("opens the options dialog for query buffers and archives them from it", () => {
    state.networks.set("1", net({ id: "1" }));
    const buffer = buf({ id: "11", network_id: "1", name: "alice", kind: "query" });
    state.buffers.set("11", buffer);
    const d = deps();
    renderSidebar(d);
    openBufferOptions(buffer, d);
    const dialog = document.body.querySelector("dialog");
    expect(dialog).not.toBeNull();
    const labels = [...(dialog?.querySelectorAll(".nf-checkbox-label") ?? [])].map((l) => l.textContent);
    expect(labels).toContain("Archive conversation");
    dialog?.remove();
  });

  it("offers two-step delete for archived buffers and sends delete_buffer", () => {
    state.networks.set("1", net({ id: "1" }));
    const buffer = buf({ id: "10", network_id: "1", name: "#left", kind: "channel", joined: false, archived: true });
    state.buffers.set("10", buffer);
    state.layout.archivesOpen[1] = true;
    const d = deps();
    const sendCmd = vi.fn();
    d.sendCmd = sendCmd;
    renderSidebar(d);

    openBufferOptions(buffer, d);
    const dialog = document.body.querySelector("dialog");
    expect(dialog).not.toBeNull();
    const del = dialog?.querySelector<HTMLElement>(".chan-delete-btn");
    expect(del).not.toBeNull();
    del?.click();
    expect(sendCmd).not.toHaveBeenCalled();
    dialog?.querySelector<HTMLElement>(".chan-delete-confirm")?.click();
    expect(sendCmd).toHaveBeenCalledWith({ type: "delete_buffer", buffer_id: "10" });
    dialog?.remove();
  });

  it("hides delete for non-archived buffers", () => {
    state.networks.set("1", net({ id: "1" }));
    const buffer = buf({ id: "10", network_id: "1", name: "#active", kind: "channel", joined: true });
    state.buffers.set("10", buffer);
    const d = deps();
    d.sendCmd = vi.fn();
    renderSidebar(d);
    openBufferOptions(buffer, d);
    const dialog = document.body.querySelector("dialog");
    expect(dialog).not.toBeNull();
    expect(dialog?.querySelector(".chan-delete-btn")).toBeNull();
    dialog?.remove();
  });
});

describe("pinned section drag reorder", () => {
  // Every drag step re-renders the sidebar, so element references go stale —
  // always re-query rows between dispatched events.
  function pinnedRows(d: SidebarDeps) {
    return [...d.sbScrollEl.querySelectorAll<HTMLElement>(".sbrow.pinned")];
  }

  function pinnedNames(d: SidebarDeps) {
    return pinnedRows(d).map((row) => row.querySelector(".name")?.textContent);
  }

  function fire(el: Element, type: string) {
    el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }));
  }

  function setupPinned() {
    state.networks.set("1", net({ id: "1", sort_order: 0 }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#one", joined: true, pinned: true, pin_order: 0 }));
    state.buffers.set("11", buf({ id: "11", network_id: "1", name: "#two", joined: true, pinned: true, pin_order: 1 }));
    state.buffers.set(
      "12",
      buf({ id: "12", network_id: "1", name: "#three", joined: true, pinned: true, pin_order: 2 }),
    );
    const d = deps();
    renderSidebar(d);
    return d;
  }

  function okFetch(ids: string[]) {
    return vi.fn(
      async () =>
        new Response(JSON.stringify({ buffers: ids.map((id, idx) => ({ id, pin_order: idx })) }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
  }

  beforeEach(() => {
    localStorage.clear();
    resetAppState();
    vi.unstubAllGlobals();
  });

  it("marks the dragged row and shows the insertion bar on the hovered row", () => {
    const d = setupPinned();
    fire(pinnedRows(d)[2], "dragstart");
    fire(pinnedRows(d)[0], "dragover");
    const rows = pinnedRows(d);
    expect(rows[2].classList.contains("dragging")).toBe(true);
    expect(rows[0].classList.contains("dragover")).toBe(true);
    expect(rows[1].classList.contains("dragover")).toBe(false);
  });

  it("does not highlight pinned rows during a network drag", () => {
    const d = setupPinned();
    state.drag.id = "1";
    fire(pinnedRows(d)[0], "dragover");
    expect(state.pinDrag.over).toBeNull();
    expect(pinnedRows(d)[0].classList.contains("dragover")).toBe(false);
  });

  it("drops before the hovered row and posts the full ordered id list", async () => {
    const fetchMock = okFetch(["12", "10", "11"]);
    vi.stubGlobal("fetch", fetchMock);
    const d = setupPinned();

    fire(pinnedRows(d)[2], "dragstart");
    fire(pinnedRows(d)[0], "dragover");
    fire(pinnedRows(d)[0], "drop");

    expect(pinnedNames(d)).toEqual(["#three", "#one", "#two"]);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(fetchMock.mock.calls[0][0]).toBe("/api/buffers/pinned/reorder");
    expect(JSON.parse(String(init.body))).toEqual({ ids: ["12", "10", "11"] });
    expect(state.pinDrag.id).toBeNull();
  });

  it("appends to the last position via the end-of-list drop zone", async () => {
    const fetchMock = okFetch(["11", "12", "10"]);
    vi.stubGlobal("fetch", fetchMock);
    const d = setupPinned();
    const endZone = d.sbScrollEl.querySelector<HTMLElement>(".sb-pin-end");
    expect(endZone).not.toBeNull();

    fire(pinnedRows(d)[0], "dragstart");
    fire(endZone as HTMLElement, "dragover");
    const activeZone = d.sbScrollEl.querySelector<HTMLElement>(".sb-pin-end");
    expect(activeZone?.classList.contains("dragover")).toBe(true);
    fire(activeZone as HTMLElement, "drop");

    expect(pinnedNames(d)).toEqual(["#two", "#three", "#one"]);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({ ids: ["11", "12", "10"] });
  });

  it("rolls back the optimistic order when the backend has no reorder endpoint", async () => {
    const fetchMock = vi.fn(async () => new Response("404 page not found", { status: 404 }));
    vi.stubGlobal("fetch", fetchMock);
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {
      // Expected: the failed reorder logs before rolling back.
    });
    const d = setupPinned();

    fire(pinnedRows(d)[2], "dragstart");
    fire(pinnedRows(d)[0], "dragover");
    fire(pinnedRows(d)[0], "drop");
    expect(pinnedNames(d)).toEqual(["#three", "#one", "#two"]);

    await vi.waitFor(() => expect(pinnedNames(d)).toEqual(["#one", "#two", "#three"]));
    expect(state.buffers.get("12")?.pin_order).toBe(2);
    errSpy.mockRestore();
  });

  it("clears drag state on dragend", () => {
    const d = setupPinned();
    fire(pinnedRows(d)[0], "dragstart");
    fire(pinnedRows(d)[1], "dragover");
    fire(pinnedRows(d)[0], "dragend");
    expect(state.pinDrag).toEqual({ id: null, over: null });
    expect(pinnedRows(d).some((row) => row.classList.contains("dragover"))).toBe(false);
  });
});

describe("network section drag reorder", () => {
  function sections(d: SidebarDeps) {
    return [...d.sbScrollEl.querySelectorAll<HTMLElement>(".netsection")];
  }

  function names(d: SidebarDeps) {
    return [...d.sbScrollEl.querySelectorAll(".netname")].map((n) => n.textContent);
  }

  function endZone(d: SidebarDeps) {
    return d.sbScrollEl.querySelector<HTMLElement>(".sb-net-end");
  }

  function fire(el: Element, type: string) {
    el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }));
  }

  const NAMES: Record<string, string> = { "1": "alpha", "2": "beta", "3": "gamma" };

  // Echoes the names the sidebar already renders, so the server-confirmation
  // render is assertable and can be awaited — an unawaited confirmation would
  // otherwise land in the middle of a later test.
  function okFetch(ids: string[]) {
    return vi.fn(
      async () =>
        new Response(
          JSON.stringify({ networks: ids.map((id, idx) => net({ id, name: NAMES[id], sort_order: idx })) }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
    );
  }

  function setupNetworks() {
    for (const [id, name] of Object.entries(NAMES)) {
      state.networks.set(id, net({ id, name, sort_order: Number(id) - 1 }));
    }
    const d = deps();
    renderSidebar(d);
    return d;
  }

  beforeEach(() => {
    localStorage.clear();
    resetAppState();
    vi.unstubAllGlobals();
  });

  it("drops before the hovered section, matching the insertion bar", async () => {
    const fetchMock = okFetch(["2", "1", "3"]);
    vi.stubGlobal("fetch", fetchMock);
    const d = setupNetworks();

    // Dragging alpha down onto gamma must land it *before* gamma, where the bar
    // is drawn. The old index math inserted it after gamma as the last row.
    fire(sections(d)[0], "dragstart");
    fire(sections(d)[2], "dragover");
    expect(sections(d)[2].classList.contains("dragover")).toBe(true);
    fire(sections(d)[2], "drop");

    expect(names(d)).toEqual(["beta", "alpha", "gamma"]);
    await vi.waitFor(() => expect(state.networks.get("1")?.sort_order).toBe(1));
    expect(names(d)).toEqual(["beta", "alpha", "gamma"]);
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({ ids: ["2", "1", "3"] });
    expect(state.drag.id).toBeNull();
  });

  it("appends to the last position via the end-of-list drop zone", async () => {
    const fetchMock = okFetch(["2", "3", "1"]);
    vi.stubGlobal("fetch", fetchMock);
    const d = setupNetworks();
    expect(endZone(d)).not.toBeNull();

    fire(sections(d)[0], "dragstart");
    fire(endZone(d) as HTMLElement, "dragover");
    expect(endZone(d)?.classList.contains("dragover")).toBe(true);
    fire(endZone(d) as HTMLElement, "drop");

    expect(names(d)).toEqual(["beta", "gamma", "alpha"]);
    await vi.waitFor(() => expect(state.networks.get("1")?.sort_order).toBe(2));
    expect(names(d)).toEqual(["beta", "gamma", "alpha"]);
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({ ids: ["2", "3", "1"] });
  });

  it("omits the end-of-list zone when there are no active networks", () => {
    const d = deps();
    renderSidebar(d);
    expect(endZone(d)).toBeNull();
  });

  it("does not highlight network sections during a pinned drag", () => {
    const d = setupNetworks();
    state.pinDrag.id = "10";
    fire(sections(d)[0], "dragover");
    expect(state.drag.over).toBeNull();
    expect(sections(d)[0].classList.contains("dragover")).toBe(false);
  });

  it("rolls back the optimistic order when the reorder fails", async () => {
    const fetchMock = vi.fn(async () => new Response("boom", { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {
      // Expected: the failed reorder logs before rolling back.
    });
    const d = setupNetworks();

    fire(sections(d)[2], "dragstart");
    fire(sections(d)[0], "dragover");
    fire(sections(d)[0], "drop");
    expect(names(d)).toEqual(["gamma", "alpha", "beta"]);

    await vi.waitFor(() => expect(names(d)).toEqual(["alpha", "beta", "gamma"]));
    expect(state.networks.get("3")?.sort_order).toBe(2);
    errSpy.mockRestore();
  });
});
