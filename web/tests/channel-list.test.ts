import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { applyChannelListUpdate, tryRenderActiveChannelList } from "../src/channel-list";
import { resetAppState } from "../src/reset";

function seedActive(networkId = "n1") {
  state.buffers.set("b1", {
    id: "b1",
    network_id: networkId,
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
}

describe("applyChannelListUpdate", () => {
  beforeEach(() => {
    resetAppState();
    state.channelList = null;
  });
  afterEach(() => {
    resetAppState();
    state.channelList = null;
  });

  it("initializes state on first update", () => {
    applyChannelListUpdate({ network_id: "n1", entries: [{ name: "#a", count: 1, topic: "" }], done: false });
    expect(state.channelList?.entries).toHaveLength(1);
    expect(state.channelList?.done).toBe(false);
  });

  it("appends entries on subsequent updates", () => {
    applyChannelListUpdate({ network_id: "n1", entries: [{ name: "#a", count: 1, topic: "" }], done: false });
    applyChannelListUpdate({ network_id: "n1", entries: [{ name: "#b", count: 2, topic: "" }], done: false });
    expect(state.channelList?.entries.map((e) => e.name)).toEqual(["#a", "#b"]);
  });

  it("resets when network_id changes", () => {
    applyChannelListUpdate({ network_id: "n1", entries: [{ name: "#a", count: 1, topic: "" }], done: false });
    applyChannelListUpdate({ network_id: "n2", entries: [{ name: "#z", count: 9, topic: "" }], done: false });
    expect(state.channelList?.network_id).toBe("n2");
    expect(state.channelList?.entries.map((e) => e.name)).toEqual(["#z"]);
  });

  it("returns true only when done and active matches network", () => {
    seedActive("n1");
    expect(applyChannelListUpdate({ network_id: "n1", entries: [], done: false })).toBe(false);
    expect(applyChannelListUpdate({ network_id: "n1", entries: [], done: true })).toBe(true);
  });

  it("returns false when done but active buffer on different network", () => {
    seedActive("n2");
    expect(applyChannelListUpdate({ network_id: "n1", entries: [], done: true })).toBe(false);
  });

  it("returns false when no active buffer", () => {
    expect(applyChannelListUpdate({ network_id: "n1", entries: [], done: true })).toBe(false);
  });
});

describe("tryRenderActiveChannelList", () => {
  beforeEach(() => {
    resetAppState();
    state.channelList = null;
  });
  afterEach(() => {
    resetAppState();
    state.channelList = null;
  });

  it("returns false when no channel list", () => {
    const messagesEl = document.createElement("div");
    const statusViewEl = document.createElement("div");
    const result = tryRenderActiveChannelList(messagesEl, statusViewEl, {
      sendCmd: vi.fn(),
      renderActiveView: vi.fn(),
    });
    expect(result).toBe(false);
  });

  it("returns false when channel list not done", () => {
    state.channelList = { network_id: "n1", entries: [], done: false };
    const messagesEl = document.createElement("div");
    const statusViewEl = document.createElement("div");
    const result = tryRenderActiveChannelList(messagesEl, statusViewEl, {
      sendCmd: vi.fn(),
      renderActiveView: vi.fn(),
    });
    expect(result).toBe(false);
  });

  it("toggles hidden flags and renders panel when done", () => {
    state.channelList = {
      network_id: "n1",
      entries: [{ name: "#a", count: 3, topic: "hi" }],
      done: true,
    };
    const messagesEl = document.createElement("div");
    messagesEl.hidden = true;
    const statusViewEl = document.createElement("div");
    statusViewEl.hidden = false;
    const result = tryRenderActiveChannelList(messagesEl, statusViewEl, {
      sendCmd: vi.fn(),
      renderActiveView: vi.fn(),
    });
    expect(result).toBe(true);
    expect(statusViewEl.hidden).toBe(true);
    expect(messagesEl.hidden).toBe(false);
    expect(messagesEl.querySelector(".cl-panel")).toBeTruthy();
    expect(messagesEl.querySelectorAll(".cl-row")).toHaveLength(1);
  });

  it("join button sends join cmd and closes list", () => {
    state.channelList = {
      network_id: "n1",
      entries: [{ name: "#a", count: 3, topic: "" }],
      done: true,
    };
    const messagesEl = document.createElement("div");
    const statusViewEl = document.createElement("div");
    const sendCmd = vi.fn();
    const renderActiveView = vi.fn();
    tryRenderActiveChannelList(messagesEl, statusViewEl, { sendCmd, renderActiveView });
    const joinBtn = messagesEl.querySelector<HTMLButtonElement>(".cl-join");
    joinBtn?.click();
    expect(sendCmd).toHaveBeenCalledWith({ type: "join", network_id: "n1", channel: "#a" });
    expect(state.channelList).toBeNull();
    expect(renderActiveView).toHaveBeenCalled();
  });

  it("close button clears state and rerenders", () => {
    state.channelList = { network_id: "n1", entries: [], done: true };
    const messagesEl = document.createElement("div");
    const statusViewEl = document.createElement("div");
    const renderActiveView = vi.fn();
    tryRenderActiveChannelList(messagesEl, statusViewEl, { sendCmd: vi.fn(), renderActiveView });
    messagesEl.querySelector<HTMLButtonElement>(".cl-close")?.click();
    expect(state.channelList).toBeNull();
    expect(renderActiveView).toHaveBeenCalled();
  });
});
