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
          "unread": 3, "mentions": 1,
          "marker_id": "0198f5f2-a000-7000-8000-000000000001",
          "marker_ts": "2026-07-23T08:12:00Z"
        }, {
          "id": "0198f5f2-9448-7ed6-b3b4-cd8a1a6e8b21",
          "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
          "name": "tove", "kind": "query", "joined": true,
          "show_embeds": true, "show_presence_events": true,
          "collapse_presence_events": false, "pinned": false,
          "unread": 0, "mentions": 0
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
    #expect(
      state.buffers.first?.markerID == UUID(uuidString: "0198f5f2-a000-7000-8000-000000000001"))
    #expect(state.buffers.first?.markerTS == "2026-07-23T08:12:00Z")
    // Marker keys omitted entirely — no marker.
    #expect(state.buffers.last?.markerID == nil)
    #expect(state.buffers.last?.markerTS == nil)
  }

  // The mark_read variant of buffer_update always carries the `marker_id` key:
  // JSON null means "caught up — clear the marker". The topic/joined variant
  // omits the key = unchanged. The double-optional field must keep those apart.
  @Test func decodesBufferUpdateMarkerPresentNullAndAbsent() throws {
    func decode(_ json: String) throws -> BufferUpdateEvent {
      let event = try JSONDecoder.lurker().decode(ServerEvent.self, from: Data(json.utf8))
      guard case .bufferUpdate(let update) = event else {
        Issue.record("Expected buffer_update event")
        throw LurkerAPIError.disconnected
      }
      return update
    }

    let set = try decode(
      """
      {
        "type": "buffer_update",
        "id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
        "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
        "last_seen_id": "0198f5f2-a000-7000-8000-000000000001",
        "marker_id": "0198f5f2-a000-7000-8000-000000000002",
        "marker_ts": "2026-07-23T08:13:00Z",
        "unread": 4, "mentions": 1
      }
      """)
    #expect(set.markerID == UUID(uuidString: "0198f5f2-a000-7000-8000-000000000002"))
    #expect(set.markerTS == "2026-07-23T08:13:00Z")
    #expect(set.unread == 4)

    let cleared = try decode(
      """
      {
        "type": "buffer_update",
        "id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
        "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
        "last_seen_id": "0198f5f2-a000-7000-8000-000000000009",
        "marker_id": null,
        "unread": 0, "mentions": 0
      }
      """)
    #expect(cleared.markerID != nil)  // key present…
    #expect(cleared.markerID! == nil)  // …with explicit null: clear the marker

    let unchanged = try decode(
      """
      {
        "type": "buffer_update",
        "id": "0198f5f2-9348-7ed6-b3b4-cd8a1a6e8b20",
        "network_id": "0198f5f2-8f2a-7a8b-9b42-4d6e72c4d8f1",
        "topic": "fresh topic"
      }
      """)
    #expect(unchanged.markerID == nil)  // key absent: marker unchanged
    #expect(unchanged.topic == "fresh topic")
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

  // Clickability is driven by which runs carry `.link`, so the runs must cover
  // exactly the URL text — nothing more, nothing less.
  @Test @MainActor func linkRunsCoverOnlyTheURLText() {
    let message = Message(
      id: UUID(),
      networkID: UUID(),
      bufferID: UUID(),
      ts: "2026-07-23T08:15:00Z",
      sender: "tove",
      kind: "privmsg",
      content:
        "inline links: https://example.com/lurker and https://news.ycombinator.com/item?id=1",
      displayKind: "message"
    )
    let rendered = attributedBody(message)
    let linkRuns = rendered.runs.compactMap { run in
      run.link.map { (text: String(rendered.characters[run.range]), url: $0.absoluteString) }
    }
    #expect(
      linkRuns.map(\.text) == [
        "https://example.com/lurker", "https://news.ycombinator.com/item?id=1",
      ])
    #expect(
      linkRuns.map(\.url) == [
        "https://example.com/lurker", "https://news.ycombinator.com/item?id=1",
      ])

    let plainText = rendered.runs.filter { $0.link == nil }.map {
      String(rendered.characters[$0.range])
    }.joined()
    #expect(plainText == "inline links:  and ")
  }

  @Test func decodesLiveServerStatusBufferMarker() throws {
    // Byte-for-byte /api/state output from a live server (status buffer with
    // residual unread): absent topic/last_seen_id, lowercase marker uuid.
    let json = """
      {"networks":[],"buffers":[{"id":"019faab1-2fc2-7ec9-8d1a-f5f794cb5e6d","network_id":"019faab1-2fc0-768b-a2dd-fc335dbd3dc0","name":"*status*","kind":"status","joined":false,"marker_id":"019faab1-2fc3-716b-b949-723cd31c6a9b","marker_ts":"2026-07-28T21:46:06Z","created_at":"2026-07-28T21:46:06.658Z","show_embeds":true,"show_presence_events":true,"collapse_presence_events":false,"pinned":false,"unread":3,"mentions":0}],"initial_messages":{}}
      """
    let snapshot = try JSONDecoder.lurker().decode(StateSnapshot.self, from: Data(json.utf8))
    let buffer = try #require(snapshot.buffers.first)
    #expect(buffer.unread == 3)
    #expect(buffer.markerID != nil)
    #expect(buffer.markerTS == "2026-07-28T21:46:06Z")
  }

  @Test func decodesChannelListEvent() throws {
    let json = """
      {"type":"channel_list","network_id":"019faab1-2fc0-768b-a2dd-fc335dbd3dc0",\
      "entries":[{"name":"#go-nuts","count":412,"topic":"Go talk"},{"name":"#quiet","count":2}],"done":true}
      """
    let event = try JSONDecoder.lurker().decode(ServerEvent.self, from: Data(json.utf8))
    guard case .channelList(let list) = event else {
      Issue.record("Expected channelList event, got \(event)")
      return
    }
    #expect(list.done)
    #expect(list.entries?.count == 2)
    #expect(list.entries?.first?.name == "#go-nuts")
    #expect(list.entries?.first?.count == 412)
    #expect(list.entries?.last?.topic == nil)

    // Go serializes an empty result as entries: null.
    let empty = try JSONDecoder.lurker().decode(
      ServerEvent.self,
      from: Data(
        #"{"type":"channel_list","network_id":"019faab1-2fc0-768b-a2dd-fc335dbd3dc0","entries":null,"done":true}"#
          .utf8))
    guard case .channelList(let emptyList) = empty else {
      Issue.record("Expected channelList event for empty list")
      return
    }
    #expect(emptyList.entries == nil)
  }

  @Test @MainActor func plainMessagesHaveNoLinkRuns() {
    let message = Message(
      id: UUID(),
      networkID: UUID(),
      bufferID: UUID(),
      ts: "2026-07-23T08:15:00Z",
      sender: "tove",
      kind: "privmsg",
      content: "no links in here, just words",
      displayKind: "message"
    )
    let rendered = attributedBody(message)
    #expect(rendered.runs.allSatisfy { $0.link == nil })
  }
}
