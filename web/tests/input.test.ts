import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, state } from "../src/app-state";
import { handleSlashCommand, updateCmdPop, updateInputEnabled } from "../src/input";
import { resetAppState } from "../src/reset";

function makeBuffer(overrides: Partial<Buffer> = {}): Buffer {
  return {
    id: 1,
    network_id: 10,
    name: "#chan",
    kind: "channel",
    joined: true,
    unread: 0,
    mentions: 0,
    ...overrides,
  };
}

describe("handleSlashCommand", () => {
  beforeEach(() => resetAppState());

  it("emits join with channel arg", () => {
    const send = vi.fn();
    const buffer = makeBuffer();
    expect(handleSlashCommand("/join #foo", buffer, send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "join", network_id: 10, channel: "#foo" });
  });

  it("emits part with reason", () => {
    const send = vi.fn();
    const buffer = makeBuffer({ id: 7 });
    expect(handleSlashCommand("/part bye now", buffer, send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "part", buffer_id: 7, content: "bye now" });
  });

  it("emits part with empty reason", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/part", makeBuffer({ id: 7 }), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "part", buffer_id: 7, content: "" });
  });

  it("returns false for unknown command", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/xyz nope", makeBuffer(), send)).toBe(false);
    expect(send).not.toHaveBeenCalled();
  });

  it("is case-insensitive on command name", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/JOIN #x", makeBuffer(), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "join", network_id: 10, channel: "#x" });
  });
});

describe("updateInputEnabled", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  function el() {
    const i = document.createElement("input");
    i.disabled = true;
    return i;
  }

  it("disables input when ws not ready", () => {
    state.wsReady = false;
    state.activeId = 1;
    state.buffers.set(1, makeBuffer());
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("disables input on status buffer", () => {
    state.wsReady = true;
    state.activeId = 1;
    state.buffers.set(1, makeBuffer({ kind: "status" }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("disables input on parted channel", () => {
    state.wsReady = true;
    state.activeId = 1;
    state.buffers.set(1, makeBuffer({ joined: false }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("enables input on joined channel with ws ready", () => {
    state.wsReady = true;
    state.activeId = 1;
    state.buffers.set(1, makeBuffer({ joined: true }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(false);
  });

  it("enables input on query buffer", () => {
    state.wsReady = true;
    state.activeId = 1;
    state.buffers.set(1, makeBuffer({ kind: "query" }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(false);
  });
});

describe("updateCmdPop", () => {
  function setup(value: string) {
    const input = document.createElement("input");
    input.value = value;
    const pop = document.createElement("div");
    pop.hidden = true;
    return { input, pop };
  }

  it("hides when value lacks leading slash", () => {
    const { input, pop } = setup("hello");
    pop.hidden = false;
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("hides when no command matches prefix", () => {
    const { input, pop } = setup("/zzz");
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("shows matching commands and highlights first", () => {
    const { input, pop } = setup("/j");
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(false);
    const rows = pop.querySelectorAll(".ci");
    expect(rows.length).toBeGreaterThan(0);
    expect(rows[0].classList.contains("hl")).toBe(true);
    expect(rows[0].querySelector(".c")?.textContent).toContain("/join");
  });

  it("shows all commands for bare slash", () => {
    const { input, pop } = setup("/");
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(false);
    expect(pop.querySelectorAll(".ci").length).toBeGreaterThanOrEqual(10);
  });
});
