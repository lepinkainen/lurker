import { afterEach, describe, expect, it } from "vitest";
import { type Buffer, type Network, state } from "../src/app-state";
import { bufferFromHash, bufferHashFor } from "../src/router";

function network(overrides: Partial<Network> = {}): Network {
  return {
    id: "1",
    name: "Libera",
    ...overrides,
  };
}

function buffer(overrides: Partial<Buffer> = {}): Buffer {
  return {
    id: "1",
    network_id: "1",
    name: "#lurker",
    kind: "channel",
    unread: 0,
    mentions: 0,
    ...overrides,
  };
}

describe("router", () => {
  afterEach(() => {
    state.networks.clear();
    state.buffers.clear();
  });

  it("builds a status hash from the network name", () => {
    state.networks.set("1", network());

    expect(bufferHashFor(buffer({ kind: "status", name: "status" }))).toBe("#net/Libera");
  });

  it("builds a channel hash and preserves # in the path", () => {
    state.networks.set("1", network({ name: "My Net" }));

    expect(bufferHashFor(buffer({ name: "#general" }))).toBe("#net/My%20Net/#general");
  });

  it("returns empty hash when the network is missing", () => {
    expect(bufferHashFor(buffer())).toBe("");
  });

  it("finds the status buffer from a network-only hash", () => {
    state.networks.set("1", network());
    const status = buffer({ id: "2", kind: "status", name: "status" });
    state.buffers.set(status.id, status);

    expect(bufferFromHash("#net/Libera")).toBe(status);
  });

  it("finds a named buffer from the hash", () => {
    state.networks.set("1", network({ name: "My Net" }));
    const chan = buffer({ id: "3", name: "#general/chat" });
    state.buffers.set(chan.id, chan);

    expect(bufferFromHash("#net/My%20Net/#general%2Fchat")).toBe(chan);
  });

  it("matches buffer names case-insensitively", () => {
    state.networks.set("1", network());
    const chan = buffer({ id: "4", name: "#Lurker" });
    state.buffers.set(chan.id, chan);

    expect(bufferFromHash("#net/Libera/#lurker")).toBe(chan);
    expect(bufferFromHash("#net/Libera/#LURKER")).toBe(chan);
  });

  it("returns null for unknown network or malformed hash", () => {
    state.networks.set("1", network());
    state.buffers.set("1", buffer());

    expect(bufferFromHash("#wat")).toBeNull();
    expect(bufferFromHash("#net/")).toBeNull();
    expect(bufferFromHash("#net/Unknown/#lurker")).toBeNull();
  });
});
