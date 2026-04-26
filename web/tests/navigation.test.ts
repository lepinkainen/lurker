import { afterEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Network, state } from "../src/app-state";
import { onHashChange } from "../src/navigation";

function network(overrides: Partial<Network> = {}): Network {
  return { id: 1, name: "Libera", ...overrides };
}

function buffer(overrides: Partial<Buffer> = {}): Buffer {
  return { id: 1, network_id: 1, name: "#lurker", kind: "channel", unread: 0, mentions: 0, ...overrides };
}

describe("navigation.onHashChange", () => {
  afterEach(() => {
    state.networks.clear();
    state.buffers.clear();
    state.activeId = null;
    location.hash = "";
  });

  it("calls setActive with skipHash when hash points to a different buffer", () => {
    state.networks.set(1, network());
    const target = buffer({ id: 7, name: "#general" });
    state.buffers.set(target.id, target);
    state.activeId = 99;
    location.hash = "#net/Libera/#general";
    const setActive = vi.fn();

    onHashChange(setActive);

    expect(setActive).toHaveBeenCalledWith(7, { skipHash: true });
  });

  it("does not call setActive when hash matches the active buffer", () => {
    state.networks.set(1, network());
    const target = buffer({ id: 7, name: "#general" });
    state.buffers.set(target.id, target);
    state.activeId = 7;
    location.hash = "#net/Libera/#general";
    const setActive = vi.fn();

    onHashChange(setActive);

    expect(setActive).not.toHaveBeenCalled();
  });

  it("does not call setActive when hash matches no buffer", () => {
    state.networks.set(1, network());
    state.activeId = 1;
    location.hash = "#net/Libera/#nope";
    const setActive = vi.fn();

    onHashChange(setActive);

    expect(setActive).not.toHaveBeenCalled();
  });
});
