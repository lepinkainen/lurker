import Foundation

actor FixtureTransport: LurkerTransport {
  private let networkID = UUID(uuidString: "0198F5F2-8F2A-7A8B-9B42-4D6E72C4D8F1")!
  private let channelID = UUID(uuidString: "0198F5F2-9348-7ED6-B3B4-CD8A1A6E8B20")!
  private let queryID = UUID(uuidString: "0198F5F2-9448-7ED6-B3B4-CD8A1A6E8B21")!

  func validateServer() async throws -> ServiceIdentity {
    ServiceIdentity(
      name: "lurker", version: "ui-test", hash: "fixture", buildTime: "2026-01-01T00:00:00Z")
  }

  func fetchState() async throws -> StateSnapshot {
    let channel = Buffer(
      id: channelID,
      networkID: networkID,
      name: "#lurker",
      kind: "channel",
      topic: "Native client development",
      joined: true,
      lastSeenID: nil,
      createdAt: "2026-01-01T00:00:00Z",
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: true,
      pinned: true,
      unread: 2,
      mentions: 1
    )
    let query = Buffer(
      id: queryID,
      networkID: networkID,
      name: "tove",
      kind: "query",
      topic: nil,
      joined: true,
      lastSeenID: nil,
      createdAt: "2026-01-01T00:00:00Z",
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      unread: 0,
      mentions: 0
    )
    let messages = [
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000001")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:12:00Z",
        sender: "tove",
        kind: "privmsg",
        content: "The native client is connected.",
        displayKind: "message",
        senderColor: 4
      ),
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000002")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:13:00Z",
        sender: "shrike",
        kind: "privmsg",
        content: "Nice — the inspector and unread state look right.",
        displayKind: "message",
        mentionsMe: true,
        countsAsUnread: true,
        senderColor: 19
      ),
    ]
    return StateSnapshot(
      networks: [
        Network(
          id: networkID,
          name: "Libera",
          kind: "irc",
          host: "irc.libera.chat",
          port: 6697,
          tls: true,
          nick: "shrike",
          status: "connected",
          sortOrder: 0,
          disabled: false
        )
      ],
      buffers: [channel, query],
      initialMessages: [channelID.uuidString: messages],
      members: [
        channelID.uuidString: [
          Member(
            nick: "shrike", prefix: "@", realname: "Shrike", away: false, self: true, color: 19),
          Member(nick: "tove", prefix: "+", realname: "Tove", away: false, self: false, color: 4),
          Member(nick: "ircfriend", prefix: nil, realname: nil, away: true, self: false, color: 31),
        ]
      ]
    )
  }

  func fetchHistory(bufferID _: UUID, before _: UUID?) async throws -> [Message] {
    []
  }

  func updateBuffer(id: UUID, patch: BufferSettingsPatch) async throws -> BufferSettingsEvent {
    BufferSettingsEvent(
      id: id,
      showEmbeds: patch.showEmbeds ?? true,
      showPresenceEvents: patch.showPresenceEvents ?? true,
      collapsePresenceEvents: patch.collapsePresenceEvents ?? true,
      pinned: patch.pinned ?? true
    )
  }

  func openEvents() async -> AsyncThrowingStream<ServerEvent, Error> {
    AsyncThrowingStream { _ in }
  }

  func send(_: ClientCommand) async throws {}
  func disconnect() async {}
}
