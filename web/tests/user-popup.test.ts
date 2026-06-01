// biome-ignore-all lint/security/noSecrets: hex strings here are mIRC color codes, not secrets
import { describe, expect, it } from "vitest";
import { cleanRealname } from "../src/user-popup";

describe("cleanRealname", () => {
  it("returns empty for undefined/empty/whitespace", () => {
    expect(cleanRealname(undefined)).toBe("");
    expect(cleanRealname("")).toBe("");
    expect(cleanRealname("   ")).toBe("");
    expect(cleanRealname("\t\n  ")).toBe("");
  });

  it("trims surrounding whitespace", () => {
    expect(cleanRealname("  Riku Lindblad  ")).toBe("Riku Lindblad");
  });

  it("passes plain realnames through", () => {
    expect(cleanRealname("Riku Lindblad")).toBe("Riku Lindblad");
    expect(cleanRealname("Jonas Berlin · http://xkr47.space/")).toBe("Jonas Berlin · http://xkr47.space/");
  });

  it("strips mIRC color codes (single digit fg)", () => {
    expect(cleanRealname("\x034RED\x03")).toBe("RED");
    expect(cleanRealname("\x033green\x03 tail")).toBe("green tail");
  });

  it("strips mIRC color codes (two-digit fg)", () => {
    expect(cleanRealname("\x0312blue\x03end")).toBe("blueend");
  });

  it("strips mIRC color codes with bg", () => {
    expect(cleanRealname("\x034,1RED ON BLACK\x03")).toBe("RED ON BLACK");
    expect(cleanRealname("\x0304,01both\x03")).toBe("both");
  });

  it("strips bare \\x03 reset with no digits", () => {
    expect(cleanRealname("foo\x03bar")).toBe("foobar");
  });

  it("strips hex color \\x04 codes", () => {
    expect(cleanRealname("\x04ff0000RED\x04")).toBe("RED");
    expect(cleanRealname("\x04AABBCC,112233both\x04")).toBe("both");
  });

  it("strips formatting controls (bold/italic/underline/reset/reverse/strike/monospace)", () => {
    expect(cleanRealname("\x02bold\x02")).toBe("bold");
    expect(cleanRealname("\x1ditalic\x1d")).toBe("italic");
    expect(cleanRealname("\x1funder\x1f")).toBe("under");
    expect(cleanRealname("a\x0fb")).toBe("ab");
    expect(cleanRealname("a\x16b")).toBe("ab");
    expect(cleanRealname("a\x1eb")).toBe("ab");
    expect(cleanRealname("a\x11b")).toBe("ab");
  });

  it("handles mixed codes", () => {
    expect(cleanRealname("\x02\x034,1Hello\x03\x02 World")).toBe("Hello World");
  });

  it("consumes up to two digits after \\x03", () => {
    expect(cleanRealname("a\x0312b\x0312c")).toBe("abc");
  });

  it("leaves non-digit text after bare \\x03 intact", () => {
    expect(cleanRealname("a\x03bcdef")).toBe("abcdef");
  });

  it("preserves non-control unicode", () => {
    expect(cleanRealname("Yön Simo")).toBe("Yön Simo");
    expect(cleanRealname("日本語")).toBe("日本語");
  });
});
