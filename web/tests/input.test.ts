import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, state } from "../src/app-state";
import {
  getEmojiAutocompleteState,
  handleEmojiKey,
  initEmojiPop,
  resetEmojiAutocomplete,
  updateEmojiPop,
} from "../src/emoji-autocomplete";
import { bindInputHandlers, handleFormatKey, updateInputEnabled } from "../src/input";
import { updateCmdPop } from "../src/input-command-popup";
import { handleHistoryKey, recordSentInput, restoreInputDraft, saveInputDraft } from "../src/input-history";
import { insertTextAtCursor, uploadFile } from "../src/input-upload";
import { resetAppState } from "../src/reset";
import { handleSlashCommand } from "../src/slash-commands";

afterEach(() => {
  vi.unstubAllGlobals();
});

function makeBuffer(overrides: Partial<Buffer> = {}): Buffer {
  return {
    id: "1",
    network_id: "10",
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
    expect(send).toHaveBeenCalledWith({ type: "join", network_id: "10", channel: "#foo" });
  });

  it("emits part with reason", () => {
    const send = vi.fn();
    const buffer = makeBuffer({ id: "7" });
    expect(handleSlashCommand("/part bye now", buffer, send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "part", buffer_id: "7", content: "bye now" });
  });

  it("emits part with empty reason", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/part", makeBuffer({ id: "7" }), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "part", buffer_id: "7", content: "" });
  });

  it("returns false for unknown command", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/xyz nope", makeBuffer(), send)).toBe(false);
    expect(send).not.toHaveBeenCalled();
  });

  it("is case-insensitive on command name", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/JOIN #x", makeBuffer(), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "join", network_id: "10", channel: "#x" });
  });

  it("supports alias commands from the same registry", () => {
    const send = vi.fn();
    expect(handleSlashCommand("/cycle", makeBuffer({ id: "7" }), send)).toBe(true);
    expect(send).toHaveBeenCalledWith({ type: "rejoin", buffer_id: "7" });
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
    state.activeId = "1";
    state.buffers.set("1", makeBuffer());
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("disables input on status buffer", () => {
    state.wsReady = true;
    state.activeId = "1";
    state.buffers.set("1", makeBuffer({ kind: "status" }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("disables input on parted channel", () => {
    state.wsReady = true;
    state.activeId = "1";
    state.buffers.set("1", makeBuffer({ joined: false }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(true);
  });

  it("enables input on joined channel with ws ready", () => {
    state.wsReady = true;
    state.activeId = "1";
    state.buffers.set("1", makeBuffer({ joined: true }));
    const i = el();
    updateInputEnabled(i);
    expect(i.disabled).toBe(false);
  });

  it("enables input on query buffer", () => {
    state.wsReady = true;
    state.activeId = "1";
    state.buffers.set("1", makeBuffer({ kind: "query" }));
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

describe("emoji autocomplete", () => {
  beforeEach(() => resetEmojiAutocomplete());
  afterEach(() => resetEmojiAutocomplete());

  function setup(value: string, caret = value.length) {
    const input = document.createElement("input");
    input.value = value;
    input.setSelectionRange(caret, caret);
    const pop = document.createElement("div");
    pop.hidden = true;
    initEmojiPop(input, pop);
    return { input, pop };
  }

  function keyEvent(key: string) {
    return new KeyboardEvent("keydown", { key, cancelable: true });
  }

  it("shows popup for :sm at cursor", () => {
    const { input, pop } = setup(":sm");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(false);
    const rows = pop.querySelectorAll(".ei");
    expect(rows.length).toBeGreaterThan(0);
    expect(rows[0].classList.contains("hl")).toBe(true);
  });

  it("hides popup for bare colon", () => {
    const { input, pop } = setup(":");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("hides popup for single char after colon", () => {
    const { input, pop } = setup(":s");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("hides popup when no emoji matches", () => {
    const { input, pop } = setup(":zzzzzzz");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("shows popup for :keyword mid-text at cursor", () => {
    const { input, pop } = setup("hello :smi world", 10);
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(false);
  });

  it("hides popup when cursor not at :keyword", () => {
    const { input, pop } = setup("hello :smile", 2);
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("hides popup when cursor is after the token and trailing text", () => {
    const { input, pop } = setup(":sm hello");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
  });

  it("does not accept a stale visible popup after cursor leaves the token", () => {
    const { input, pop } = setup(":sm hello", 3);
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(false);

    input.setSelectionRange(input.value.length, input.value.length);
    const ev = keyEvent("Enter");
    expect(handleEmojiKey(ev, input, pop)).toBe(false);
    expect(input.value).toBe(":sm hello");
    expect(pop.hidden).toBe(true);
    expect(ev.defaultPrevented).toBe(false);
  });

  it("inserts emoji on Enter", () => {
    const { input, pop } = setup(":smile");
    updateEmojiPop(input, pop);
    const ev = keyEvent("Enter");
    handleEmojiKey(ev, input, pop);
    expect(input.value).not.toContain(":");
    expect(input.value.length).toBeGreaterThan(0);
    expect(pop.hidden).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("inserts emoji on Tab", () => {
    const { input, pop } = setup(":smile");
    updateEmojiPop(input, pop);
    const ev = keyEvent("Tab");
    handleEmojiKey(ev, input, pop);
    expect(input.value).not.toContain(":");
    expect(pop.hidden).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("dismisses popup on Escape", () => {
    const { input, pop } = setup(":smile");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(false);
    const ev = keyEvent("Escape");
    handleEmojiKey(ev, input, pop);
    expect(pop.hidden).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("navigates selection with ArrowDown and ArrowUp", () => {
    const { input, pop } = setup(":sm");
    updateEmojiPop(input, pop);
    const state0 = getEmojiAutocompleteState();
    expect(state0.selected).toBe(0);

    const down = keyEvent("ArrowDown");
    handleEmojiKey(down, input, pop);
    expect(getEmojiAutocompleteState().selected).toBe(1);
    expect(down.defaultPrevented).toBe(true);

    const up = keyEvent("ArrowUp");
    handleEmojiKey(up, input, pop);
    expect(getEmojiAutocompleteState().selected).toBe(0);
    expect(up.defaultPrevented).toBe(true);
  });

  it("replaces token in middle of text preserving surrounding text", () => {
    const { input, pop } = setup("before :smile after", 13);
    updateEmojiPop(input, pop);
    const ev = keyEvent("Enter");
    handleEmojiKey(ev, input, pop);
    // :smile maps to 😄 (U+1F604), a 2-code-unit surrogate pair
    expect(input.value).not.toContain(":smile");
    expect(input.value.length).toBeGreaterThan(12);
    expect(input.value.startsWith("before ")).toBe(true);
  });

  it("replaces the full emoji token when caret is inside the token", () => {
    const { input, pop } = setup("before :smile after", 10);
    updateEmojiPop(input, pop);
    const ev = keyEvent("Enter");
    handleEmojiKey(ev, input, pop);
    expect(input.value.startsWith("before ")).toBe(true);
    expect(input.value.endsWith(" after")).toBe(true);
    expect(input.value).not.toContain(":smile");
    expect(input.value).not.toContain("ile after");
  });

  it("does not handle keys when popup hidden", () => {
    const { input, pop } = setup("hello");
    updateEmojiPop(input, pop);
    expect(pop.hidden).toBe(true);
    expect(handleEmojiKey(keyEvent("Enter"), input, pop)).toBe(false);
    expect(handleEmojiKey(keyEvent("ArrowDown"), input, pop)).toBe(false);
  });

  it("hides slash command popup when emoji popup is active", () => {
    const input = document.createElement("input");
    const form = document.createElement("form");
    const uploadInput = document.createElement("input");
    const uploadButton = document.createElement("button");
    const cmdPop = document.createElement("div");
    const emojiPop = document.createElement("div");
    cmdPop.hidden = true;
    emojiPop.hidden = true;
    bindInputHandlers({
      inputEl: input,
      inputForm: form,
      uploadInputEl: uploadInput,
      uploadButtonEl: uploadButton,
      cmdPopEl: cmdPop,
      emojiPopEl: emojiPop,
      getActiveBuffer: () => makeBuffer(),
      sendCmd: vi.fn(),
    });

    input.value = "/me :sm";
    input.setSelectionRange(input.value.length, input.value.length);
    input.dispatchEvent(new Event("input"));

    expect(emojiPop.hidden).toBe(false);
    expect(cmdPop.hidden).toBe(true);
  });
});
