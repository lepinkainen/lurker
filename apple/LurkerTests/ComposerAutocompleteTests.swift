import Foundation
import Testing

@testable import Lurker

struct ComposerAutocompleteTests {

  // MARK: Internal

  @Test
  func `nick prefix matches before substring`() {
    let matches = NickCompletion.matches(text: "al", members: members, ownNick: nil)
    #expect(matches == ["alice", "Albert", "malice"])
  }

  @Test
  func `nick excludes self and own network nick`() {
    let all = members + [member("Shrike2")]
    let matches = NickCompletion.matches(text: "shr", members: all, ownNick: "Shrike2")
    #expect(matches.isEmpty)
  }

  @Test
  func `nick bails on whitespace command or emoji`() {
    #expect(NickCompletion.matches(text: "al bob", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: "/al", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: ":al", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: "", members: members, ownNick: nil).isEmpty)
  }

  @Test
  func `nick caps results`() {
    let many = (0..<20).map { member("nick\($0)") }
    let matches = NickCompletion.matches(text: "nick", members: many, ownNick: nil)
    #expect(matches.count == NickCompletion.maxResults)
  }

  @Test
  func `nick apply uses original casing with colon space`() {
    #expect(NickCompletion.apply(nick: "Albert") == "Albert: ")
  }

  @Test
  func `nick deduplicates case insensitively`() {
    let dupes = [member("Alice"), member("alice"), member("ALICE")]
    let matches = NickCompletion.matches(text: "ali", members: dupes, ownNick: nil)
    #expect(matches == ["Alice"])
  }

  @Test
  func `emoji keyword needs colon at start or after whitespace`() {
    #expect(EmojiCompletion.keyword(in: ":sm") == "sm")
    #expect(EmojiCompletion.keyword(in: "hello :sm") == "sm")
    #expect(EmojiCompletion.keyword(in: "a:sm") == nil)
    #expect(EmojiCompletion.keyword(in: ":s") == nil) // min 2 chars
    #expect(EmojiCompletion.keyword(in: ":sm ile") == nil) // whitespace after keyword
  }

  @Test
  func `emoji keyword charset`() {
    #expect(EmojiCompletion.keyword(in: ":+1") == "+1")
    #expect(EmojiCompletion.keyword(in: ":north-east") == "north-east")
    #expect(EmojiCompletion.keyword(in: ":no!pe") == nil)
  }

  @Test
  func `emoji prefix before substring`() {
    let matches = EmojiCompletion.matches(text: ":sm", catalog: catalog)
    #expect(matches.map(\.name) == ["smile", "smirk", "cosmic"])
  }

  @Test
  func `emoji apply replaces token with glyph no trailing space`() {
    let match = EmojiMatch(name: "smile", emoji: "😄")
    #expect(EmojiCompletion.apply(text: "hello :sm", match: match) == "hello 😄")
    #expect(EmojiCompletion.apply(text: ":sm", match: match) == "😄")
  }

  @Test
  func `bundled catalog loads`() {
    // The app bundle carries a build-time copy of web/src/emoji-map.json.
    #expect(EmojiCatalog.shared.names.count > 1500)
    #expect(EmojiCatalog.shared.byName["smile"] != nil)
  }

  @Test
  func `slash matching filters by first token prefix`() {
    #expect(SlashCommands.matching("/").count == SlashCommands.help.count)
    #expect(SlashCommands.matching("/jo").map(\.command) == ["/join"])
    #expect(SlashCommands.matching("/join #chan").map(\.command) == ["/join"])
    #expect(SlashCommands.matching("/xyz").isEmpty)
    #expect(SlashCommands.matching("hello").isEmpty)
  }

  @Test
  func `emoji wins over nick`() {
    // ":al" is an emoji token AND would prefix-match nicks — emoji wins, and
    // the nick matcher independently refuses ":"-leading text.
    let popup = ComposerPopup.resolve(
      text: ":smi",
      buffer: channel(),
      members: members,
      ownNick: nil,
    )
    guard case .emoji = popup else {
      Issue.record("expected emoji popup, got \(popup)")
      return
    }
  }

  @Test
  func `command wins over nick`() {
    let popup = ComposerPopup.resolve(
      text: "/jo",
      buffer: channel(),
      members: members,
      ownNick: nil,
    )
    guard case .command = popup else {
      Issue.record("expected command popup, got \(popup)")
      return
    }
  }

  @Test
  func `nick popup only in channels`() {
    var query = channel()
    query.kind = "query"
    let inChannel = ComposerPopup.resolve(
      text: "al",
      buffer: channel(),
      members: members,
      ownNick: nil,
    )
    let inQuery = ComposerPopup.resolve(
      text: "al",
      buffer: query,
      members: members,
      ownNick: nil,
    )
    guard case .nick(let nicks) = inChannel else {
      Issue.record("expected nick popup, got \(inChannel)")
      return
    }
    #expect(nicks.first == "alice")
    #expect(inQuery == .none)
  }

  @Test
  func `command popup is not selectable`() {
    let popup = ComposerPopup.resolve(text: "/", buffer: channel(), members: [], ownNick: nil)
    #expect(popup.selectableCount == 0)
  }

  // MARK: Private

  private var members: [Member] {
    [
      member("alice"),
      member("Albert"),
      member("bob"),
      member("malice"),
      member("shrike", self: true),
    ]
  }

  private var catalog: EmojiCatalog {
    EmojiCatalog(byName: [
      "smile": "😄",
      "smirk": "😏",
      "cosmic": "🌌",
      "+1": "👍",
      "north-east": "↗️",
    ])
  }

  private func member(
    _ nick: String,
    self isSelf: Bool = false,
  ) -> Member {
    Member(nick: nick, prefix: nil, realname: nil, away: false, self: isSelf, color: nil)
  }

  private func channel() -> Buffer {
    Buffer(
      id: UUID(),
      networkID: UUID(),
      name: "#chan",
      kind: "channel",
      joined: true,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      unread: 0,
      mentions: 0,
    )
  }

}
