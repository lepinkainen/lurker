import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { applyTheme, persistThemeId, storedThemeId, type Theme } from "../src/theme";

function makeTheme(overrides: Partial<Theme> = {}): Theme {
  return {
    id: "test",
    name: "Test",
    colors: {},
    fonts: {},
    nicks: {},
    ...overrides,
  };
}

describe("applyTheme", () => {
  beforeEach(() => {
    const root = document.documentElement;
    for (const k of ["--bg", "--fg", "--sans", "--mono", "--nick-l", "--nick-c"]) {
      root.style.removeProperty(k);
    }
    delete root.dataset.theme;
  });

  it("writes color CSS vars on :root", () => {
    applyTheme(makeTheme({ colors: { bg: "#111", fg: "#eee" } }));
    expect(document.documentElement.style.getPropertyValue("--bg")).toBe("#111");
    expect(document.documentElement.style.getPropertyValue("--fg")).toBe("#eee");
  });

  it("writes font CSS vars when provided", () => {
    applyTheme(makeTheme({ fonts: { sans: "Inter", mono: "JetBrains Mono" } }));
    expect(document.documentElement.style.getPropertyValue("--sans")).toBe("Inter");
    expect(document.documentElement.style.getPropertyValue("--mono")).toBe("JetBrains Mono");
  });

  it("writes oklch nick params with units", () => {
    applyTheme(makeTheme({ nicks: { oklch_l: 70, oklch_c: 0.15 } }));
    expect(document.documentElement.style.getPropertyValue("--nick-l")).toBe("70%");
    expect(document.documentElement.style.getPropertyValue("--nick-c")).toBe("0.15");
  });

  it("sets data-theme attribute", () => {
    applyTheme(makeTheme({ id: "tokyo-night" }));
    expect(document.documentElement.dataset.theme).toBe("tokyo-night");
  });

  it("ignores absent color/font/nick fields", () => {
    expect(() => applyTheme({ id: "x", name: "x", colors: {}, fonts: {}, nicks: {} })).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe("x");
  });
});

describe("storedThemeId / persistThemeId", () => {
  afterEach(() => localStorage.clear());

  it("returns null when nothing stored", () => {
    expect(storedThemeId()).toBeNull();
  });

  it("round-trips through localStorage", () => {
    persistThemeId("solarized");
    expect(storedThemeId()).toBe("solarized");
  });
});
