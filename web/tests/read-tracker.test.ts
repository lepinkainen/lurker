import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, state } from "../src/app-state";
import { createReadTracker } from "../src/read-tracker";
import { resetAppState } from "../src/reset";

function seedBuffer(id = "b1", overrides: Partial<Buffer> = {}) {
  state.buffers.set(id, {
    id,
    network_id: "n1",
    name: "#chan",
    kind: "channel",
    last_seen_id: "0",
    marker_id: "m5",
    marker_ts: "2024-05-01T10:00:00Z",
    unread: 5,
    mentions: 2,
    show_embeds: true,
    show_presence_events: true,
    collapse_presence_events: false,
    pinned: false,
    ...overrides,
  });
  state.activeId = id;
}

describe("read-tracker", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  describe("ackBufferRead", () => {
    it("no-op when ws not ready (server state stays authoritative)", () => {
      seedBuffer();
      state.wsReady = false;
      state.messages.set("b1", [{ id: "m5", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn(), renderActiveView: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).ackBufferRead("b1");
      expect(sendCmd).not.toHaveBeenCalled();
      expect(state.buffers.get("b1")?.marker_id).toBe("m5");
      expect(state.buffers.get("b1")?.unread).toBe(5);
    });

    it("no-op when buffer unknown", () => {
      state.wsReady = true;
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).ackBufferRead("nope");
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op when message list empty (nothing to ack)", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", []);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).ackBufferRead("b1");
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("optimistically clears marker/counts, sends mark_read, re-renders sidebar and active view", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [
        { id: "m1", buffer_id: "b1" },
        { id: "m9", buffer_id: "b1" },
      ]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn(), renderActiveView: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).ackBufferRead("b1");
      const buf = state.buffers.get("b1");
      expect(buf?.last_seen_id).toBe("m9");
      expect(buf?.marker_id).toBeUndefined();
      expect(buf?.marker_ts).toBeUndefined();
      expect(buf?.unread).toBe(0);
      expect(buf?.mentions).toBe(0);
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
      expect(view.renderSidebar).toHaveBeenCalledOnce();
      expect(view.renderActiveView).toHaveBeenCalledOnce();
    });

    it("skips active-view re-render when acked buffer is not active", () => {
      seedBuffer();
      state.activeId = "other";
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn(), renderActiveView: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).ackBufferRead("b1");
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
      expect(view.renderSidebar).toHaveBeenCalledOnce();
      expect(view.renderActiveView).not.toHaveBeenCalled();
    });

    it("acks even when already caught up (ack always moves to newest loaded)", () => {
      seedBuffer("b1", { last_seen_id: "m9", marker_id: undefined, marker_ts: undefined, unread: 0, mentions: 0 });
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).ackBufferRead("b1");
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
    });

    it("tolerates null view", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      expect(() => createReadTracker({ getView: () => null, sendCmd }).ackBufferRead("b1")).not.toThrow();
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
