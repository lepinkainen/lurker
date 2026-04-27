import { describe, expect, it } from "vitest";
import { escapeHTML, linkify } from "../src/format";
import { mircFormat } from "../src/mirc";

describe("mircFormat", () => {
  it("passes plain text through unchanged", () => {
    expect(mircFormat("hello world")).toBe("hello world");
  });

  it("wraps bold runs", () => {
    expect(mircFormat("a\x02bold\x02b")).toBe('a<span class="m-b">bold</span>b');
  });

  it("supports italic, underline, strike, monospace", () => {
    expect(mircFormat("\x1di\x1d")).toBe('<span class="m-i">i</span>');
    expect(mircFormat("\x1fu\x1f")).toBe('<span class="m-u">u</span>');
    expect(mircFormat("\x1es\x1e")).toBe('<span class="m-s">s</span>');
    expect(mircFormat("\x11m\x11")).toBe('<span class="m-mono">m</span>');
  });

  it("combines overlapping styles", () => {
    expect(mircFormat("\x02\x1dboth\x02\x1d")).toBe('<span class="m-b m-i">both</span>');
  });

  it("emits separate spans when styles change mid-string", () => {
    expect(mircFormat("\x02a\x1db\x02c\x1d")).toBe(
      '<span class="m-b">a</span><span class="m-b m-i">b</span><span class="m-i">c</span>',
    );
  });

  it("resets all formatting on \\x0f", () => {
    expect(mircFormat("\x02\x1dab\x0fcd")).toBe('<span class="m-b m-i">ab</span>cd');
  });

  it("applies foreground color", () => {
    expect(mircFormat("\x034red\x03plain")).toBe('<span style="color:var(--mirc-c-4)">red</span>plain');
  });

  it("applies fg+bg color", () => {
    expect(mircFormat("\x033,1fgbg\x03x")).toBe(
      '<span style="color:var(--mirc-c-3);background:var(--mirc-c-1)">fgbg</span>x',
    );
  });

  it("treats \\x03 with no digits as color reset only", () => {
    expect(mircFormat("\x02\x034ab\x03cd\x02")).toBe(
      '<span class="m-b" style="color:var(--mirc-c-4)">ab</span><span class="m-b">cd</span>',
    );
  });

  it("ignores out-of-range color codes (>15)", () => {
    expect(mircFormat("\x0399x")).toBe("x");
  });

  it("closes span at end of input", () => {
    expect(mircFormat("\x02unterminated")).toBe('<span class="m-b">unterminated</span>');
  });

  it("composes with linkify (escape happens before)", () => {
    const escaped = escapeHTML("\x02https://example.com\x02 done");
    expect(linkify(mircFormat(escaped))).toBe(
      '<span class="m-b"><a href="https://example.com" target="_blank" rel="noreferrer">https://example.com</a></span> done',
    );
  });

  it("handles empty input", () => {
    expect(mircFormat("")).toBe("");
  });
});
