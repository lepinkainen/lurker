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
    state.uiFocused = true;
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

    it("no-op when UI not focused", () => {
      seedBuffer();
      state.wsReady = true;
      state.uiFocused = false;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).maybeMarkActiveRead();
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("keeps marker anchor on mark-read (marker persists while viewing)", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).maybeMarkActiveRead();
      expect(state.markerAnchorId.get("b1")).toBe("m5");
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
    });
  });

  describe("clearActiveMarker", () => {
    it("clears anchor on active buffer and re-renders active view", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn(), renderActiveView: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).clearActiveMarker();
      expect(state.markerAnchorId.has("b1")).toBe(false);
      expect(view.renderActiveView).toHaveBeenCalledOnce();
    });

    it("also marks the buffer read", () => {
      seedBuffer();
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).clearActiveMarker();
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
    });

    it("no-op render when no anchor present", () => {
      seedBuffer("b1", "m9");
      state.wsReady = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn(), renderActiveView: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).clearActiveMarker();
      expect(view.renderActiveView).not.toHaveBeenCalled();
    });

    it("no-op when no active buffer", () => {
      state.wsReady = true;
      const sendCmd = vi.fn();
      expect(() => createReadTracker({ getView: () => null, sendCmd }).clearActiveMarker()).not.toThrow();
      expect(sendCmd).not.toHaveBeenCalled();
    });
  });

  // biome-ignore lint/security/noSecrets: function-name-not-a-secret
  describe("markBufferReadOnExit", () => {
    it("no-op when activeFocusedSinceEnter is false", () => {
      seedBuffer();
      state.wsReady = true;
      state.activeFocusedSinceEnter = false;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).markBufferReadOnExit("b1");
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("no-op on null buffer id", () => {
      const sendCmd = vi.fn();
      state.activeFocusedSinceEnter = true;
      createReadTracker({ getView: () => null, sendCmd }).markBufferReadOnExit(null);
      expect(sendCmd).not.toHaveBeenCalled();
    });

    it("marks targeted buffer read regardless of focus state", () => {
      seedBuffer();
      state.wsReady = true;
      state.activeFocusedSinceEnter = true;
      state.uiFocused = false; // focus lost at moment of exit; was focused at some point
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      const sendCmd = vi.fn();
      const view = { renderSidebar: vi.fn() };
      createReadTracker({ getView: () => view as never, sendCmd }).markBufferReadOnExit("b1");
      expect(sendCmd).toHaveBeenCalledWith({ type: "mark_read", buffer_id: "b1", message_id: "m9" });
      expect(state.markerAnchorId.has("b1")).toBe(false);
    });

    it("clears anchor even when read position was already sent", () => {
      seedBuffer();
      state.wsReady = true;
      state.activeFocusedSinceEnter = true;
      state.lastMarkedReadId.set("b1", "m9");
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).markBufferReadOnExit("b1");
      expect(sendCmd).not.toHaveBeenCalled();
      expect(state.markerAnchorId.has("b1")).toBe(false);
    });

    it("keeps anchor when WS is down (read boundary would be unrecoverable)", () => {
      seedBuffer();
      state.wsReady = false;
      state.activeFocusedSinceEnter = true;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).markBufferReadOnExit("b1");
      expect(sendCmd).not.toHaveBeenCalled();
      expect(state.markerAnchorId.get("b1")).toBe("m5");
    });

    it("keeps anchor when never focused since enter", () => {
      seedBuffer();
      state.wsReady = true;
      state.activeFocusedSinceEnter = false;
      state.messages.set("b1", [{ id: "m9", buffer_id: "b1" }]);
      state.markerAnchorId.set("b1", "m5");
      const sendCmd = vi.fn();
      createReadTracker({ getView: () => null, sendCmd }).markBufferReadOnExit("b1");
      expect(state.markerAnchorId.get("b1")).toBe("m5");
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
