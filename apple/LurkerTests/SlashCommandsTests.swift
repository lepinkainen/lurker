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

  @Test func parsesListCommand() {
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

  @Test func statusBufferAcceptsCommandsOnly() {
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

  @Test func archiveAndUnarchiveTargetTheBuffer() {
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

  @Test func deleteRequiresArchivedBuffer() {
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
}
