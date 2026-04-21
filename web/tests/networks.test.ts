import { describe, expect, it } from "vitest";

type Network = { id: number; name: string; sort_order?: number };

async function fetchNetworks(): Promise<Network[]> {
  const res = await fetch("/api/state");
  expect(res.ok).toBe(true);
  const s = await res.json();
  return s.networks as Network[];
}

describe("network reorder", () => {
  it("persists reversed sort order", async () => {
    const original = await fetchNetworks();
    const ids = [...original].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0) || a.id - b.id).map((n) => n.id);
    if (ids.length < 2) return;

    const reversed = [...ids].reverse();
    const res = await fetch("/api/networks/reorder", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ids: reversed }),
    });
    expect(res.ok).toBe(true);

    const after = await fetchNetworks();
    const afterIds = [...after]
      .sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0) || a.id - b.id)
      .map((n) => n.id);
    expect(afterIds).toEqual(reversed);

    // restore
    const restore = await fetch("/api/networks/reorder", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ids }),
    });
    expect(restore.ok).toBe(true);
  });
});

describe("service endpoints", () => {
  it("/healthz returns 200", async () => {
    const res = await fetch("/healthz");
    expect(res.ok).toBe(true);
  });

  it("/whoami returns build metadata", async () => {
    const res = await fetch("/whoami");
    expect(res.ok).toBe(true);
    const body = await res.json();
    expect(typeof body.name).toBe("string");
    expect(body.name.length).toBeGreaterThan(0);
    expect(body).toHaveProperty("version");
    expect(body).toHaveProperty("hash");
    expect(body).toHaveProperty("build_time");
  });
});
