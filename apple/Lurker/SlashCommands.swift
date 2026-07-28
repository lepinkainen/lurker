import Foundation

enum ComposerResult: Equatable, Sendable {
  case command(ClientCommand)
  case invalid(String)
}

enum SlashCommands {
  static let help: [(command: String, arguments: String, description: String)] = [
    ("/me", "<action>", "Send an action"),
    ("/join", "<channel>", "Join a channel"),
    ("/part", "[reason]", "Leave this channel"),
    ("/query", "<nick>", "Open a conversation"),
    ("/msg", "<nick> <message>", "Send a direct message"),
    ("/nick", "<new nick>", "Change your nickname"),
    ("/whois", "<nick>", "Look up a user"),
    ("/away", "[message]", "Set away status"),
    ("/back", "", "Clear away status"),
    ("/topic", "[topic]", "Set the channel topic"),
  ]

  static func parse(_ input: String, buffer: Buffer) -> ComposerResult {
    let value = input.trimmingCharacters(in: .whitespacesAndNewlines)
    guard value.hasPrefix("/") else {
      return .command(ClientCommand(type: "send", bufferID: buffer.id, content: value))
    }
    let pieces = value.dropFirst().split(whereSeparator: \.isWhitespace).map(String.init)
    guard let name = pieces.first?.lowercased() else {
      return .invalid("Enter a command.")
    }
    let rest = Array(pieces.dropFirst())
    let content = rest.joined(separator: " ")
    switch name {
    case "me":
      return require(content, usage: "/me <action>") {
        ClientCommand(type: "me", bufferID: buffer.id, content: content)
      }
    case "join":
      return require(content, usage: "/join <channel>") {
        ClientCommand(type: "join", networkID: buffer.networkID, channel: content)
      }
    case "part":
      return .command(ClientCommand(type: "part", bufferID: buffer.id, content: content))
    case "query":
      return require(content, usage: "/query <nick>") {
        ClientCommand(type: "query", networkID: buffer.networkID, target: content)
      }
    case "msg":
      guard let target = rest.first, rest.count > 1 else {
        return .invalid("Usage: /msg <nick> <message>")
      }
      return .command(
        ClientCommand(
          type: "msg",
          networkID: buffer.networkID,
          target: target,
          content: rest.dropFirst().joined(separator: " ")
        ))
    case "nick":
      return require(content, usage: "/nick <new nick>") {
        ClientCommand(type: "nick", networkID: buffer.networkID, content: content)
      }
    case "whois":
      return require(content, usage: "/whois <nick>") {
        ClientCommand(type: "whois", networkID: buffer.networkID, target: content)
      }
    case "away":
      return .command(ClientCommand(type: "away", networkID: buffer.networkID, content: content))
    case "back":
      return .command(ClientCommand(type: "back", networkID: buffer.networkID))
    case "topic":
      return .command(ClientCommand(type: "topic", bufferID: buffer.id, content: content))
    default:
      return .invalid("Unknown command /\(name)")
    }
  }

  private static func require(
    _ value: String,
    usage: String,
    command: () -> ClientCommand
  ) -> ComposerResult {
    value.isEmpty ? .invalid("Usage: \(usage)") : .command(command())
  }
}
