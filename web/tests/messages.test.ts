import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Message, type Network, state } from "../src/app-state";
import { type MessagesDom, onBufferUpdate, onMessage, renderActiveView, renderHeader } from "../src/messages";
import { resetAppState } from "../src/reset";

function buf(overrides: Partial<Buffer> & { id: string }): Buffer {
  return {
    network_id: "1",
    name: "#chan",
    kind: "channel",
    joined: true,
    unread: 0,
    mentions: 0,
    last_seen_id: "0",
    ...overrides,
  };
}

function net(id: string, name = `n${id}`): Network {
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
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#lurker", topic: "welcome" }));
    state.networks.set("1", net("1", "libera"));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferNameEl.querySelector(".hash")?.textContent).toBe("#");
    expect(d.bufferNameEl.textContent).toBe("#lurker");
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toBe("welcome");
    expect(deps.renderPromptNick).toHaveBeenCalled();
  });

  it("renders status header with network info", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", kind: "status", name: "(status)" }));
    state.networks.set("1", net("1", "libera"));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferNameEl.textContent).toBe("libera (status)");
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toContain("libera.example");
  });

  it("falls back to 'No topic set' when topic missing", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", topic: "" }));
    state.networks.set("1", net("1"));
    const d = dom();
    renderHeader(d, deps);
    expect(d.bufferTopicEl.querySelector(".topictext")?.textContent).toBe("No topic set");
  });
});

