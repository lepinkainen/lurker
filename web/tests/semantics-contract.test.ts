import { describe, expect, it } from "vitest";
import fixture from "../../testdata/semantic-kinds.json";
import type { Message } from "../src/app-state";
import { msgCountsAsUnread, msgDisplayKind, msgIsSelf, msgMentionsMe, PRESENCE_KINDS } from "../src/messages";

// Web half of the Go irc.TestSemanticKindsContract guard. The server computes
// display_kind + is_self/mentions_me/counts_as_unread once and ships them on
// every message (state, history, live); the client must consume them verbatim
// rather than re-deriving from message.kind. PRESENCE_KINDS is the one kind set
// still defined client-side and is kept in lockstep with the shared fixture.

const msg = (p: Partial<Message>): Message => ({ id: "1", buffer_id: "b", ...p });

describe("presence-kind contract", () => {
  it("PRESENCE_KINDS matches the shared cross-language fixture", () => {
    expect([...PRESENCE_KINDS].sort()).toEqual([...fixture.presenceKinds].sort());
  });
});

describe("server-flag consumption (finding 4)", () => {
  it("msgDisplayKind honors server display_kind over raw kind", () => {
    // Raw kind says privmsg; server classified it action. Client must honor
    // the server — re-deriving from kind would reintroduce Go<->TS drift.
    expect(msgDisplayKind(msg({ kind: "privmsg", display_kind: "action" }))).toBe("action");
    expect(msgDisplayKind(msg({ kind: "join", display_kind: "sys" }))).toBe("sys");
  });

  it("msgDisplayKind defaults to message only when display_kind absent", () => {
    expect(msgDisplayKind(msg({ kind: "join" }))).toBe("message");
  });

  it("mention/self/unread read the wire flags, not message.kind", () => {
    const flagged = msg({ kind: "join", mentions_me: true, is_self: true, counts_as_unread: true });
    expect(msgMentionsMe(flagged)).toBe(true);
    expect(msgIsSelf(flagged)).toBe(true);
    expect(msgCountsAsUnread(flagged)).toBe(true);

    // Omitzero false => field absent on the wire => falsy regardless of kind.
    const bare = msg({ kind: "privmsg" });
    expect(msgMentionsMe(bare)).toBe(false);
    expect(msgIsSelf(bare)).toBe(false);
    expect(msgCountsAsUnread(bare)).toBe(false);
  });
});
