import Foundation

// MARK: - NickCompletion

/// Composer autocomplete matching, mirroring the web client's
/// nick-autocomplete.ts / emoji-autocomplete.ts / input-command-popup.ts.
///
/// The web logic is caret-driven; SwiftUI's TextField exposes no caret
/// position, so all matching treats the caret as sitting at the end of the
/// text. Every web rule collapses to a string-suffix rule under that policy,
/// and typing happens at the end in practice.
enum NickCompletion {

  // MARK: Internal

  static let maxResults = 12

  /// Nick candidates for the current composer text. Active only for the
  /// first token of a channel message: any whitespace, or a leading `/`
  /// (command) or `:` (emoji), disables it (web parity).
  static func matches(text: String, members: [Member], ownNick: String?) -> [String] {
    guard !text.isEmpty, buffersFirstToken(text) else { return [] }
    let query = text.lowercased()
    var seen = Set<String>()
    var prefixes = [String]()
    var contains = [String]()
    for member in members {
      let nick = member.nick
      guard !nick.isEmpty, member.`self` != true else { continue }
      if let ownNick, nick.caseInsensitiveCompare(ownNick) == .orderedSame {
        continue
      }
      let lowered = nick.lowercased()
      guard seen.insert(lowered).inserted else { continue }
      if lowered.hasPrefix(query) {
        prefixes.append(nick)
      } else if lowered.contains(query) {
        contains.append(nick)
      }
    }
    return Array((prefixes + contains).prefix(maxResults))
  }

  /// Completing the first token always yields `"Nick: "` in the member's
  /// original casing (web parity: replaceNickToken).
  static func apply(nick: String) -> String {
    "\(nick): "
  }

  // MARK: Private

  private static func buffersFirstToken(_ text: String) -> Bool {
    !text.hasPrefix("/") && !text.hasPrefix(":") && !text.contains(where: \.isWhitespace)
  }

}

// MARK: - EmojiMatch

struct EmojiMatch: Equatable, Identifiable {
  let name: String
  let emoji: String

  var id: String {
    name
  }
}

// MARK: - EmojiCompletion

enum EmojiCompletion {

  // MARK: Internal

  static let maxResults = 12
  static let minKeywordLength = 2

  /// The `:keyword` token ending at the caret (end of text), or nil. The
  /// colon must sit at the start of the text or after whitespace, and the
  /// keyword must be ≥2 chars of `[-+\w]` (web parity: tokenAtCursor).
  static func keyword(in text: String) -> String? {
    guard let colon = text.lastIndex(of: ":") else { return nil }
    if colon != text.startIndex {
      let before = text[text.index(before: colon)]
      guard before.isWhitespace else { return nil }
    }
    let keyword = text[text.index(after: colon)...]
    guard keyword.count >= minKeywordLength, keyword.allSatisfy(isKeywordChar) else { return nil }
    return String(keyword)
  }

  static func matches(text: String, catalog: EmojiCatalog = .shared) -> [EmojiMatch] {
    guard let keyword = keyword(in: text)?.lowercased() else { return [] }
    var results = [EmojiMatch]()
    for name in catalog.names where name.hasPrefix(keyword) {
      results.append(EmojiMatch(name: name, emoji: catalog.byName[name]!))
      if results.count == maxResults {
        return results
      }
    }
    for name in catalog.names where !name.hasPrefix(keyword) && name.contains(keyword) {
      results.append(EmojiMatch(name: name, emoji: catalog.byName[name]!))
      if results.count == maxResults {
        break
      }
    }
    return results
  }

  /// Replace the trailing `:keyword` with the emoji glyph itself — no
  /// trailing space (web parity: replaceEmojiToken).
  static func apply(text: String, match: EmojiMatch) -> String {
    guard let colon = text.lastIndex(of: ":") else { return text }
    return text[..<colon] + match.emoji
  }

  // MARK: Private

  private static func isKeywordChar(_ char: Character) -> Bool {
    char == "-" || char == "+" || char == "_" || char.isLetter || char.isNumber
  }

}

// MARK: - EmojiCatalog

/// The bundled emoji table (a build-time copy of web/src/emoji-map.json).
struct EmojiCatalog {
  init(byName: [String: String]) {
    self.byName = byName
    names = byName.keys.sorted()
  }

  static let shared: EmojiCatalog = {
    guard
      let url = Bundle.main.url(forResource: "emoji-map", withExtension: "json"),
      let data = try? Data(contentsOf: url),
      let map = try? JSONDecoder().decode([String: String].self, from: data)
    else {
      return EmojiCatalog(byName: [:])
    }
    return EmojiCatalog(byName: map)
  }()

  let byName: [String: String]
  /// Alphabetical for deterministic prefix-then-substring ordering.
  let names: [String]
}

// MARK: - ComposerPopup

/// Which popup (if any) the composer shows for the current text. Priority is
/// emoji > command > nick (web parity: updateInputPopups in input.ts).
enum ComposerPopup: Equatable {
  case emoji([EmojiMatch])
  case command([SlashCommandHelp])
  case nick([String])
  case none

  // MARK: Internal

  /// Rows the selection can move across; the command popup is display-only.
  var selectableCount: Int {
    switch self {
    case .emoji(let matches): matches.count
    case .nick(let nicks): nicks.count
    case .command,
         .none: 0
    }
  }

  static func resolve(
    text: String,
    buffer: Buffer?,
    members: [Member],
    ownNick: String?,
  ) -> ComposerPopup {
    let emoji = EmojiCompletion.matches(text: text)
    if !emoji.isEmpty {
      return .emoji(emoji)
    }
    if text.hasPrefix("/") {
      let commands = SlashCommands.matching(text)
      return commands.isEmpty ? .none : .command(commands)
    }
    guard let buffer, buffer.kind == "channel" else { return .none }
    let nicks = NickCompletion.matches(text: text, members: members, ownNick: ownNick)
    return nicks.isEmpty ? .none : .nick(nicks)
  }
}
