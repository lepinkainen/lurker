import { beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { resetAppState } from "../src/reset";

describe("resetAppState", () => {
  let closeSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    closeSpy = vi.fn();
    state.ws = { close: closeSpy } as unknown as WebSocket;
    state.wsReady = true;
    state.activeId = "99";
    state.networks.set("1", { id: "1", name: "Libera" });
    state.buffers.set("2", { id: "2", network_id: "1", name: "#lurker", kind: "channel", unread: 3, mentions: 1 });
    state.messages.set("2", [{ id: "10", buffer_id: "2", content: "hi" }]);
    state.members.set("2", [{ nick: "shrike", prefix: "@", away: false, self: true }]);
    state.inputHistory.set(2, { entries: ["hello"], draft: "draft", index: 0 });
    state.loadingHistory.add(2);
    state.historyExhausted.add(2);
    state.lastMarkedReadId.set(2, 10);
    state.markerAnchorId.set("2", "10");
    state.uiFocused = false;
    state.activeFocusedSinceEnter = false;
    state.me.nick = "shrike";
    state.showMemberList = false;
    state.drag = { id: "1", over: 2 };
    localStorage.setItem(
      "lurker.layout",
      JSON.stringify({ collapsed: { 5: true }, pinned: [2], archivesOpen: { 1: true } }),
    );

    resetAppState();
  });

  it("closes the websocket", () => {
    expect(closeSpy).toHaveBeenCalledOnce();
  });

  describe.each([
    ["networks", () => state.networks.size, 0],
    ["buffers", () => state.buffers.size, 0],
    ["messages", () => state.messages.size, 0],
    ["members", () => state.members.size, 0],
    ["inputHistory", () => state.inputHistory.size, 0],
    ["loadingHistory", () => state.loadingHistory.size, 0],
    ["historyExhausted", () => state.historyExhausted.size, 0],
    ["lastMarkedReadId", () => state.lastMarkedReadId.size, 0],
    ["markerAnchorId", () => state.markerAnchorId.size, 0],
  ])("clears collection %s", (_name, getter, expected) => {
    it("becomes empty", () => {
      expect(getter()).toBe(expected);
    });
  });

  describe.each([
    ["activeId", () => state.activeId, null],
    ["ws", () => state.ws, null],
    ["wsReady", () => state.wsReady, false],
    ["uiFocused", () => state.uiFocused, true],
    ["activeFocusedSinceEnter", () => state.activeFocusedSinceEnter, true],
    ["me.nick", () => state.me.nick, "you"],
    ["showMemberList", () => state.showMemberList, true],
  ])("restores scalar %s", (_name, getter, expected) => {
    it("matches default", () => {
      expect(getter()).toBe(expected);
    });
  });

  it("rehydrates layout from localStorage", () => {
    expect(state.layout).toEqual({
      collapsed: { 5: true },
      pinned: [2],
      archivesOpen: { 1: true },
      sidebarHidden: false,
    });
  });

  it("resets drag", () => {
    expect(state.drag).toEqual({ id: null, over: null });
  });
});

describe("resetAppState error handling", () => {
  it("ignores websocket close errors", () => {
    state.ws = {
      close: () => {
        throw new Error("boom");
      },
    } as unknown as WebSocket;

    expect(() => resetAppState()).not.toThrow();
    expect(state.ws).toBeNull();
  });
});
