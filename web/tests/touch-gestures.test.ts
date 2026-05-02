import { describe, expect, it } from "vitest";
import { classifyHorizontalSwipe } from "../src/touch-gestures";

describe("swipe classification", () => {
  it("detects a right swipe", () => {
    expect(classifyHorizontalSwipe(0, 10, 80, 20)?.direction).toBe("right");
  });

  it("detects a left swipe", () => {
    expect(classifyHorizontalSwipe(100, 10, 20, 20)?.direction).toBe("left");
  });

  it("ignores short movement", () => {
    expect(classifyHorizontalSwipe(0, 0, 40, 0)).toBeNull();
  });

  it("ignores mostly vertical movement", () => {
    expect(classifyHorizontalSwipe(0, 0, 70, 70)).toBeNull();
  });
});
