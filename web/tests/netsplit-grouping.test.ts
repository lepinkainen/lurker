import { describe, expect, it } from "vitest";
import type { Message, NetsplitInfo } from "../src/app-state";
import { groupPresenceRun } from "../src/messages";

// Netsplit clustering is server-side (irc/netsplit_tracker.go +
// api annotateNetsplits); messages arrive annotated with a shared group id.
// The client only collates annotated messages — these tests cover that
// collation.

const ns: NetsplitInfo = {
  id: "a.example.net b.example.net|1718100000000",
  server_a: "a.example.net",
  server_b: "b.example.net",
};

let seq = 0;
const msg = (kind: string, sender: string, netsplit?: NetsplitInfo): Message => ({
  id: `m${++seq}`,
  buffer_id: "b1",
  kind,
  sender,
  netsplit,
});

describe("groupPresenceRun", () => {
  it("returns one plain run for unannotated presence", () => {
    const groups = groupPresenceRun([msg("join", "alice"), msg("part", "bob")]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.kind).toBe("plain");
  });

  it("collates messages sharing a netsplit id into one group", () => {
    const groups = groupPresenceRun([msg("quit", "alice", ns), msg("quit", "bob", ns), msg("join", "alice", ns)]);
    expect(groups).toHaveLength(1);
    const g = groups[0];
    if (g?.kind !== "netsplit") throw new Error("expected netsplit group");
    expect(g.id).toBe(ns.id);
    expect(g.serverA).toBe("a.example.net");
    expect(g.quits.map((m) => m.sender)).toEqual(["alice", "bob"]);
    expect(g.rejoins.map((m) => m.sender)).toEqual(["alice"]);
  });

  it("renders the group at its first member and resumes plain runs around it", () => {
    const groups = groupPresenceRun([
      msg("join", "carol"),
      msg("quit", "alice", ns),
      msg("part", "dave"),
      msg("quit", "bob", ns),
      msg("join", "erin"),
    ]);
    // Later group members fold in without breaking the surrounding plain
    // run, so dave and erin collate into one trailing plain group.
    expect(groups.map((g) => g.kind)).toEqual(["plain", "netsplit", "plain"]);
    const netsplit = groups[1];
    if (netsplit?.kind !== "netsplit") throw new Error("expected netsplit group");
    expect(netsplit.quits).toHaveLength(2);
    const tail = groups[2];
    if (tail?.kind !== "plain") throw new Error("expected plain group");
    expect(tail.items.map((m) => m.sender)).toEqual(["dave", "erin"]);
  });

  it("keeps distinct netsplit ids as separate groups", () => {
    const other: NetsplitInfo = {
      id: "c.example.net d.example.net|1718100099000",
      server_a: "c.example.net",
      server_b: "d.example.net",
    };
    const groups = groupPresenceRun([msg("quit", "alice", ns), msg("quit", "bob", other)]);
    expect(groups).toHaveLength(2);
    expect(groups.every((g) => g.kind === "netsplit")).toBe(true);
  });

  it("handles empty input", () => {
    expect(groupPresenceRun([])).toEqual([]);
  });
});
