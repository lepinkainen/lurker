import Foundation
import Testing

@testable import Lurker

struct SlashCommandsTests {

  // MARK: Internal

  @Test
  func `plain text sends to buffer`() {
    guard case .command(let command) = SlashCommands.parse("hello", buffer: buffer) else {
      Issue.record("Expected command")
      return
    }
    #expect(command.type == "send")
    #expect(command.bufferID == buffer.id)
    #expect(command.content == "hello")
  }

  @Test
  func `parses common commands`() {
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

  @Test
  func `parses list command`() {
    guard case .command(let list) = SlashCommands.parse("/list", buffer: buffer) else {
      Issue.record("Expected list command")
      return
    }
    #expect(list.type == "list")
    #expect(list.networkID == buffer.networkID)
    #expect(list.content == "")

    guard case .command(let filtered) = SlashCommands.parse("/list linux", buffer: buffer) else {
      Issue.record("Expected filtered list command")
      return
    }
    #expect(filtered.content == "linux")
  }

  @Test
  func `parses moderation commands`() {
    guard case .command(let op) = SlashCommands.parse("/op tove", buffer: buffer) else {
      Issue.record("Expected op command")
      return
    }
    #expect(op.type == "op")
    #expect(op.bufferID == buffer.id)
    #expect(op.target == "tove")

    guard case .command(let mode) = SlashCommands.parse("/mode +m", buffer: buffer) else {
      Issue.record("Expected mode command")
      return
    }
    #expect(mode.type == "mode")
    #expect(mode.bufferID == buffer.id)
    #expect(mode.content == "+m")

    guard case .command(let kick) = SlashCommands.parse("/kick tove be nice", buffer: buffer)
    else {
      Issue.record("Expected kick command")
      return
    }
    #expect(kick.type == "kick")
    #expect(kick.target == "tove")
    #expect(kick.content == "be nice")

    // Target-less moderation commands are rejected with usage hints.
    guard case .invalid = SlashCommands.parse("/voice", buffer: buffer) else {
      Issue.record("Expected /voice without nick to be invalid")
      return
    }
    guard case .invalid = SlashCommands.parse("/kickban", buffer: buffer) else {
      Issue.record("Expected /kickban without nick to be invalid")
      return
    }
  }

  @Test
  func `status buffer accepts commands only`() {
    var status = buffer
    status.kind = "status"
    // Plain text has no message target in the status window.
    guard case .invalid = SlashCommands.parse("hello", buffer: status) else {
      Issue.record("Expected plain text to be rejected in status buffer")
      return
    }
    // Commands still parse.
    guard case .command(let list) = SlashCommands.parse("/list", buffer: status) else {
      Issue.record("Expected /list to work in status buffer")
      return
    }
    #expect(list.type == "list")
  }

  @Test
  func `rejects missing arguments and unknown commands`() {
    guard case .invalid = SlashCommands.parse("/msg tove", buffer: buffer) else {
      Issue.record("Expected invalid /msg")
      return
    }
    guard case .invalid = SlashCommands.parse("/raw WHO", buffer: buffer) else {
      Issue.record("Expected deferred command to be rejected")
      return
    }
  }

  @Test
  func `archive and unarchive target the buffer`() {
    guard case .command(let archive) = SlashCommands.parse("/archive", buffer: buffer) else {
      Issue.record("Expected /archive command")
      return
    }
    #expect(archive.type == "archive_buffer")
    #expect(archive.bufferID == buffer.id)

    guard case .command(let unarchive) = SlashCommands.parse("/unarchive", buffer: buffer) else {
      Issue.record("Expected /unarchive command")
      return
    }
    #expect(unarchive.type == "unarchive_buffer")

    var status = buffer
    status.kind = "status"
    guard case .invalid = SlashCommands.parse("/archive", buffer: status) else {
      Issue.record("Expected /archive to be rejected in status buffer")
      return
    }
  }

  @Test
  func `delete requires archived buffer`() {
    guard case .invalid = SlashCommands.parse("/delete", buffer: buffer) else {
      Issue.record("Expected /delete to be rejected on a non-archived buffer")
      return
    }
    var archived = buffer
    archived.archived = true
    guard case .command(let delete) = SlashCommands.parse("/delete", buffer: archived) else {
      Issue.record("Expected /delete command on archived buffer")
      return
    }
    #expect(delete.type == "delete_buffer")
    #expect(delete.bufferID == buffer.id)
  }

  // MARK: Private

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
    mentions: 0,
  )

}