describe("renderActiveView", () => {
  beforeEach(() => resetAppState());

  it("toggles status vs message panes by buffer kind", () => {
    state.activeId = "1";
    state.networks.set("1", net("1"));
    state.buffers.set("1", buf({ id: "1", kind: "status" }));
    const d = dom();
    renderActiveView(d, deps);
    expect(d.statusViewEl.hidden).toBe(false);
    expect(d.messagesEl.hidden).toBe(true);

    state.buffers.set("1", buf({ id: "1", kind: "channel", name: "#x" }));
    renderActiveView(d, deps);
    expect(d.statusViewEl.hidden).toBe(true);
    expect(d.messagesEl.hidden).toBe(false);
  });

  it("renders day separator and message rows", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x" }));
    state.me.nick = "you";
    const ts = "2024-05-01T10:00:00Z";
    const messages: Message[] = [
      { id: "1", buffer_id: "1", sender: "alice", content: "hi", kind: "message", ts },
      { id: "2", buffer_id: "1", sender: "alice", content: "you around?", kind: "message", ts },
    ];
    state.messages.set("1", messages);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelectorAll(".msg").length).toBe(2);
    expect(d.messagesEl.querySelector(".daysep")).not.toBeNull();
    expect(d.messagesEl.querySelector(".msg.mention")).not.toBeNull();
  });

  it("inserts unread bar for messages past last_seen_id", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", last_seen_id: "5" }));
    state.messages.set("1", [
      { id: "4", buffer_id: "1", sender: "a", content: "old", ts: "2024-05-01T10:00:00Z" },
      { id: "6", buffer_id: "1", sender: "a", content: "new", ts: "2024-05-01T10:01:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".unreadbar")).not.toBeNull();
  });

  it("does not insert unread bar for only state-change noise past last_seen_id", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", last_seen_id: "5" }));
    state.messages.set("1", [
      { id: "6", buffer_id: "1", sender: "a", content: "joined", kind: "join", ts: "2024-05-01T10:01:00Z" },
      { id: "7", buffer_id: "1", sender: "a", content: "+o a", kind: "mode", ts: "2024-05-01T10:02:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".unreadbar")).toBeNull();
  });

  it("collapses consecutive presence events when enabled", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", show_presence_events: true, collapse_presence_events: true }));
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "a", kind: "join", ts: "2024-05-01T10:00:00Z" },
      { id: "2", buffer_id: "1", sender: "b", kind: "part", ts: "2024-05-01T10:01:00Z" },
      { id: "3", buffer_id: "1", sender: "c", kind: "quit", ts: "2024-05-01T10:02:00Z" },
      { id: "4", buffer_id: "1", sender: "d", kind: "nick", ts: "2024-05-01T10:03:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    const summary = d.messagesEl.querySelector<HTMLElement>(".presence-summary");
    expect(summary?.dataset.presenceCount).toBe("4");
    expect(summary?.textContent).toContain("4 presence events");
    expect(summary?.textContent).toContain("1 join");
    expect(summary?.textContent).toContain("1 nick change");
    expect(d.messagesEl.querySelectorAll(".msg").length).toBe(1);
  });

  it("breaks collapsed presence groups around normal messages", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", show_presence_events: true, collapse_presence_events: true }));
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "a", kind: "join", ts: "2024-05-01T10:00:00Z" },
      { id: "2", buffer_id: "1", sender: "b", kind: "part", ts: "2024-05-01T10:01:00Z" },
      { id: "3", buffer_id: "1", sender: "alice", content: "hi", kind: "message", ts: "2024-05-01T10:02:00Z" },
      { id: "4", buffer_id: "1", sender: "c", kind: "join", ts: "2024-05-01T10:03:00Z" },
      { id: "5", buffer_id: "1", sender: "d", kind: "quit", ts: "2024-05-01T10:04:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(
      [...d.messagesEl.querySelectorAll<HTMLElement>(".msg")].map((row) => row.dataset.presenceCount || "msg"),
    ).toEqual(["2", "msg", "2"]);
  });

  it("renders individual presence events when collapse is disabled", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", show_presence_events: true, collapse_presence_events: false }));
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "a", kind: "join", ts: "2024-05-01T10:00:00Z" },
      { id: "2", buffer_id: "1", sender: "b", kind: "part", ts: "2024-05-01T10:01:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".presence-summary")).toBeNull();
    expect(d.messagesEl.querySelectorAll(".msg.sys").length).toBe(2);
  });

  it("hides presence events when show presence is disabled", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", show_presence_events: false, collapse_presence_events: true }));
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "a", kind: "join", ts: "2024-05-01T10:00:00Z" },
      { id: "2", buffer_id: "1", sender: "alice", content: "hi", kind: "message", ts: "2024-05-01T10:01:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    expect(d.messagesEl.querySelector(".presence-summary")).toBeNull();
    expect(d.messagesEl.querySelectorAll(".msg").length).toBe(1);
    expect(d.messagesEl.querySelector(".msg")?.textContent).toContain("hi");
  });

  it("expands collapsed presence events on click", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1", name: "#x", show_presence_events: true, collapse_presence_events: true }));
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "a", kind: "join", ts: "2024-05-01T10:00:00Z" },
      { id: "2", buffer_id: "1", sender: "b", kind: "part", ts: "2024-05-01T10:01:00Z" },
    ]);
    const d = dom();
    renderActiveView(d, deps);
    d.messagesEl.querySelector<HTMLElement>(".presence-summary")?.click();
    expect(d.messagesEl.querySelector<HTMLElement>(".presence-summary")?.dataset.expanded).toBe("true");
    expect(d.messagesEl.querySelectorAll(".presence-expanded").length).toBe(2);

    d.messagesEl
      .querySelector<HTMLElement>(".presence-summary")
      ?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    expect(d.messagesEl.querySelector<HTMLElement>(".presence-summary")?.dataset.expanded).toBe("false");
    expect(d.messagesEl.querySelectorAll(".presence-expanded").length).toBe(0);
  });
});

