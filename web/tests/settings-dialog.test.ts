import { describe, expect, it } from "vitest";
import { formatBuildTime } from "../src/settings-dialog";

describe("formatBuildTime", () => {
  it("returns the raw string when not a parseable date", () => {
    expect(formatBuildTime("unknown")).toBe("unknown");
    expect(formatBuildTime("")).toBe("");
  });

  it("appends a relative age in minutes for recent builds", () => {
    const now = Date.parse("2026-07-29T12:00:00Z");
    const out = formatBuildTime("2026-07-29T11:30:00Z", now);
    expect(out.endsWith("(30 minutes ago)")).toBe(true);
  });

  it("uses hours within a day and days beyond", () => {
    const now = Date.parse("2026-07-29T12:00:00Z");
    expect(formatBuildTime("2026-07-29T07:00:00Z", now).endsWith("(5 hours ago)")).toBe(true);
    expect(formatBuildTime("2026-07-26T12:00:00Z", now).endsWith("(3 days ago)")).toBe(true);
  });

  it("keeps the ISO 8601 timestamp verbatim", () => {
    const now = Date.parse("2026-07-29T12:00:00Z");
    const out = formatBuildTime("2026-07-26T12:00:00Z", now);
    expect(out.startsWith("2026-07-26T12:00:00Z ")).toBe(true);
  });
});
