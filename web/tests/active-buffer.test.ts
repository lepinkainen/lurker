import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createSetActive } from "../src/active-buffer";
import { state } from "../src/app-state";
import type { AppView } from "../src/app-view";
import type { DomRefs } from "../src/dom";
import { resetAppState } from "../src/reset";

function makeDom(): DomRefs {
  const inputEl = document.createElement("input");
  const cmdPopEl = document.createElement("div");
  const emojiPopEl = document.createElement("div");
  const nickPopEl = document.createElement("div");
  cmdPopEl.hidden = true;
  emojiPopEl.hidden = true;
  nickPopEl.hidden = true;
  return { inputEl, cmdPopEl, emojiPopEl, nickPopEl } as unknown as DomRefs;
}

function makeView(dom: DomRefs) {
  const view = {
    dom,
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
  return view;
}

const NOT_INITIALIZED_RE = /app view not initialized/u;

function seedBuffer(id: string) {
  state.networks.set("n1", { id: "n1", name: "Libera" });
  state.buffers.set(id, {
    id,
    network_id: "n1",
    name: `#${id}`,
    kind: "channel",
    unread: 0,
    mentions: 0,
    show_embeds: true,
    show_presence_events: true,
    collapse_presence_events: false,
    pinned: false,
  });
}

describe("createSetActive", () => {
  let originalHash: string;
  beforeEach(() => {
    resetAppState();
    localStorage.clear();
    originalHash = location.hash;
  });
  afterEach(() => {
    resetAppState();
    localStorage.clear();
    history.replaceState(null, "", `${location.pathname}${originalHash}`);
  });

  it("throws when view is null", () => {
    const dom = makeDom();
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => null,
    });
    seedBuffer("b1");
    expect(() => setActive("b1")).toThrow(NOT_INITIALIZED_RE);
  });

  it("sets state.activeId to the selected buffer", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");

    setActive("b1");
    expect(state.activeId).toBe("b1");
  });

  it("focuses the input on a non-touch device", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const focusSpy = vi.spyOn(dom.inputEl, "focus");

    setActive("b1");
    expect(focusSpy).toHaveBeenCalled();
    focusSpy.mockRestore();
  });

  it("never touches read state when switching buffers (no implicit mark_read)", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    seedBuffer("b2");
    const b1 = state.buffers.get("b1");
    if (!b1) throw new Error("missing b1");
    b1.unread = 7;
    b1.mentions = 2;
    b1.marker_id = "m5";
    b1.marker_ts = "2024-05-01T10:00:00Z";
    b1.last_seen_id = "m1";

    state.activeId = "b1";
    setActive("b2", { skipHash: true });
    // Exiting b1 (and entering b2) is not an ack: badge, divider, and unread
    // bar clear only on explicit Esc / unread-bar click.
    expect(b1.unread).toBe(7);
    expect(b1.mentions).toBe(2);
    expect(b1.marker_id).toBe("m5");
    expect(b1.last_seen_id).toBe("m1");
  });

  it("persists the active buffer to localStorage", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");

    setActive("b1");
    expect(localStorage.getItem("lurker.lastActive")).toBe("b1");
  });

  it("does not auto-focus input on coarse-pointer (touch) devices", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const focusSpy = vi.spyOn(dom.inputEl, "focus");
    const matchMediaSpy = vi
      .spyOn(window, "matchMedia")
      .mockImplementation((q: string) => ({ matches: q === "(pointer: coarse)" }) as MediaQueryList);

    setActive("b1");
    expect(focusSpy).not.toHaveBeenCalled();
    focusSpy.mockRestore();
    matchMediaSpy.mockRestore();
  });

  it("focuses input on keyboard-driven switches even on touch devices", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const focusSpy = vi.spyOn(dom.inputEl, "focus");
    const matchMediaSpy = vi
      .spyOn(window, "matchMedia")
      .mockImplementation((q: string) => ({ matches: q === "(pointer: coarse)" }) as MediaQueryList);

    setActive("b1", { focusInput: true });
    expect(focusSpy).toHaveBeenCalled();
    focusSpy.mockRestore();
    matchMediaSpy.mockRestore();
  });

  it("clears channelList state", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    state.channelList = { network_id: "n1", entries: [], done: true };

    setActive("b1");
    expect(state.channelList).toBeNull();
  });

  it("pushes hash by default", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const pushSpy = vi.spyOn(history, "pushState");

    setActive("b1");
    expect(pushSpy).toHaveBeenCalled();
    pushSpy.mockRestore();
  });

  it("replaces hash when replaceHash:true", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const replaceSpy = vi.spyOn(history, "replaceState");
    const pushSpy = vi.spyOn(history, "pushState");

    setActive("b1", { replaceHash: true });
    expect(replaceSpy).toHaveBeenCalled();
    expect(pushSpy).not.toHaveBeenCalled();
    replaceSpy.mockRestore();
    pushSpy.mockRestore();
  });

  it("skips hash when skipHash:true", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    const pushSpy = vi.spyOn(history, "pushState");
    const replaceSpy = vi.spyOn(history, "replaceState");

    setActive("b1", { skipHash: true });
    expect(pushSpy).not.toHaveBeenCalled();
    expect(replaceSpy).not.toHaveBeenCalled();
    pushSpy.mockRestore();
    replaceSpy.mockRestore();
  });

  it("saves draft of previous buffer when switching", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
    });
    seedBuffer("b1");
    seedBuffer("b2");

    state.activeId = "b1";
    dom.inputEl.value = "draft for b1";
    setActive("b2", { skipHash: true });

    expect(state.activeId).toBe("b2");
    setActive("b1", { skipHash: true });
    expect(dom.inputEl.value).toBe("draft for b1");
  });
});