describe("onMessage", () => {
  beforeEach(() => resetAppState());

  it("appends to buffer and bumps unread/mentions on inactive buffer", () => {
    state.activeId = "99";
    state.me.nick = "you";
    state.buffers.set("1", buf({ id: "1" }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: "10", buffer_id: "1", sender: "alice", content: "hey you here?", kind: "message" }, handlers);
    const stored = state.messages.get("1");
    expect(stored?.length).toBe(1);
    expect(state.buffers.get("1")?.unread).toBe(1);
    expect(state.buffers.get("1")?.mentions).toBe(1);
    expect(handlers.renderSidebar).toHaveBeenCalled();
    expect(handlers.renderActiveView).not.toHaveBeenCalled();
  });

  it("does not bump unread for active buffer", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ id: "1" }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: "10", buffer_id: "1", sender: "a", content: "hi", kind: "message" }, handlers);
    expect(state.buffers.get("1")?.unread).toBe(0);
    expect(handlers.renderActiveView).toHaveBeenCalled();
  });

  it("does not bump unread or mentions for state-change noise on inactive buffer", () => {
    state.activeId = "99";
    state.me.nick = "you";
    state.buffers.set("1", buf({ id: "1", show_presence_events: true }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    ["join", "part", "quit", "nick", "mode", "kick", "connected", "disconnected", "error"].forEach((kind, idx) => {
      onMessage(
        { id: `10${idx}`, buffer_id: "1", sender: "alice", content: `mentions you in ${kind}`, kind },
        handlers,
      );
    });
    expect(state.buffers.get("1")?.unread).toBe(0);
    expect(state.buffers.get("1")?.mentions).toBe(0);
  });

  it("dedupes by id and keeps sorted", () => {
    state.activeId = "99";
    state.buffers.set("1", buf({ id: "1" }));
    const handlers = { renderActiveView: vi.fn(), maybeMarkActiveRead: vi.fn(), renderSidebar: vi.fn() };
    onMessage({ id: "2", buffer_id: "1", sender: "a", content: "two", kind: "message" }, handlers);
    onMessage({ id: "1", buffer_id: "1", sender: "a", content: "one", kind: "message" }, handlers);
    onMessage({ id: "2", buffer_id: "1", sender: "a", content: "two-dup", kind: "message" }, handlers);
    const stored = state.messages.get("1") || [];
    expect(stored.map((m) => m.id)).toEqual(["1", "2"]);
    expect(stored[1].content).toBe("two-dup");
  });
});

describe("onBufferUpdate", () => {
  beforeEach(() => resetAppState());

  it("applies topic/joined/last_seen and triggers handlers", () => {
    state.buffers.set("1", buf({ id: "1", topic: "old", joined: false, last_seen_id: "0" }));
    const handlers = { renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: "1", topic: "new topic", joined: true, last_seen_id: "42" }, handlers);
    const b = state.buffers.get("1");
    expect(b?.topic).toBe("new topic");
    expect(b?.joined).toBe(true);
    expect(b?.last_seen_id).toBe("42");
    expect(handlers.renderHeader).toHaveBeenCalled();
    expect(handlers.renderSidebar).toHaveBeenCalled();
  });

  it("applies server-authoritative unread/mentions when present", () => {
    state.buffers.set("1", buf({ id: "1", unread: 9, mentions: 3 }));
    const handlers = { renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: "1", last_seen_id: "42", unread: 0, mentions: 0 }, handlers);
    const b = state.buffers.get("1");
    expect(b?.unread).toBe(0);
    expect(b?.mentions).toBe(0);
  });

  it("preserves existing counts when buffer_update omits them", () => {
    state.buffers.set("1", buf({ id: "1", unread: 4, mentions: 1 }));
    const handlers = { renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: "1", topic: "x" }, handlers);
    const b = state.buffers.get("1");
    expect(b?.unread).toBe(4);
    expect(b?.mentions).toBe(1);
  });

  it("ignores unknown buffer", () => {
    const handlers = { renderHeader: vi.fn(), renderSidebar: vi.fn() };
    onBufferUpdate({ id: "999", topic: "x" }, handlers);
    expect(handlers.renderHeader).not.toHaveBeenCalled();
  });
});
