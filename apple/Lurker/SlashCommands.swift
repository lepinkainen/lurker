import Foundation

// MARK: - ComposerResult

enum ComposerResult: Equatable, Sendable {
  case command(ClientCommand)
  case invalid(String)
}

// MARK: - SlashCommandHelp

struct SlashCommandHelp: Equatable, Identifiable, Sendable {
  let command: String
  let arguments: String
  let description: String

  var id: String {
    command
  }
}

// MARK: - SlashCommands

enum SlashCommands {

  // MARK: Internal

  static let help: [SlashCommandHelp] = [
    .init(command: "/me", arguments: "<action>", description: "Send an action"),
    .init(command: "/join", arguments: "<channel>", description: "Join a channel"),
    .init(command: "/part", arguments: "[reason]", description: "Leave this channel"),
    .init(command: "/query", arguments: "<nick>", description: "Open a conversation"),
    .init(command: "/msg", arguments: "<nick> <message>", description: "Send a direct message"),
    .init(command: "/nick", arguments: "<new nick>", description: "Change your nickname"),
    .init(command: "/whois", arguments: "<nick>", description: "Look up a user"),
    .init(command: "/away", arguments: "[message]", description: "Set away status"),
    .init(command: "/back", arguments: "", description: "Clear away status"),
    .init(command: "/topic", arguments: "[topic]", description: "Set the channel topic"),
    .init(command: "/mode", arguments: "<modes> [params]", description: "Set channel modes"),
    .init(command: "/op", arguments: "<nick>", description: "Give channel op (+o)"),
    .init(command: "/deop", arguments: "<nick>", description: "Remove channel op (-o)"),
    .init(command: "/voice", arguments: "<nick>", description: "Give voice (+v)"),
    .init(command: "/devoice", arguments: "<nick>", description: "Remove voice (-v)"),
    .init(command: "/kick", arguments: "<nick> [reason]", description: "Kick nick from channel"),
    .init(command: "/kickban", arguments: "<nick> [reason]", description: "Kick and ban nick"),
    .init(command: "/ban", arguments: "<mask>", description: "Ban mask (+b)"),
    .init(command: "/unban", arguments: "<mask>", description: "Unban mask (-b)"),
    .init(command: "/banlist", arguments: "", description: "Show channel ban list"),
    .init(command: "/list", arguments: "[filter]", description: "List channels on the network"),
    .init(command: "/archive", arguments: "", description: "Archive this buffer"),
    .init(command: "/unarchive", arguments: "", description: "Unarchive this buffer"),
    .init(
      command: "/delete",
      arguments: "",
      description: "Delete this archived buffer and its history",
    ),
  ]

  /// Commands whose name prefix-matches the first token of a `/`-leading
  /// composer text (web parity: matchSlashCommands — display-only popup, so
  /// the popup stays up while typing arguments as long as the prefix holds).
  static func matching(_ text: String) -> [SlashCommandHelp] {
    guard text.hasPrefix("/") else { return [] }
    let query = text.dropFirst().split(whereSeparator: \.isWhitespace).first.map(String.init) ?? ""
    let lowered = "/" + query.lowercased()
    return help.filter { $0.command.hasPrefix(lowered) }
  }

  static func parse(_ input: String, buffer: Buffer) -> ComposerResult {
    let value = input.trimmingCharacters(in: .whitespacesAndNewlines)
    guard value.hasPrefix("/") else {
      // The status window has no message target — it accepts commands only
      // (web parity: the status composer placeholder says the same).
      if buffer.kind == "status" {
        return .invalid("Commands only here — try /list, /join or /msg.")
      }
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
          content: rest.dropFirst().joined(separator: " "),
        )
      )

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

    case "mode":
      return require(content, usage: "/mode <modes> [params]") {
        ClientCommand(type: "mode", bufferID: buffer.id, content: content)
      }

    case "op",
         "deop",
         "voice",
         "devoice":
      return require(content, usage: "/\(name) <nick>") {
        ClientCommand(type: name, bufferID: buffer.id, target: content)
      }

    case "ban",
         "unban":
      return require(content, usage: "/\(name) <mask>") {
        ClientCommand(type: name, bufferID: buffer.id, target: content)
      }

    case "banlist":
      return .command(ClientCommand(type: "banlist", bufferID: buffer.id))

    case "kick",
         "kickban":
      guard let target = rest.first else {
        return .invalid("Usage: /\(name) <nick> [reason]")
      }
      return .command(
        ClientCommand(
          type: name,
          bufferID: buffer.id,
          target: target,
          content: rest.dropFirst().joined(separator: " "),
        )
      )

    case "list":
      return .command(ClientCommand(type: "list", networkID: buffer.networkID, content: content))

    case "archive":
      guard buffer.kind != "status" else {
        return .invalid("The status window cannot be archived.")
      }
      return .command(ClientCommand(type: "archive_buffer", bufferID: buffer.id))

    case "unarchive":
      guard buffer.kind != "status" else {
        return .invalid("The status window cannot be archived.")
      }
      return .command(ClientCommand(type: "unarchive_buffer", bufferID: buffer.id))

    case "delete":
      guard buffer.kind != "status", buffer.archived else {
        return .invalid("Only archived channels and conversations can be deleted.")
      }
      return .command(ClientCommand(type: "delete_buffer", bufferID: buffer.id))

    default:
      return .invalid("Unknown command /\(name)")
    }
  }

  // MARK: Private

  private static func require(
    _ value: String,
    usage: String,
    command: () -> ClientCommand,
  ) -> ComposerResult {
    value.isEmpty ? .invalid("Usage: \(usage)") : .command(command())
  }

}
