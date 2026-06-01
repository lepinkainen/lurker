import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Buffer, type Member, state } from "../src/app-state";
import { type MembersDom, membersForActive, populateMembersForActive, renderMembers } from "../src/members";
import { resetAppState } from "../src/reset";

function buf(overrides: Partial<Buffer> = {}): Buffer {
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

function dom(): MembersDom {
  const memberPaneEl = document.createElement("aside");
  const toggleMembersEl = document.createElement("button");
  const memberCountEl = document.createElement("span");
  const memberCountInlineEl = document.createElement("span");
  const bufferMemcountEl = document.createElement("span");
  bufferMemcountEl.hidden = true;
  const memberListEl = document.createElement("div");
  return {
    memberPaneEl,
    toggleMembersEl,
    memberCountEl,
    memberCountInlineEl,
    bufferMemcountEl,
    memberListEl,
    sendCmd: vi.fn(),
  };
}

describe("membersForActive", () => {
  beforeEach(() => resetAppState());

  it("returns empty list when no active buffer", () => {
    expect(membersForActive()).toEqual([]);
  });

  it("returns stored members for active buffer", () => {
    state.activeId = "5";
    const list: Member[] = [{ nick: "alice", prefix: "@", away: false, self: false }];
    state.members.set("5", list);
    expect(membersForActive()).toBe(list);
  });

  it("returns empty array when active buffer has no members entry", () => {
    state.activeId = "99";
    expect(membersForActive()).toEqual([]);
  });
});

describe("active member population", () => {
  beforeEach(() => resetAppState());

  it("noops on non-channel buffer", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ kind: "query", name: "alice" }));
    populateMembersForActive();
    expect(state.members.has(1)).toBe(false);
  });

  it("noops if members already cached", () => {
    state.activeId = "1";
    state.buffers.set("1", buf());
    state.members.set("1", []);
    state.messages.set("1", [{ id: "1", buffer_id: "1", sender: "bob", content: "hi" }]);
    populateMembersForActive();
    expect(state.members.get("1")).toEqual([]);
  });

  it("derives members from message senders + me", () => {
    state.activeId = "1";
    state.buffers.set("1", buf());
    state.me.nick = "you";
    state.messages.set("1", [
      { id: "1", buffer_id: "1", sender: "alice", content: "hi" },
      { id: "2", buffer_id: "1", sender: "bob", content: "yo" },
      { id: "3", buffer_id: "1", sender: "alice", content: "again" },
      { id: "4", buffer_id: "1", sender: "carol", target: "ex", kind: "kick" },
    ]);
    populateMembersForActive();
    const got = state.members.get("1") || [];
    expect(got.map((m) => m.nick)).toEqual(["alice", "bob", "carol", "ex", "you"]);
    const me = got.find((m) => m.nick === "you");
    expect(me?.self).toBe(true);
    expect(me?.prefix).toBe("+");
  });
});

describe("renderMembers", () => {
  beforeEach(() => resetAppState());

  it("hides pane on non-channel buffer", () => {
    state.activeId = "1";
    state.buffers.set("1", buf({ kind: "query" }));
    const d = dom();
    renderMembers(d);
    expect(d.memberPaneEl.dataset.hidden).toBe("true");
    expect(d.bufferMemcountEl.hidden).toBe(true);
  });

  it("groups members by prefix and renders headings", () => {
    state.activeId = "1";
    state.buffers.set("1", buf());
    state.members.set("1", [
      { nick: "op1", prefix: "@", away: false, self: false },
      { nick: "voice1", prefix: "+", away: false, self: false },
      { nick: "reg1", prefix: "", away: false, self: false },
      { nick: "reg2", prefix: "", away: false, self: false },
    ]);
    const d = dom();
    renderMembers(d);
    const groups = [...d.memberListEl.querySelectorAll(".mgroup")].map((el) => el.textContent);
    expect(groups).toEqual(["ops · 1", "voice · 1", "regulars · 2"]);
    expect(d.memberCountEl.textContent).toBe("4");
    expect(d.memberCountInlineEl.textContent).toBe("4");
    expect(d.bufferMemcountEl.hidden).toBe(false);
  });

  it("hides pane when showMemberList false", () => {
    state.activeId = "1";
    state.buffers.set("1", buf());
    state.members.set("1", [{ nick: "op1", prefix: "@", away: false, self: false }]);
    state.showMemberList = false;
    const d = dom();
    renderMembers(d);
    expect(d.memberPaneEl.dataset.hidden).toBe("true");
    expect(d.memberListEl.children.length).toBe(0);
  });
});
