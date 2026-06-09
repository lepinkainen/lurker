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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
    });
    seedBuffer("b1");
    const focusSpy = vi.spyOn(dom.inputEl, "focus");

    setActive("b1");
    expect(focusSpy).toHaveBeenCalled();
    focusSpy.mockRestore();
  });

  it("invokes markBufferReadOnExit when leaving a previous buffer", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const markRead = vi.fn();
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
      markBufferReadOnExit: markRead,
      maybeMarkActiveRead: vi.fn(),
    });
    seedBuffer("b1");
    seedBuffer("b2");

    state.activeId = "b1";
    setActive("b2", { skipHash: true });
    expect(markRead).toHaveBeenCalled();
  });

  it("persists the active buffer to localStorage", () => {
    const dom = makeDom();
    const view = makeView(dom);
    const setActive = createSetActive({
      getDom: () => dom,
      getView: () => view,
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
      markBufferReadOnExit: vi.fn(),
      maybeMarkActiveRead: vi.fn(),
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
