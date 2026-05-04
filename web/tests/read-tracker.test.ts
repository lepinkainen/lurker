import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { createReadTracker } from "../src/read-tracker";
import { resetAppState } from "../src/reset";

function seedBuffer(id = "b1", lastSeen = "0") {
  state.buffers.set(id, {
    id,
    network_id: "n1",
    name: "#chan",
    kind: "channel",
    last_seen_id: lastSeen,
    unread: 5,
    mentions: 2,
    show_embeds: true,
    show_presence_events: true,
    collapse_presence_events: false,
    pinned: false,
  });
  state.activeId = id;
}

describe("read-tracker", () => {
  beforeEach(() => {
    resetAppState();
  });
  afterEach(() => {
    resetAppState();
  });

  describe("maybeMarkActiveRead", () => {
    it("no-op when ws not ready", () => {
      seedBuffer();
      state.wsReady = false;
      state.messages.set("b1", [{ id: "m5", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when no buffer", () => {
      state.wsReady = true;
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when message list empty", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", []);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when lastId <= last_seen_id", () => {
      seedBuffer("b1", "m9");
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m5", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when lastId already sent", () => {
      seedBuffer();
      state.wsReady = true;
      state.lastMarkedReadId.set("b1", "m9");
      state.messages.set("b1", [{ id: "m5", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("emits mark_read, clears unread/mentions, calls renderSidebar", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [
        { id: "m1", buffer_id: "b1" },
        { id: "m9", buffer_id: "b1" },
      ]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).maybeMarkActiveRead();
      const buf = state.buffers.get("b1");
      expect(buf?.unread).toBe(0);
      expect(buf?.mentions).toBe(0);
      expect(buf?.last_seen_id).toBe("m9");
      expect(state.lastMarkedReadId.get("b1")).toBe("m9");
      expect(view.renderSidebar).toHaveBeenCalledOnce();
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
    });

    it("tolerates null view", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      expect(() => createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead()).not.toThrow();
      expect(sendCmd).toHaveBeenCalled();
    });
  });

  describe("loadOlderHistory", () => {
    it("no-op when no active buffer", () => {
      state.wsReady = true;
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when ws not ready", () => {
      seedBuffer();
      state.wsReady = false;
      state.messages.set("b1", [{ id: "m1", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when already loading", () => {
      seedBuffer();
      state.wsReady = true;
      state.loadingHistory.add("b1");
      state.messages.set("b1", [{ id: "m1", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when exhausted", () => {
      seedBuffer();
      state.wsReady = true;
      state.historyExhausted.add("b1");
      state.messages.set("b1", [{ id: "m1", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when message list empty", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", []);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("emits history cmd before earliest message and marks loading", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [
        { id: "m3", buffer_id: "b1" },
        { id: "m4", buffer_id: "b1" },
      ]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).loadOlderHistory();
      expect(sendCmd).toHaveBeenCalledWith({ type: "history", buffer_id: "b1", before: "m3", limit: 100 });
      expect(state.loadingHistory.has("b1")).toBe(true);
    });
  });
});
