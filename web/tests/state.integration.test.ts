// Integration test: depends on the seedtest fixture (`task seed-test`) and the
// running backend started by tests/globalSetup.ts. Asserts against seeded
// networks, buffers, and messages — drift in the seed will break this.
import { describe, expect, it } from "vitest";

describe("seeded backend", () => {
  it("exposes the seeded networks via /api/state", async () => {
    const res = await fetch("/api/state");
    expect(res.ok).toBe(true);

    const state = await res.json();
    const names = state.networks.map((n: { name: string }) => n.name).sort();
    expect(names).toEqual(["libera", "oftc"]);
    expect(state.networks.map((n: { status: string }) => n.status)).toEqual(["connected", "connected"]);

    const liberaBuffers = state.buffers
      .filter(
        (b: { network_id: string }) =>
          b.network_id === state.networks.find((n: { name: string }) => n.name === "libera").id,
      )
      .map((b: { name: string }) => b.name)
      .sort();
    expect(liberaBuffers).toContain("#lurker");
    expect(liberaBuffers).toContain("#go-nuts");
    expect(liberaBuffers).toContain("alice");

    const lurker = state.buffers.find((b: { name: string }) => b.name === "#lurker");
    expect(lurker.joined).toBe(true);
    expect(state.members[String(lurker.id)].map((m: { nick: string }) => m.nick)).toEqual([
      "alice",
      "bob",
      "carol",
      "lurkertest",
    ]);
  });

  it("returns seeded messages for the #lurker buffer", async () => {
    const state = await (await fetch("/api/state")).json();
    const lurker = state.buffers.find((b: { name: string }) => b.name === "#lurker");
    expect(lurker).toBeTruthy();

    const msgs = state.initial_messages[String(lurker.id)] ?? [];
    const contents = msgs.map((m: { content: string }) => m.content);
    expect(contents).toContain("morning folks");
    expect(contents.length).toBeGreaterThanOrEqual(5);
  });
});
