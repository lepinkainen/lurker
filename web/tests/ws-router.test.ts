import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import type { AppView } from "../src/app-view";
import { resetAppState } from "../src/reset";
import { createWSRouter } from "../src/ws-router";

type FakeView = AppView & Record<string, ReturnType<typeof vi.fn>>;

function fakeView(): FakeView {
  return {
    dom: {} as never,
    renderStatus: vi.fn(),
    renderPromptNick: vi.fn(),
    renderHeader: vi.fn(),
    renderActiveView: vi.fn(),
    renderMembers: vi.fn(),
    updateInputEnabled: vi.fn(),
    renderSidebar: vi.fn(),
    appendMessage: vi.fn(),
    prependHistory: vi.fn(),
    patchPreview: vi.fn(),
    updateBuffer: vi.fn(),
    setMembers: vi.fn(),
    removeBuffer: vi.fn(),
  } as unknown as FakeView;
}

describe("ws-router state transformations", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  it("buffer_created seeds a buffer in state with default flags", () => {
    createWSRouter(fakeView())({
      type: "buffer_created",
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
    });
    const buf = state.buffers.get("b1");
    expect(buf).toMatchObject({
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      joined: false,
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
  });

  it("buffer_created preserves the server-assigned channel sort order", () => {
    createWSRouter(fakeView())({
      type: "buffer_created",
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      sort_order: 4,
    });
    expect(state.buffers.get("b1")?.sort_order).toBe(4);
  });

  it("buffer_reorder updates channel sort orders and rerenders the sidebar", () => {
    const alpha = {
      id: "b1",
      network_id: "n1",
      name: "#alpha",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
      sort_order: 0,
    };
    state.buffers.set("b1", alpha);
    state.buffers.set("b2", {
      ...alpha,
      id: "b2",
      name: "#beta",
      sort_order: 1,
    });
    const view = fakeView();

    createWSRouter(view)({
      type: "buffer_reorder",
      network_id: "n1",
      buffers: [
        { id: "b2", sort_order: 0 },
        { id: "b1", sort_order: 1 },
      ],
    });

    expect(state.buffers.get("b2")?.sort_order).toBe(0);
    expect(state.buffers.get("b1")?.sort_order).toBe(1);
    expect(view.renderSidebar).toHaveBeenCalledTimes(1);
  });

  it("buffer_deleted delegates to view.removeBuffer", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "buffer_deleted", id: "b1", network_id: "n1" });
    expect(view.removeBuffer).toHaveBeenCalledWith("b1");
  });

  it("network_state updates network status in state on change", () => {
    state.networks.set("n1", { id: "n1", name: "Libera", status: "connected" });
    const route = createWSRouter(fakeView());

    route({ type: "network_state", network_id: "n1", state: "connected" });
    expect(state.networks.get("n1")?.status).toBe("connected");

    route({ type: "network_state", network_id: "n1", state: "disconnected" });
    expect(state.networks.get("n1")?.status).toBe("disconnected");
  });

  it("network_state ignored for unknown network — state unchanged", () => {
    const before = state.networks.size;
    createWSRouter(fakeView())({ type: "network_state", network_id: "missing", state: "x" });
    expect(state.networks.size).toBe(before);
    expect(state.networks.has("missing")).toBe(false);
  });

  it("ignorelist_result writes masks to console", () => {
    const log = vi.spyOn(console, "log").mockImplementation(() => undefined);
    createWSRouter(fakeView())({ type: "ignorelist_result", req_id: "r1", network_id: "n1", masks: ["a!*@*"] });
    expect(log).toHaveBeenCalledWith("ignore list:", ["a!*@*"]);
    log.mockRestore();
  });
});

// Pure-dispatch contract: kind → handler. Router transforms nothing; it forwards.
// Asserting the mapping (not observable state) is intentional here because the
// router's whole job IS dispatch. Handler bodies are tested where they live.
describe("ws-router dispatch contract", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  const DISPATCH_CASES: Array<{
    kind: string;
    handler: keyof FakeView;
    msg: Record<string, unknown>;
  }> = [
    {
      kind: "message",
      handler: "appendMessage",
      msg: { type: "message", id: "m1", buffer_id: "b1", content: "hi" },
    },
    {
      kind: "buffer_update",
      handler: "updateBuffer",
      msg: { type: "buffer_update", id: "b1", topic: "new" },
    },
    {
      kind: "buffer_settings",
      handler: "updateBuffer",
      msg: {
        type: "buffer_settings",
        id: "b1",
        show_embeds: false,
        show_presence_events: true,
        collapse_presence_events: false,
        pinned: false,
      },
    },
    {
      kind: "history_result",
      handler: "prependHistory",
      msg: { type: "history_result", buffer_id: "b1", messages: [] },
    },
    {
      kind: "preview",
      handler: "patchPreview",
      msg: { type: "preview", buffer_id: "b1", message_id: "m1", previews: [] },
    },
    {
      kind: "member_list",
      handler: "setMembers",
      msg: { type: "member_list", buffer_id: "b1", members: [] },
    },
  ];

  it.each(DISPATCH_CASES)("$kind dispatches to $handler", ({ handler, msg }) => {
    const view = fakeView();
    createWSRouter(view)(msg);
    expect(view[handler]).toHaveBeenCalledTimes(1);
  });

  it("buffer_settings forces rerenderActive opt", () => {
    const view = fakeView();
    const m = {
      type: "buffer_settings" as const,
      id: "b1",
      show_embeds: false,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    };
    createWSRouter(view)(m);
    expect(view.updateBuffer).toHaveBeenCalledWith(m, { rerenderActive: true });
  });

  it("buffer_update for a non-active buffer does not force rerenderActive", () => {
    const view = fakeView();
    state.activeId = "other";
    const m = { type: "buffer_update" as const, id: "b1", topic: "new" };
    createWSRouter(view)(m);
    expect(view.updateBuffer).toHaveBeenCalledWith(m, { rerenderActive: false });
  });

  it("buffer_update for the active buffer forces rerenderActive (marker/divider are server state)", () => {
    const view = fakeView();
    state.activeId = "b1";
    const m = {
      type: "buffer_update" as const,
      id: "b1",
      last_seen_id: "12",
      marker_id: null,
      unread: 0,
      mentions: 0,
    };
    createWSRouter(view)(m);
    expect(view.updateBuffer).toHaveBeenCalledWith(m, { rerenderActive: true });
  });
});

describe("ws-router channel_list", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  it("triggers render only when applyChannelListUpdate returns true (done + active buffer matches)", () => {
    state.buffers.set("b1", {
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
    const view = fakeView();
    createWSRouter(view)({ type: "channel_list", network_id: "n1", entries: [], done: true });
    expect(view.renderActiveView).toHaveBeenCalled();
  });

  it("does not trigger render while not done", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "channel_list", network_id: "n1", entries: [], done: false });
    expect(view.renderActiveView).not.toHaveBeenCalled();
  });
});
