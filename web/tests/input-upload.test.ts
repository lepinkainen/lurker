import { afterEach, describe, expect, it, vi } from "vitest";
import { uploadFile } from "../src/input-upload";

function makeFile(size: number, name = "img.png"): File {
  return new File([new Uint8Array(size)], name, { type: "image/png" });
}

afterEach(() => vi.unstubAllGlobals());

describe("uploadFile client-side dedup precheck", () => {
  it("skips the precheck for small files and uploads directly", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/upload")) {
        return new Response(JSON.stringify({ url: "/uploads/abc.png" }), { status: 201 });
      }
      throw new Error(`unexpected fetch ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(1024)); // < 1 MiB → no precheck
    expect(url).toBe("/uploads/abc.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(false);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(true);
  });

  it("returns the existing url without uploading on a precheck hit", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/media/exists")) {
        return new Response(JSON.stringify({ url: "/uploads/dup.png" }), { status: 200 });
      }
      throw new Error(`should not upload on a dedup hit: ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(2 * 1024 * 1024)); // >= 1 MiB
    expect(url).toBe("/uploads/dup.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(true);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(false);
  });

  it("uploads normally when the precheck misses", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/media/exists")) return new Response("", { status: 404 });
      if (u.includes("/api/upload")) {
        return new Response(JSON.stringify({ url: "/uploads/new.png" }), { status: 201 });
      }
      throw new Error(`unexpected fetch ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(2 * 1024 * 1024));
    expect(url).toBe("/uploads/new.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(true);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(true);
  });
});
