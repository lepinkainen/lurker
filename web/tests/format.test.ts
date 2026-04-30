import { describe, expect, it } from "vitest";
import {
  classifyKind,
  dayKeyOf,
  dotClass,
  escapeHTML,
  formatTime,
  highlightMentions,
  inlineCode,
  isSelf,
  linkify,
  mentionsMe,
  nickColor,
  nickHue,
} from "../src/format";

describe("escapeHTML", () => {
  it("escapes the five entity chars", () => {
    expect(escapeHTML(`<script>alert("x&y")</script>`)).toBe("&lt;script&gt;alert(&quot;x&amp;y&quot;)&lt;/script&gt;");
    expect(escapeHTML("it's")).toBe("it&#39;s");
  });

  it("returns empty string for null/undefined", () => {
    expect(escapeHTML(null)).toBe("");
    expect(escapeHTML(undefined)).toBe("");
  });

  it("coerces non-strings", () => {
    expect(escapeHTML(42)).toBe("42");
  });
});

describe("linkify", () => {
  it("wraps http(s) URLs in anchors", () => {
    expect(linkify("see https://example.com now")).toBe(
      'see <a href="https://example.com" target="_blank" rel="noreferrer">https://example.com</a> now',
    );
  });

  it("handles multiple URLs", () => {
    const out = linkify("a http://a.test b https://b.test");
    expect(out).toContain('href="http://a.test"');
    expect(out).toContain('href="https://b.test"');
  });

  it("does not match ftp://", () => {
    expect(linkify("ftp://x.test")).toBe("ftp://x.test");
  });

  it("passes plain text through", () => {
    expect(linkify("hello world")).toBe("hello world");
  });
});

describe("inlineCode", () => {
  it("wraps a backtick pair", () => {
    expect(inlineCode("run `npm test` now")).toBe("run <code>npm test</code> now");
  });

  it("handles multiple pairs", () => {
    expect(inlineCode("`a` and `b`")).toBe("<code>a</code> and <code>b</code>");
  });

  it("leaves unmatched backtick alone", () => {
    expect(inlineCode("`unclosed")).toBe("`unclosed");
  });
});

describe("highlightMentions", () => {
  const mention = (text: string) => `<span class="selfmention">${text}</span>`;

  it("wraps exact matches on word boundary", () => {
    expect(highlightMentions("hey alice", "alice")).toBe(`hey ${mention("alice")}`);
  });

  it("is case-insensitive", () => {
    expect(highlightMentions("hi ALICE!", "alice")).toBe(`hi ${mention("ALICE")}!`);
  });

  it("does not match inside words", () => {
    expect(highlightMentions("aliceish", "alice")).toBe("aliceish");
  });

  it("is a no-op when nick is empty", () => {
    expect(highlightMentions("alice hi", "")).toBe("alice hi");
  });
});

describe("mentionsMe", () => {
  it("detects exact nick", () => {
    expect(mentionsMe({ content: "hey alice" }, "alice")).toBe(true);
  });

  it("returns false without nick or content", () => {
    expect(mentionsMe({ content: "hi" }, "")).toBe(false);
    expect(mentionsMe({}, "alice")).toBe(false);
  });

  it("does not match substring", () => {
    expect(mentionsMe({ content: "aliceish" }, "alice")).toBe(false);
  });

  it("escapes regex metachars in nick", () => {
    expect(mentionsMe({ content: "hi a.b" }, "a.b")).toBe(true);
    expect(mentionsMe({ content: "hi axb" }, "a.b")).toBe(false);
  });
});

describe("isSelf", () => {
  it("matches case-insensitively", () => {
    expect(isSelf({ sender: "Alice" }, "alice")).toBe(true);
    expect(isSelf({ sender: "bob" }, "alice")).toBe(false);
  });

  it("returns true when both empty", () => {
    expect(isSelf({}, "")).toBe(true);
  });
});

describe("nickHue", () => {
  it("is deterministic", () => {
    expect(nickHue("alice")).toBe(nickHue("alice"));
  });

  it("is case-insensitive", () => {
    expect(nickHue("ALICE")).toBe(nickHue("alice"));
  });

  it("is in range 0..359", () => {
    for (const n of ["a", "alice", "bob", "", null]) {
      const h = nickHue(n);
      expect(h).toBeGreaterThanOrEqual(0);
      expect(h).toBeLessThan(360);
    }
  });
});

const OKLCH_FUNCTION_RE = /^oklch\(/;

describe("nickColor", () => {
  it("emits oklch with CSS vars", () => {
    const c = nickColor("alice");
    expect(c).toMatch(OKLCH_FUNCTION_RE);
    expect(c).toContain("var(--nick-l");
    expect(c).toContain("var(--nick-c");
    expect(c).toContain("deg)");
  });
});

describe("classifyKind", () => {
  it("maps IRC system kinds to sys", () => {
    for (const k of ["join", "part", "quit", "nick", "kick", "mode", "topic", "connected", "disconnected"]) {
      expect(classifyKind(k)).toBe("sys");
    }
  });

  it("maps notice/action", () => {
    expect(classifyKind("notice")).toBe("notice");
    expect(classifyKind("action")).toBe("action");
  });

  it("falls back to message", () => {
    expect(classifyKind("privmsg")).toBe("message");
    expect(classifyKind(undefined)).toBe("message");
    expect(classifyKind("")).toBe("message");
  });
});

describe("formatTime", () => {
  it("formats HH:MM with zero-pad", () => {
    const iso = new Date(2026, 0, 1, 7, 5).toISOString();
    expect(formatTime(iso)).toBe("07:05");
  });

  it("returns empty for invalid input", () => {
    expect(formatTime("not-a-date")).toBe("");
    expect(formatTime("")).toBe("");
    expect(formatTime(undefined)).toBe("");
  });
});

describe("dayKeyOf", () => {
  it("returns null for invalid/empty", () => {
    expect(dayKeyOf(null)).toBeNull();
    expect(dayKeyOf("")).toBeNull();
    expect(dayKeyOf("garbage")).toBeNull();
  });

  it("groups same local day", () => {
    const morning = new Date(2026, 3, 22, 1, 0).toISOString();
    const evening = new Date(2026, 3, 22, 23, 0).toISOString();
    expect(dayKeyOf(morning)).toBe(dayKeyOf(evening));
  });

  it("differs across days", () => {
    const d1 = new Date(2026, 3, 22, 12, 0).toISOString();
    const d2 = new Date(2026, 3, 23, 12, 0).toISOString();
    expect(dayKeyOf(d1)).not.toBe(dayKeyOf(d2));
  });
});

describe("dotClass", () => {
  it("maps connection status to dot class", () => {
    expect(dotClass("connected")).toBe("on");
    expect(dotClass("connecting")).toBe("warn");
    expect(dotClass("disconnected")).toBe("bad");
    expect(dotClass(undefined)).toBe("bad");
  });
});
