import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Network, state } from "../src/app-state";
import { createAppView } from "../src/app-view";
import type { DomRefs } from "../src/dom";
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
    show_embeds: true,
    show_presence_events: true,
    collapse_presence_events: false,
    pinned: false,
    ...overrides,
  };
}

function net(overrides: Partial<Network> & { id: string }): Network {
  return { name: "libera", host: "irc.example", status: "connected", ...overrides };
}

function button(): HTMLButtonElement {
  return document.createElement("button");
}

function refs(): DomRefs {
  return {
    sidebarEl: document.createElement("aside"),
    sbScrollEl: document.createElement("div"),
    backendStatusEl: document.createElement("div"),
    backendStatusTextEl: document.createElement("span"),
    tailscaleStatusEl: document.createElement("div"),
    tailscaleStatusTextEl: document.createElement("span"),
    updateStatusEl: document.createElement("div"),
    updateStatusTextEl: document.createElement("span"),
    messagesEl: document.createElement("section"),
    statusViewEl: document.createElement("section"),
    bufferNameEl: document.createElement("div"),
    bufferTopicEl: document.createElement("div"),
    bufferMemcountEl: document.createElement("span"),
    memberCountInlineEl: document.createElement("span"),
    toggleMembersEl: button(),
    shortcutsHelpBtnEl: button(),
    mobileMenuEl: button(),
    inputEl: document.createElement("input"),
    inputForm: document.createElement("form"),
    uploadInputEl: document.createElement("input"),
    uploadButtonEl: button(),
    inputNickEl: document.createElement("span"),
    cmdPopEl: document.createElement("div"),
    memberListEl: document.createElement("div"),
    memberCountEl: document.createElement("span"),
    memberPaneEl: document.createElement("aside"),
  };
}

describe("createAppView", () => {
  beforeEach(() => {
    localStorage.clear();
    resetAppState();
  });

  it("renders active network nick in prompt", () => {
    state.activeId = "10";
    state.networks.set("1", net({ id: "1", nick: "tester" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1" }));
    const d = refs();
    const view = createAppView(d, { sendCmd: vi.fn(), setActive: vi.fn() });

    view.renderPromptNick();

    expect(d.inputNickEl.textContent).toContain("tester");
  });

  it("renders completed channel list as active view and joins selected channel", () => {
    state.activeId = "10";
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1" }));
    state.channelList = {
      network_id: "1",
      done: true,
      entries: [{ name: "#new", count: 42, topic: "fresh channel" }],
    };
    const d = refs();
    const sendCmd = vi.fn();
    const view = createAppView(d, { sendCmd, setActive: vi.fn() });

    view.renderActiveView();

    expect(d.statusViewEl.hidden).toBe(true);
    expect(d.messagesEl.hidden).toBe(false);
    expect(d.messagesEl.querySelector(".cl-panel")).not.toBeNull();
    expect(d.messagesEl.querySelector(".cl-name")?.textContent).toBe("#new");

    d.messagesEl.querySelector<HTMLButtonElement>(".cl-join")?.click();

    expect(sendCmd).toHaveBeenCalledWith({ type: "join", network_id: "1", channel: "#new" });
    expect(state.channelList).toBeNull();
    expect(d.messagesEl.querySelector(".cl-panel")).toBeNull();
  });

  it("wires sidebar buffer clicks through provided setActive callback", () => {
    state.networks.set("1", net({ id: "1" }));
    state.buffers.set("10", buf({ id: "10", network_id: "1", name: "#go" }));
    const d = refs();
    const setActive = vi.fn();
    const view = createAppView(d, { sendCmd: vi.fn(), setActive });

    view.renderSidebar();
    d.sbScrollEl.querySelector<HTMLButtonElement>(".sbrow.chan")?.click();

    expect(setActive).toHaveBeenCalledWith("10");
  });
});
