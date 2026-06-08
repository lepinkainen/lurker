import { describe, expect, it } from "vitest";
import fx from "../../testdata/netsplit/cases.json";
import { caseFoldNick, parseNetsplitReason } from "../src/netsplit";

// Shared fixture loaded by Go (irc/netsplit_test.go TestSharedFixture) and
// TS so the two netsplit implementations cannot silently drift.

describe("netsplit shared fixture", () => {
  it("parseNetsplitReason matches Go", () => {
    for (const c of fx.reason) {
      const got = parseNetsplitReason(c.in);
      if (c.ok) {
        expect(got, `reason ${JSON.stringify(c.in)}`).toEqual({ a: c.a, b: c.b });
      } else {
        expect(got, `reason ${JSON.stringify(c.in)}`).toBeNull();
      }
    }
  });
  it("caseFoldNick matches Go", () => {
    for (const c of fx.casemap) {
      expect(caseFoldNick(c.in), `casefold ${JSON.stringify(c.in)}`).toBe(c.out);
    }
  });
});
