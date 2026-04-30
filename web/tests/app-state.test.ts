import { afterEach, describe, expect, it } from "vitest";
import { loadLayout, saveLayout } from "../src/app-state";

describe("app-state layout persistence", () => {
  afterEach(() => {
    localStorage.clear();
  });

  it("returns default layout when nothing is saved", () => {
    expect(loadLayout()).toEqual({ collapsed: {}, pinned: [], archivesOpen: {}, sidebarHidden: false });
  });

  it("merges saved layout with defaults", () => {
    localStorage.setItem(
      "lurker.layout",
      JSON.stringify({
        collapsed: { 1: true },
        pinned: [10, 11],
      }),
    );

    expect(loadLayout()).toEqual({
      collapsed: { 1: true },
      pinned: [10, 11],
      archivesOpen: {},
      sidebarHidden: false,
    });
  });

  it("falls back to defaults on invalid json", () => {
    localStorage.setItem("lurker.layout", "not-json");
    expect(loadLayout()).toEqual({ collapsed: {}, pinned: [], archivesOpen: {}, sidebarHidden: false });
  });

  it("saves layout to localStorage", () => {
    const layout = {
      collapsed: { 2: true },
      pinned: [4],
      archivesOpen: { 7: true },
    };

    saveLayout(layout);

    expect(JSON.parse(localStorage.getItem("lurker.layout") ?? "null")).toEqual(layout);
  });
});
