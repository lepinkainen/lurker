import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, state } from "../src/app-state";
import {
  handleFormatKey,
  handleHistoryKey,
  insertTextAtCursor,
  recordSentInput,
  restoreInputDraft,
  saveInputDraft,
  updateCmdPop,
  updateInputEnabled,
  uploadFile,
} from "../src/input";
import { resetAppState } from "../src/reset";
import { handleSlashCommand } from "../src/slash-commands";

afterEach(() => {
  vi.unstubAllGlobals();
});

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

  it("supports alias commands from the same registry", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/cycle", makeBuffer({ id: 7 }), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "rejoin", buffer_id: 7 });
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

  it("shows alias commands in autocomplete", () => {
    const { input, pop } = setup("/cy");
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(false);
    expect(pop.querySelector(".c")?.textContent).toContain("/cycle");
  });

  it("shows all commands for bare slash", () => {
    const { input, pop } = setup("/");
    updateCmdPop(input, pop);
    expect(pop.hidden).toBe(false);
    expect(pop.querySelectorAll(".ci").length).toBeGreaterThanOrEqual(10);
  });
});

describe("insertTextAtCursor", () => {
  it("inserts spaces around uploaded URL when needed", () => {
    const i = document.createElement("input");
    i.value = "hello world";
    i.setSelectionRange(5, 5);
    insertTextAtCursor(i, "https://files.test/x.png");
    expect(i.value).toBe("hello https://files.test/x.png world");
  });

  it("replaces selection without adding duplicate spaces", () => {
    const i = document.createElement("input");
    i.value = "hello there";
    i.setSelectionRange(6, 11);
    insertTextAtCursor(i, "https://files.test/x.png");
    expect(i.value).toBe("hello https://files.test/x.png");
  });
});

describe("input history", () => {
  beforeEach(() => resetAppState());
  afterEach(() => resetAppState());

  function keyEvent(key: string) {
    return new KeyboardEvent("keydown", { key, cancelable: true });
  }

  it("cycles sent messages per buffer and restores current draft", () => {
    recordSentInput(1, "first");
    recordSentInput(1, "second");
    const input = document.createElement("input");
    const pop = document.createElement("div");
    input.value = "draft";

    const up1 = keyEvent("ArrowUp");
    expect(handleHistoryKey(up1, input, pop, 1)).toBe(true);
    expect(input.value).toBe("second");
    expect(up1.defaultPrevented).toBe(true);

    const up2 = keyEvent("ArrowUp");
    expect(handleHistoryKey(up2, input, pop, 1)).toBe(true);
    expect(input.value).toBe("first");

    const down1 = keyEvent("ArrowDown");
    expect(handleHistoryKey(down1, input, pop, 1)).toBe(true);
    expect(input.value).toBe("second");

    const down2 = keyEvent("ArrowDown");
    expect(handleHistoryKey(down2, input, pop, 1)).toBe(true);
    expect(input.value).toBe("draft");
  });

  it("keeps history isolated per buffer", () => {
    recordSentInput(1, "alpha");
    recordSentInput(2, "beta");
    const input = document.createElement("input");
    const pop = document.createElement("div");

    expect(handleHistoryKey(keyEvent("ArrowUp"), input, pop, 2)).toBe(true);
    expect(input.value).toBe("beta");
    expect(handleHistoryKey(keyEvent("ArrowDown"), input, pop, 2)).toBe(true);

    expect(handleHistoryKey(keyEvent("ArrowUp"), input, pop, 1)).toBe(true);
    expect(input.value).toBe("alpha");
  });

  it("saves and restores drafts per buffer", () => {
    saveInputDraft(1, "hello");
    saveInputDraft(2, "world");
    const input = document.createElement("input");

    restoreInputDraft(input, 1);
    expect(input.value).toBe("hello");
    restoreInputDraft(input, 2);
    expect(input.value).toBe("world");
  });
});

describe("uploadFile", () => {
  it("posts multipart data and returns the uploaded URL", async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      expect(init?.method).toBe("POST");
      expect(init?.body).toBeInstanceOf(FormData);
      return new Response(JSON.stringify({ url: "/uploads/test.txt" }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(uploadFile(new File(["hello"], "test.txt", { type: "text/plain" }))).resolves.toBe(
      "/uploads/test.txt",
    );
  });

  it("throws backend error text on failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("upload too large", { status: 413, headers: { "Content-Type": "text/plain" } })),
    );

    await expect(uploadFile(new File(["oops"], "big.bin"))).rejects.toThrow("upload too large");
  });
});

describe("handleFormatKey", () => {
  function makeInput(value = "", caret = value.length) {
    const i = document.createElement("input");
    i.value = value;
    i.setSelectionRange(caret, caret);
    return i;
  }

  function keyEvent(key: string, mods: { ctrl?: boolean; meta?: boolean; alt?: boolean; shift?: boolean } = {}) {
    return new KeyboardEvent("keydown", {
      key,
      ctrlKey: mods.ctrl ?? false,
      metaKey: mods.meta ?? false,
      altKey: mods.alt ?? false,
      shiftKey: mods.shift ?? false,
      cancelable: true,
    });
  }

  it("inserts bold byte at caret on Ctrl+B", () => {
    const i = makeInput("hello", 2);
    const ev = keyEvent("b", { ctrl: true });
    expect(handleFormatKey(ev, i)).toBe(true);
    expect(i.value).toBe("he\x02llo");
    expect(i.selectionStart).toBe(3);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("inserts reset byte on Ctrl+O", () => {
    const i = makeInput("");
    expect(handleFormatKey(keyEvent("o", { ctrl: true }), i)).toBe(true);
    expect(i.value).toBe("\x0f");
  });

  it("supports meta key on macOS", () => {
    const i = makeInput("");
    expect(handleFormatKey(keyEvent("i", { meta: true }), i)).toBe(true);
    expect(i.value).toBe("\x1d");
  });

  it("ignores plain letter without modifier", () => {
    const i = makeInput("abc", 3);
    const ev = keyEvent("b");
    expect(handleFormatKey(ev, i)).toBe(false);
    expect(i.value).toBe("abc");
    expect(ev.defaultPrevented).toBe(false);
  });

  it("ignores Ctrl+Shift combinations", () => {
    const i = makeInput("");
    expect(handleFormatKey(keyEvent("b", { ctrl: true, shift: true }), i)).toBe(false);
  });

  it("ignores unmapped Ctrl shortcuts", () => {
    const i = makeInput("");
    expect(handleFormatKey(keyEvent("a", { ctrl: true }), i)).toBe(false);
  });

  it("replaces selection with format byte", () => {
    const i = document.createElement("input");
    i.value = "abcdef";
    i.setSelectionRange(2, 5);
    expect(handleFormatKey(keyEvent("u", { ctrl: true }), i)).toBe(true);
    expect(i.value).toBe("ab\x1ff");
    expect(i.selectionStart).toBe(3);
  });
});
