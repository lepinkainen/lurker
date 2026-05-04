import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import type { AppView } from "../src/app-view";
import { resetAppState } from "../src/reset";
import { createWSRouter } from "../src/ws-router";

function fakeView() {
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
  } as unknown as AppView & Record<string, ReturnType<typeof vi.fn>>;
}

describe("ws-router", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  it("routes message → appendMessage", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "message", id: "m1", buffer_id: "b1", content: "hi" });
    expect(view.appendMessage).toHaveBeenCalledWith({ type: "message", id: "m1", buffer_id: "b1", content: "hi" });
  });

  it("buffer_created seeds defaults and renders sidebar", () => {
    const view = fakeView();
    createWSRouter(view)({
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
    expect(view.renderSidebar).toHaveBeenCalled();
  });

  it("buffer_update routes through view.updateBuffer without rerender", () => {
    const view = fakeView();
    const m = { type: "buffer_update" as const, id: "b1", topic: "new" };
    createWSRouter(view)(m);
    expect(view.updateBuffer).toHaveBeenCalledWith(m, { rerenderActive: false });
  });

  it("buffer_settings forces rerenderActive", () => {
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

  it("network_state updates status and re-renders only on change", () => {
    const view = fakeView();
    state.networks.set("n1", { id: "n1", name: "Libera", status: "connected" });
    const route = createWSRouter(view);
    route({ type: "network_state", network_id: "n1", state: "connected" });
    expect(view.renderSidebar).not.toHaveBeenCalled();
    route({ type: "network_state", network_id: "n1", state: "disconnected" });
    expect(state.networks.get("n1")?.status).toBe("disconnected");
    expect(view.renderSidebar).toHaveBeenCalled();
    expect(view.renderHeader).toHaveBeenCalled();
  });

  it("network_state ignored for unknown network", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "network_state", network_id: "missing", state: "x" });
    expect(view.renderSidebar).not.toHaveBeenCalled();
  });

  it("history_result → prependHistory", () => {
    const view = fakeView();
    const m = { type: "history_result" as const, buffer_id: "b1", messages: [] };
    createWSRouter(view)(m);
    expect(view.prependHistory).toHaveBeenCalledWith(m);
  });

  it("preview → patchPreview", () => {
    const view = fakeView();
    const m = { type: "preview" as const, buffer_id: "b1", message_id: "m1", previews: [] };
    createWSRouter(view)(m);
    expect(view.patchPreview).toHaveBeenCalledWith(m);
  });

  it("member_list → setMembers", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "member_list", buffer_id: "b1", members: [] });
    expect(view.setMembers).toHaveBeenCalledWith("b1", []);
  });

  it("channel_list triggers renderActiveView when active matches and done", () => {
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

  it("channel_list does not trigger render when not done", () => {
    const view = fakeView();
    createWSRouter(view)({ type: "channel_list", network_id: "n1", entries: [], done: false });
    expect(view.renderActiveView).not.toHaveBeenCalled();
  });

  it("ignorelist_result logs", () => {
    const view = fakeView();
    const log = vi.spyOn(console, "log").mockImplementation(() => undefined);
    createWSRouter(view)({ type: "ignorelist_result", req_id: "r1", network_id: "n1", masks: ["a!*@*"] });
    expect(log).toHaveBeenCalledWith("ignore list:", ["a!*@*"]);
    log.mockRestore();
  });
});
