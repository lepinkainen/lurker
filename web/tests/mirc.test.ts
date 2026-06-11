import { describe, expect, it } from "vitest";
import type { MircSegment } from "../src/app-state";
import { linkify } from "../src/format";
import { renderSegmentsHTML } from "../src/mirc";

// mIRC parsing is server-side (Go mirc package, mirc/mirc_test.go); the
// client renders pre-parsed segments. These tests cover the HTML rendering.

describe("renderSegmentsHTML", () => {
  it("renders an unstyled segment as plain escaped text", () => {
    expect(renderSegmentsHTML([{ text: "hello world" }])).toBe("hello world");
  });

  it("wraps bold runs", () => {
    const segs: MircSegment[] = [{ text: "a" }, { text: "bold", bold: true }, { text: "b" }];
    expect(renderSegmentsHTML(segs)).toBe('a<span class="m-b">bold</span>b');
  });

  it("supports italic, underline, strike, monospace", () => {
    expect(renderSegmentsHTML([{ text: "i", italic: true }])).toBe('<span class="m-i">i</span>');
    expect(renderSegmentsHTML([{ text: "u", underline: true }])).toBe('<span class="m-u">u</span>');
    expect(renderSegmentsHTML([{ text: "s", strike: true }])).toBe('<span class="m-s">s</span>');
    expect(renderSegmentsHTML([{ text: "m", mono: true }])).toBe('<span class="m-mono">m</span>');
  });

  it("combines overlapping styles", () => {
    expect(renderSegmentsHTML([{ text: "both", bold: true, italic: true }])).toBe('<span class="m-b m-i">both</span>');
  });

  it("applies foreground color via CSS var", () => {
    expect(renderSegmentsHTML([{ text: "red", fg: 4 }, { text: "plain" }])).toBe(
      '<span style="color:var(--mirc-c-4)">red</span>plain',
    );
  });

  it("applies fg+bg color", () => {
    expect(renderSegmentsHTML([{ text: "fgbg", fg: 3, bg: 1 }])).toBe(
      '<span style="color:var(--mirc-c-3);background:var(--mirc-c-1)">fgbg</span>',
    );
  });

  it("treats fg index 0 as styled (white)", () => {
    expect(renderSegmentsHTML([{ text: "w", fg: 0 }])).toBe('<span style="color:var(--mirc-c-0)">w</span>');
  });

  it("escapes HTML inside segment text", () => {
    expect(renderSegmentsHTML([{ text: "<script>", bold: true }])).toBe('<span class="m-b">&lt;script&gt;</span>');
  });

  it("composes with linkify", () => {
    const html = renderSegmentsHTML([{ text: "https://example.com", bold: true }, { text: " done" }]);
    expect(linkify(html)).toBe(
      '<span class="m-b"><a href="https://example.com" target="_blank" rel="noreferrer">https://example.com</a></span> done',
    );
  });

  it("handles empty input", () => {
    expect(renderSegmentsHTML([])).toBe("");
  });
});
