import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { cleanupFocusTracking, initFocusTracking } from "../src/focus";
import { resetAppState } from "../src/reset";

function makeDeps() {
  return {
    renderSidebar: vi.fn(),
    maybeMarkActiveRead: vi.fn(),
  };
}

describe("focus tracking", () => {
  beforeEach(() => {
    resetAppState();
  });

  afterEach(() => {
    cleanupFocusTracking();
    vi.restoreAllMocks();
  });

  it("binds visibilitychange/focus/blur listeners once", () => {
    const docAdd = vi.spyOn(document, "addEventListener");
    const winAdd = vi.spyOn(window, "addEventListener");
    const deps = makeDeps();

    initFocusTracking(deps);
    initFocusTracking(deps); // second call is a no-op

    const docEvents = docAdd.mock.calls.map((c) => c[0]);
    const winEvents = winAdd.mock.calls.map((c) => c[0]);
    expect(docEvents.filter((e) => e === "visibilitychange")).toHaveLength(1);
    expect(winEvents.filter((e) => e === "focus")).toHaveLength(1);
    expect(winEvents.filter((e) => e === "blur")).toHaveLength(1);
  });

  it("cleanupFocusTracking removes the listeners it added", () => {
    const docRemove = vi.spyOn(document, "removeEventListener");
    const winRemove = vi.spyOn(window, "removeEventListener");

    initFocusTracking(makeDeps());
    cleanupFocusTracking();

    const docEvents = docRemove.mock.calls.map((c) => c[0]);
    const winEvents = winRemove.mock.calls.map((c) => c[0]);
    expect(docEvents).toContain("visibilitychange");
    expect(winEvents).toContain("focus");
    expect(winEvents).toContain("blur");
  });

  it("cleanup before init is a no-op (does not throw or detach)", () => {
    const docRemove = vi.spyOn(document, "removeEventListener");
    expect(() => cleanupFocusTracking()).not.toThrow();
    expect(docRemove).not.toHaveBeenCalled();
  });

  it("on blur sets state.uiFocused to false and renders sidebar", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const deps = makeDeps();
    state.uiFocused = true;
    state.activeId = "b1";

    initFocusTracking(deps);
    window.dispatchEvent(new Event("blur"));

    expect(state.uiFocused).toBe(false);
    expect(deps.renderSidebar).toHaveBeenCalled();
    expect(deps.maybeMarkActiveRead).not.toHaveBeenCalled();
  });

  it("on becoming focused while a buffer is active, sets activeFocusedSinceEnter and marks read", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const deps = makeDeps();
    state.uiFocused = false;
    state.activeFocusedSinceEnter = false;
    state.activeId = "b1";

    initFocusTracking(deps);
    window.dispatchEvent(new Event("focus"));

    expect(state.uiFocused).toBe(true);
    expect(state.activeFocusedSinceEnter).toBe(true);
    expect(deps.maybeMarkActiveRead).toHaveBeenCalledWith({ render: false });
    expect(deps.renderSidebar).toHaveBeenCalled();
  });

  it("does not mark read on focus when no active buffer", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const deps = makeDeps();
    state.uiFocused = false;
    state.activeFocusedSinceEnter = false;
    state.activeId = null;

    initFocusTracking(deps);
    window.dispatchEvent(new Event("focus"));

    expect(state.uiFocused).toBe(true);
    expect(state.activeFocusedSinceEnter).toBe(false);
    expect(deps.maybeMarkActiveRead).not.toHaveBeenCalled();
  });

  it("focus event when already focused is a no-op for activeFocusedSinceEnter", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const deps = makeDeps();
    state.uiFocused = true;
    state.activeFocusedSinceEnter = false;
    state.activeId = "b1";

    initFocusTracking(deps);
    window.dispatchEvent(new Event("focus"));

    expect(state.activeFocusedSinceEnter).toBe(false);
    expect(deps.maybeMarkActiveRead).not.toHaveBeenCalled();
  });

  it("visibilitychange routes through the same handler", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const deps = makeDeps();
    state.uiFocused = false;
    state.activeId = "b1";

    initFocusTracking(deps);
    document.dispatchEvent(new Event("visibilitychange"));

    expect(state.uiFocused).toBe(true);
    expect(state.activeFocusedSinceEnter).toBe(true);
  });

  it("after cleanup, events no longer mutate state", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const deps = makeDeps();
    state.uiFocused = true;

    initFocusTracking(deps);
    cleanupFocusTracking();
    deps.renderSidebar.mockClear();

    window.dispatchEvent(new Event("blur"));

    expect(state.uiFocused).toBe(true);
    expect(deps.renderSidebar).not.toHaveBeenCalled();
  });
});
