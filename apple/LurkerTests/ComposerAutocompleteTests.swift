import Foundation
import Testing

@testable import Lurker

struct ComposerAutocompleteTests {
  private func member(
    _ nick: String, self isSelf: Bool = false
  ) -> Member {
    Member(nick: nick, prefix: nil, realname: nil, away: false, self: isSelf, color: nil)
  }

  private var members: [Member] {
    [
      member("alice"), member("Albert"), member("bob"),
      member("malice"), member("shrike", self: true),
    ]
  }

  // MARK: Nick completion

  @Test func nickPrefixMatchesBeforeSubstring() {
    let matches = NickCompletion.matches(text: "al", members: members, ownNick: nil)
    #expect(matches == ["alice", "Albert", "malice"])
  }

  @Test func nickExcludesSelfAndOwnNetworkNick() {
    let all = members + [member("Shrike2")]
    let matches = NickCompletion.matches(text: "shr", members: all, ownNick: "Shrike2")
    #expect(matches.isEmpty)
  }

  @Test func nickBailsOnWhitespaceCommandOrEmoji() {
    #expect(NickCompletion.matches(text: "al bob", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: "/al", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: ":al", members: members, ownNick: nil).isEmpty)
    #expect(NickCompletion.matches(text: "", members: members, ownNick: nil).isEmpty)
  }

  @Test func nickCapsResults() {
    let many = (0..<20).map { member("nick\($0)") }
    let matches = NickCompletion.matches(text: "nick", members: many, ownNick: nil)
    #expect(matches.count == NickCompletion.maxResults)
  }

  @Test func nickApplyUsesOriginalCasingWithColonSpace() {
    #expect(NickCompletion.apply(nick: "Albert") == "Albert: ")
  }

  @Test func nickDeduplicatesCaseInsensitively() {
    let dupes = [member("Alice"), member("alice"), member("ALICE")]
    let matches = NickCompletion.matches(text: "ali", members: dupes, ownNick: nil)
    #expect(matches == ["Alice"])
  }

  // MARK: Emoji completion

  private var catalog: EmojiCatalog {
    EmojiCatalog(byName: [
      "smile": "😄", "smirk": "😏", "cosmic": "🌌", "+1": "👍", "north-east": "↗️",
    ])
  }

  @Test func emojiKeywordNeedsColonAtStartOrAfterWhitespace() {
    #expect(EmojiCompletion.keyword(in: ":sm") == "sm")
    #expect(EmojiCompletion.keyword(in: "hello :sm") == "sm")
    #expect(EmojiCompletion.keyword(in: "a:sm") == nil)
    #expect(EmojiCompletion.keyword(in: ":s") == nil)  // min 2 chars
    #expect(EmojiCompletion.keyword(in: ":sm ile") == nil)  // whitespace after keyword
  }

  @Test func emojiKeywordCharset() {
    #expect(EmojiCompletion.keyword(in: ":+1") == "+1")
    #expect(EmojiCompletion.keyword(in: ":north-east") == "north-east")
    #expect(EmojiCompletion.keyword(in: ":no!pe") == nil)
  }

  @Test func emojiPrefixBeforeSubstring() {
    let matches = EmojiCompletion.matches(text: ":sm", catalog: catalog)
    #expect(matches.map(\.name) == ["smile", "smirk", "cosmic"])
  }

  @Test func emojiApplyReplacesTokenWithGlyphNoTrailingSpace() {
    let match = EmojiMatch(name: "smile", emoji: "😄")
    #expect(EmojiCompletion.apply(text: "hello :sm", match: match) == "hello 😄")
    #expect(EmojiCompletion.apply(text: ":sm", match: match) == "😄")
  }

  @Test func bundledCatalogLoads() {
    // The app bundle carries a build-time copy of web/src/emoji-map.json.
    #expect(EmojiCatalog.shared.names.count > 1500)
    #expect(EmojiCatalog.shared.byName["smile"] != nil)
  }

  // MARK: Slash-command popup

  @Test func slashMatchingFiltersByFirstTokenPrefix() {
    #expect(SlashCommands.matching("/").count == SlashCommands.help.count)
    #expect(SlashCommands.matching("/jo").map(\.command) == ["/join"])
    #expect(SlashCommands.matching("/join #chan").map(\.command) == ["/join"])
    #expect(SlashCommands.matching("/xyz").isEmpty)
    #expect(SlashCommands.matching("hello").isEmpty)
  }

  // MARK: Popup priority

  private func channel() -> Buffer {
    Buffer(
      id: UUID(), networkID: UUID(), name: "#chan", kind: "channel", joined: true,
      showEmbeds: true, showPresenceEvents: true, collapsePresenceEvents: false,
      pinned: false, unread: 0, mentions: 0)
  }

  @Test func emojiWinsOverNick() {
    // ":al" is an emoji token AND would prefix-match nicks — emoji wins, and
    // the nick matcher independently refuses ":"-leading text.
    let popup = ComposerPopup.resolve(
      text: ":smi", buffer: channel(), members: members, ownNick: nil)
    guard case .emoji = popup else {
      Issue.record("expected emoji popup, got \(popup)")
      return
    }
  }

  @Test func commandWinsOverNick() {
    let popup = ComposerPopup.resolve(
      text: "/jo", buffer: channel(), members: members, ownNick: nil)
    guard case .command = popup else {
      Issue.record("expected command popup, got \(popup)")
      return
    }
  }

  @Test func nickPopupOnlyInChannels() {
    var query = channel()
    query.kind = "query"
    let inChannel = ComposerPopup.resolve(
      text: "al", buffer: channel(), members: members, ownNick: nil)
    let inQuery = ComposerPopup.resolve(
      text: "al", buffer: query, members: members, ownNick: nil)
    guard case .nick(let nicks) = inChannel else {
      Issue.record("expected nick popup, got \(inChannel)")
      return
    }
    #expect(nicks.first == "alice")
    #expect(inQuery == .none)
  }

  @Test func commandPopupIsNotSelectable() {
    let popup = ComposerPopup.resolve(text: "/", buffer: channel(), members: [], ownNick: nil)
    #expect(popup.selectableCount == 0)
  }
}
