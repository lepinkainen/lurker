import Foundation
import Testing

@testable import Lurker

struct WireModelTests {
  @Test func decodesStateSnapshot() throws {
    let data = Data(
      """
      {
        "networks": [{
          "id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
          "name": "Libera", "kind": "irc", "host": "irc.libera.chat",
          "port": 6697, "tls": true, "nick": "shrike", "sort_order": 0,
          "disabled": false
        }],
        "buffers": [{
          "id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
          "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
          "name": "#lurker", "kind": "channel", "joined": true,
          "show_embeds": true, "show_presence_events": true,
          "collapse_presence_events": false, "pinned": true,
          "unread": 3, "mentions": 1
        }],
        "initial_messages": {},
        "members": {}
      }
      """.utf8
    )
    let state = try JSONDecoder.lurker().decode(StateSnapshot.self, from: data)
    #expect(state.networks.first?.sortOrder == 0)
    #expect(state.buffers.first?.pinned == true)
    #expect(state.buffers.first?.mentions == 1)
  }

  // Third leg of the presence-kind contract: irc.presenceKinds (Go) and
  // PRESENCE_KINDS (web) assert against the same fixture, so a kind added
  // server-side fails here until the Swift mirror follows.
  @Test func presenceKindsMatchSharedContract() throws {
    let fixture = URL(fileURLWithPath: #filePath)
      .deletingLastPathComponent()  // LurkerTests
      .deletingLastPathComponent()  // macos
      .deletingLastPathComponent()  // repo root
      .appending(path: "testdata/semantic-kinds.json")
    struct Contract: Decodable { let presenceKinds: [String] }
    let contract = try JSONDecoder().decode(Contract.self, from: Data(contentsOf: fixture))
    #expect(Set(contract.presenceKinds) == presenceKinds)
  }

  @Test func decodesKnownAndUnknownEvents() throws {
    let message = Data(
      """
      {
        "type": "message",
        "id": "0198f5f2-a000-7000-8000-000000000001",
        "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
        "buffer_id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
              "ts": "2026-07-23T08:12:00Z", "sender": "tove",
              "kind": "privmsg", "content": "hello",
              "display_kind": "message",
              "netsplit": {
                "id": "irc.example.net|irc2.example.net|1784794320000",
                "server_a": "irc.example.net",
                "server_b": "irc2.example.net"
              }
      }
      """.utf8
    )
    let event = try JSONDecoder.lurker().decode(ServerEvent.self, from: message)
    guard case .message(let decoded) = event else {
      Issue.record("Expected message event")
      return
    }
    #expect(decoded.sender == "tove")
    #expect(decoded.netsplit?.id == "irc.example.net|irc2.example.net|1784794320000")

    let future = Data(#"{"type":"future_event","value":1}"#.utf8)
    guard case .ignored(let type) = try JSONDecoder.lurker().decode(ServerEvent.self, from: future)
    else {
      Issue.record("Expected ignored event")
      return
    }
    #expect(type == "future_event")
  }

  @Test func encodesCommandAcronymsAsSnakeCase() throws {
    let command = ClientCommand(
      type: "mark_read",
      reqID: "request-1",
      bufferID: UUID(uuidString: "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20"),
      messageID: UUID(uuidString: "0198f5f2-a000-7000-8000-000000000001")
    )
    let object = try JSONSerialization.jsonObject(with: JSONEncoder.lurker().encode(command))
    let payload = try #require(object as? [String: Any])
    #expect(payload["req_id"] as? String == "request-1")
    #expect(payload["buffer_id"] != nil)
    #expect(payload["message_id"] != nil)
    #expect(payload["reqID"] == nil)
  }

  @Test func decodesRESTHistoryEnvelope() throws {
    let data = Data(
      """
      {
        "buffer_id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
        "messages": [{
          "id": "0198f5f2-a000-7000-8000-000000000001",
          "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
          "buffer_id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
          "ts": "2026-07-23T08:12:00Z",
          "sender": "tove",
          "kind": "privmsg",
          "content": "older message",
          "display_kind": "message"
        }]
      }
      """.utf8
    )
    let history = try JSONDecoder.lurker().decode(HistoryResult.self, from: data)
    #expect(history.messages.count == 1)
    #expect(history.messages.first?.content == "older message")
  }

  @Test @MainActor func linksUseTheStrippedSegmentText() {
    let message = Message(
      id: UUID(),
      networkID: UUID(),
      bufferID: UUID(),
      ts: "2026-07-23T08:12:00Z",
      sender: "tove",
      kind: "privmsg",
      content: "\u{02}bold\u{02} https://example.com",
      displayKind: "message",
      segments: [
        MircSegment(text: "bold", bold: true),
        MircSegment(text: " https://example.com"),
      ]
    )
    let rendered = attributedBody(message)
    #expect(String(rendered.characters) == "bold https://example.com")
    #expect(rendered.runs.contains { $0.link?.absoluteString == "https://example.com" })
  }
}
