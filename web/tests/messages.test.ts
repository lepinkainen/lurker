import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Message, type Network, state } from "../src/app-state";
import {
  inferUnreadCounts,
  type MessagesDom,
  onBufferUpdate,
  onMessage,
  renderActiveView,
  renderHeader,
} from "../src/messages";
import { resetAppState } from "../src/reset";

function buf(overrides: Partial<Buffer> & { id: number }): Buffer {
  return {
    network_id: 1,
    name: "#chan",
    kind: "channel",
    joined: true,
    unread: 0,
    mentions: 0,
    last_seen_id: 0,
    ...overrides,
  };
}

function net(id: number, name = `n${id}`): Network {
  return { id, name, host: `${name}.example`, status: "connected" };
}

function dom(): MessagesDom {
  const messagesEl = document.createElement("section");
  const statusViewEl = document.createElement("section");
  statusViewEl.hidden = true;
  const bufferNameEl = document.createElement("div");
  const bufferTopicEl = document.createElement("div");
  const inputEl = document.createElement("input");
  return { messagesEl, statusViewEl, bufferNameEl, bufferTopicEl, inputEl };
}

const deps = {
  renderPromptNick: vi.fn(),
  iconEl: () => document.createElementNS("http://www.w3.org/2000/svg", "svg") as SVGSVGElement,
};

describe("renderHeader", () => {
  beforeEach(() => {
    resetAppState();
    deps.renderPromptNick.mockClear();
  });

  it("renders channel name with hash prefix", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, name: "#lurker", topic: "welcome" }));
    state.networks.set(1, net(1, "libera"));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferNameEl.querySelector(".hash")?.textContent).toBe("#");
    expect(d.bufferNameEl.textContent).toBe("#lurker");
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toBe("welcome");
    expect(deps.renderPromptNick).toHaveBeenCalled();
  });

  it("renders status header with network info", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, kind: "status", name: "(status)" }));
    state.networks.set(1, net(1, "libera"));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferNameEl.textContent).toBe("libera (status)");
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toContain("libera.example");
  });

  it("falls back to 'No topic set' when topic missing", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, name: "#x", topic: "" }));
    state.networks.set(1, net(1));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toBe("No topic set");
  });
});

describe("renderActiveView", () => {
  beforeEach(() => resetAppState());

  it("toggles status vs message panes by buffer kind", () => {
    state.activeId = 1;
    state.networks.set(1, net(1));
    state.buffers.set(1, buf({ id: 1, kind: "status" }));
    const d = dom();
    renderActiveView(d, deps);
    expect(d.statusViewEl.hidden).toBe(false);
    expect(d.messagesEl.hidden).toBe(true);

    state.buffers.set(1, buf({ id: 1, kind: "channel", name: "#x" }));
    renderActiveView(d, deps);
    expect(d.statusViewEl.hidden).toBe(true);
    expect(d.messagesEl.hidden).toBe(false);
  });

  it("renders day separator and message rows", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, name: "#x" }));
    state.me.nick = "you";
    const ts = "2024-05-01T10:00:00Z";
    const messages: Message[] = [
      { id: 1, buffer_id: 1, sender: "alice", content: "hi", kind: "message", ts },
      { id: 2, buffer_id: 1, sender: "alice", content: "you around?", kind: "message", ts },
    ];
    state.messages.set(1, messages);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelectorAll(".msg").length).toBe(2);
    expect(d.messagesEl.querySelector(".daysep")).not.toBeNull();
    expect(d.messagesEl.querySelector(".msg.mention")).not.toBeNull();
  });

  it("inserts unread bar for messages past last_seen_id", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, name: "#x", last_seen_id: 5 }));
    state.messages.set(1, [
      { id: 4, buffer_id: 1, sender: "a", content: "old", ts: "2024-05-01T10:00:00Z" },
      { id: 6, buffer_id: 1, sender: "a", content: "new", ts: "2024-05-01T10:01:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".unreadbar")).not.toBeNull();
  });

  it("does not insert unread bar for only state-change noise past last_seen_id", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1, name: "#x", last_seen_id: 5 }));
    state.messages.set(1, [
      { id: 6, buffer_id: 1, sender: "a", content: "joined", kind: "join", ts: "2024-05-01T10:01:00Z" },
      { id: 7, buffer_id: 1, sender: "a", content: "+o a", kind: "mode", ts: "2024-05-01T10:02:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".unreadbar")).toBeNull();
  });
});

