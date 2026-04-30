import { describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { resetAppState } from "../src/reset";

describe("resetAppState", () => {
  it("clears transient app state and restores defaults", () => {
    const close = vi.fn();
    state.ws = { close } as unknown as WebSocket;
    state.wsReady = true;
    state.activeId = 99;
    state.networks.set(1, { id: 1, name: "Libera" });
    state.buffers.set(2, { id: 2, network_id: 1, name: "#lurker", kind: "channel", unread: 3, mentions: 1 });
    state.messages.set(2, [{ id: 10, buffer_id: 2, content: "hi" }]);
    state.members.set(2, [{ nick: "shrike", prefix: "@", away: false, self: true }]);
    state.inputHistory.set(2, { entries: ["hello"], draft: "draft", index: 0 });
    state.loadingHistory.add(2);
    state.historyExhausted.add(2);
    state.lastMarkedReadId.set(2, 10);
    state.me.nick = "shrike";
    state.showMemberList = false;
    state.drag = { id: 1, over: 2 };
    localStorage.setItem(
      "lurker.layout",
      JSON.stringify({ collapsed: { 5: true }, pinned: [2], archivesOpen: { 1: true } }),
    );

    resetAppState();

    expect(close).toHaveBeenCalledOnce();
    expect(state.networks.size).toBe(0);
    expect(state.buffers.size).toBe(0);
    expect(state.messages.size).toBe(0);
    expect(state.members.size).toBe(0);
    expect(state.inputHistory.size).toBe(0);
    expect(state.activeId).toBeNull();
    expect(state.ws).toBeNull();
    expect(state.wsReady).toBe(false);
    expect(state.loadingHistory.size).toBe(0);
    expect(state.historyExhausted.size).toBe(0);
    expect(state.lastMarkedReadId.size).toBe(0);
    expect(state.me.nick).toBe("you");
    expect(state.showMemberList).toBe(true);
    expect(state.layout).toEqual({
      collapsed: { 5: true },
      pinned: [2],
      archivesOpen: { 1: true },
      sidebarHidden: false,
    });
    expect(state.drag).toEqual({ id: null, over: null });
  });

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
