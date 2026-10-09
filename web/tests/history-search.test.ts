import { describe, expect, it } from "vitest";

import { highlightRuns, hitTimeLabel } from "../src/history-search";

const WEEKDAY_CLOCK = /^\S+ 22:17$/u;

describe("highlightRuns", () => {
  it("marks every query token case-insensitively, longest first", () => {
    const runs = highlightRuns("SQLite WAL mode, sqlite again", "sqlite wal");
    expect(runs).toEqual([
      { text: "SQLite", mark: true },
      { text: " ", mark: false },
      { text: "WAL", mark: true },
      { text: " mode, ", mark: false },
      { text: "sqlite", mark: true },
      { text: " again", mark: false },
    ]);
  });

  it("treats regex metacharacters in the query literally", () => {
    expect(highlightRuns("a.b vs axb", "a.b")).toEqual([
      { text: "a.b", mark: true },
      { text: " vs axb", mark: false },
    ]);
  });

  it("returns the text unmarked for a blank query", () => {
    expect(highlightRuns("hello", "   ")).toEqual([{ text: "hello", mark: false }]);
  });
});

describe("hitTimeLabel", () => {
  const now = new Date(2026, 9, 9, 15, 0, 0);
  it("shows only the clock today", () => {
    expect(hitTimeLabel(new Date(2026, 9, 9, 14, 41).toISOString(), now)).toBe("14:41");
  });
  it("adds the weekday inside a week", () => {
    expect(hitTimeLabel(new Date(2026, 9, 6, 22, 17).toISOString(), now)).toMatch(WEEKDAY_CLOCK);
  });
  it("drops the clock for older hits", () => {
    expect(hitTimeLabel(new Date(2026, 8, 28, 9, 0).toISOString(), now)).not.toContain(":");
  });
  it("is empty for missing or invalid timestamps", () => {
    expect(hitTimeLabel(undefined, now)).toBe("");
    expect(hitTimeLabel("nope", now)).toBe("");
  });
});