describe("onMessage", () => {
  beforeEach(() => resetAppState());

  it("appends to buffer and bumps unread/mentions on inactive buffer", () => {
    state.activeId = 99;
    state.me.nick = "you";
    state.buffers.set(1, buf({ id: 1 }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: 10, buffer_id: 1, sender: "alice", content: "hey you here?", kind: "message" }, handlers);
    const stored = state.messages.get(1);
    expect(stored?.length).toBe(1);
    expect(state.buffers.get(1)?.unread).toBe(1);
    expect(state.buffers.get(1)?.mentions).toBe(1);
    expect(handlers.renderSidebar).toHaveBeenCalled();
    expect(handlers.renderActiveView).not.toHaveBeenCalled();
  });

  it("does not bump unread for active buffer", () => {
    state.activeId = 1;
    state.buffers.set(1, buf({ id: 1 }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: 10, buffer_id: 1, sender: "a", content: "hi", kind: "message" }, handlers);
    expect(state.buffers.get(1)?.unread).toBe(0);
    expect(handlers.renderActiveView).toHaveBeenCalled();
  });

  it("does not bump unread or mentions for state-change noise on inactive buffer", () => {
    state.activeId = 99;
    state.me.nick = "you";
    state.buffers.set(1, buf({ id: 1, show_presence_events: true }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    ["join", "part", "quit", "nick", "mode", "kick", "connected", "disconnected", "error"].forEach((kind, idx) => {
      onMessage({ id: 10 + idx, buffer_id: 1, sender: "alice", content: `mentions you in ${kind}`, kind }, handlers);
    });
    expect(state.buffers.get(1)?.unread).toBe(0);
    expect(state.buffers.get(1)?.mentions).toBe(0);
  });

  it("dedupes by id and keeps sorted", () => {
    state.activeId = 99;
    state.buffers.set(1, buf({ id: 1 }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: 2, buffer_id: 1, sender: "a", content: "two", kind: "message" }, handlers);
    onMessage({ id: 1, buffer_id: 1, sender: "a", content: "one", kind: "message" }, handlers);
    onMessage({ id: 2, buffer_id: 1, sender: "a", content: "two-dup", kind: "message" }, handlers);
    const stored = state.messages.get(1) || [];
    expect(stored.map((m) => m.id)).toEqual([1, 2]);
    expect(stored[1].content).toBe("two-dup");
  });
});

describe("inferUnreadCounts", () => {
  beforeEach(() => resetAppState());

  it("counts messages past last_seen and zeros active buffer", () => {
    state.activeId = 1;
    state.me.nick = "you";
    state.buffers.set(1, buf({ id: 1, last_seen_id: 0 }));
    state.buffers.set(2, buf({ id: 2, last_seen_id: 5 }));
    state.messages.set(1, [
      { id: 1, buffer_id: 1, sender: "a", content: "hi" },
      { id: 2, buffer_id: 1, sender: "a", content: "you?" },
    ]);
    state.messages.set(2, [
      { id: 4, buffer_id: 2, sender: "a", content: "old" },
      { id: 6, buffer_id: 2, sender: "a", content: "ping you" },
      { id: 7, buffer_id: 2, sender: "a", content: "more" },
    ]);
    inferUnreadCounts();
    expect(state.buffers.get(1)?.unread).toBe(0);
    expect(state.buffers.get(2)?.unread).toBe(2);
    expect(state.buffers.get(2)?.mentions).toBe(1);
  });

  it("excludes state-change noise but counts topic changes as unread activity", () => {
    state.activeId = 99;
    state.me.nick = "you";
    state.buffers.set(1, buf({ id: 1, last_seen_id: 5, show_presence_events: true }));
    state.messages.set(1, [
      { id: 6, buffer_id: 1, sender: "a", content: "you joined", kind: "join" },
      { id: 7, buffer_id: 1, sender: "a", content: "you parted", kind: "part" },
      { id: 8, buffer_id: 1, sender: "a", content: "new topic", kind: "topic" },
    ]);
    inferUnreadCounts();
    expect(state.buffers.get(1)?.unread).toBe(1);
    expect(state.buffers.get(1)?.mentions).toBe(0);
  });
});

describe("onBufferUpdate", () => {
  beforeEach(() => resetAppState());

  it("applies topic/joined/last_seen and triggers handlers", () => {
    state.buffers.set(1, buf({ id: 1, topic: "old", joined: false, last_seen_id: 0 }));
    const handlers = { inferUnreadCounts: vi.fn(), renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: 1, topic: "new topic", joined: true, last_seen_id: 42 }, handlers);
    const b = state.buffers.get(1);
    expect(b?.topic).toBe("new topic");
    expect(b?.joined).toBe(true);
    expect(b?.last_seen_id).toBe(42);
    expect(handlers.inferUnreadCounts).toHaveBeenCalled();
    expect(handlers.renderHeader).toHaveBeenCalled();
    expect(handlers.renderSidebar).toHaveBeenCalled();
  });

  it("ignores unknown buffer", () => {
    const handlers = { inferUnreadCounts: vi.fn(), renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: 999, topic: "x" }, handlers);
    expect(handlers.inferUnreadCounts).not.toHaveBeenCalled();
  });
});
