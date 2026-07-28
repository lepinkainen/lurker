import Foundation
import Testing

@testable import Lurker

struct SlashCommandsTests {
  private let buffer = Buffer(
    id: UUID(),
    networkID: UUID(),
    name: "#swift",
    kind: "channel",
    joined: true,
    showEmbeds: true,
    showPresenceEvents: true,
    collapsePresenceEvents: false,
    pinned: false,
    unread: 0,
    mentions: 0
  )

  @Test func plainTextSendsToBuffer() {
    guard case .command(let command) = SlashCommands.parse("hello", buffer: buffer) else {
      Issue.record("Expected command")
      return
    }
    #expect(command.type == "send")
    #expect(command.bufferID == buffer.id)
    #expect(command.content == "hello")
  }

  @Test func parsesCommonCommands() {
    guard case .command(let message) = SlashCommands.parse("/msg tove hello there", buffer: buffer)
    else {
      Issue.record("Expected message command")
      return
    }
    #expect(message.type == "msg")
    #expect(message.target == "tove")
    #expect(message.content == "hello there")

    guard case .command(let join) = SlashCommands.parse("/join #macdev", buffer: buffer) else {
      Issue.record("Expected join command")
      return
    }
    #expect(join.networkID == buffer.networkID)
    #expect(join.channel == "#macdev")
  }

  @Test func rejectsMissingArgumentsAndUnknownCommands() {
    guard case .invalid = SlashCommands.parse("/msg tove", buffer: buffer) else {
      Issue.record("Expected invalid /msg")
      return
    }
    guard case .invalid = SlashCommands.parse("/raw WHO", buffer: buffer) else {
      Issue.record("Expected deferred command to be rejected")
      return
    }
  }
}
