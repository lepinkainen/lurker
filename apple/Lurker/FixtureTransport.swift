import Foundation

actor FixtureTransport: LurkerTransport {
  static let networkID = UUID(uuidString: "0198F5F2-8F2A-7A8B-9B42-4D6E72C4D8F1")!
  static let statusID = UUID(uuidString: "0198F5F2-8F2A-7A8B-9B42-4D6E72C4D8F0")!
  static let channelID = UUID(uuidString: "0198F5F2-9348-7ED6-B3B4-CD8A1A6E8B20")!
  static let queryID = UUID(uuidString: "0198F5F2-9448-7ED6-B3B4-CD8A1A6E8B21")!
  static let fullChannelID = UUID(uuidString: "0198F5F3-0000-7A8B-9B42-4D6E72C4D8F2")!
  static let archivedChannelID = UUID(uuidString: "0198F5F3-1000-7A8B-9B42-4D6E72C4D8F3")!
  static let archivedQueryID = UUID(uuidString: "0198F5F3-2000-7A8B-9B42-4D6E72C4D8F4")!

  static let fullTotal = 400
  static let fullPageSize = 50
  static let fullUnread = 10

  static let fullMessages: [Message] = buildFullMessages()

  nonisolated static func buildFullMessages() -> [Message] {
    let pool: [(String, Int)] = [
      ("shrike", 19),
      ("tove", 4),
      ("ava", 7),
      ("ircfriend", 31),
    ]
    let contentTemplates: [(Int) -> String] = [
      { i in "backlog line #\(i): checking in on the pagination work" },
      { i in "backlog line #\(i)" },
      { i in "backlog line #\(i): see https://example.com/thread/\(i) for context" },
      { i in "backlog line #\(i): that build finally went green after the flaky test fix" },
      { i in "backlog line #\(i): lol" },
      { i in
        "backlog line #\(i): can someone review the PR when they get a chance? https://example.com/thread/\(i)"
      },
      { i in
        "backlog line #\(i): scrolling through history to make sure everything renders correctly"
      },
      { i in "backlog line #\(i): +1" },
      { i in
        "backlog line #\(i): the timeline should merge and dedupe by id so this should be smooth"
      },
      { i in "backlog line #\(i): brb, coffee" },
    ]

    let formatter = ISO8601DateFormatter()
    guard let base = formatter.date(from: "2026-07-23T01:33:00Z") else {
      return []
    }

    var messages: [Message] = []
    messages.reserveCapacity(fullTotal)
    for i in 0..<fullTotal {
      let (sender, color) = pool[i % pool.count]
      let content = contentTemplates[i % contentTemplates.count](i)
      let ts = formatter.string(from: base.addingTimeInterval(Double(i) * 60))
      let id = UUID(uuidString: String(format: "0198F5F3-B000-7000-8000-%012X", i))!
      let isMention = sender != "shrike" && i % 17 == 0
      messages.append(
        Message(
          id: id,
          networkID: networkID,
          bufferID: fullChannelID,
          ts: ts,
          sender: sender,
          kind: "privmsg",
          content: content,
          displayKind: "message",
          mentionsMe: isMention,
          countsAsUnread: isMention,
          senderColor: color
        ))
    }
    return messages
  }

  nonisolated static let identity = ServiceIdentity(
    name: "lurker", version: "ui-test", hash: "fixture", buildTime: "2026-01-01T00:00:00Z")

  func validateServer() async throws -> ServiceIdentity {
    Self.identity
  }

  func fetchState() async throws -> StateSnapshot {
    Self.snapshot()
  }

  nonisolated static func snapshot() -> StateSnapshot {
    let status = Buffer(
      id: statusID,
      networkID: networkID,
      name: "Libera",
      kind: "status",
      joined: true,
      showEmbeds: false,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      unread: 0,
      mentions: 0
    )
    let channel = Buffer(
      id: channelID,
      networkID: networkID,
      name: "#lurker",
      kind: "channel",
      topic: "Native client development",
      joined: true,
      lastSeenID: nil,
      // Marker at the first countable fixture message so the divider and
      // unread bar render in the default preview/UI-test channel.
      markerID: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000001")!,
      markerTS: "2026-07-23T08:12:00Z",
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
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      unread: 0,
      mentions: 0
    )
    let fullChannel = Buffer(
      id: fullChannelID,
      networkID: networkID,
      name: "#lurker-full",
      kind: "channel",
      topic: "Procedurally generated backlog for pagination testing",
      joined: true,
      lastSeenID: fullMessages[fullTotal - fullUnread - 1].id,
      markerID: fullMessages[fullTotal - fullUnread].id,
      markerTS: fullMessages[fullTotal - fullUnread].ts,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: true,
      pinned: false,
      unread: fullUnread,
      mentions: 0
    )
    // Archived fixtures: a parted channel and an archived query so previews
    // and UI tests exercise the folded Archives section.
    let archivedChannel = Buffer(
      id: archivedChannelID,
      networkID: networkID,
      name: "#old-project",
      kind: "channel",
      topic: "Archived: no longer active",
      joined: false,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      archived: true,
      unread: 0,
      mentions: 0
    )
    let archivedQuery = Buffer(
      id: archivedQueryID,
      networkID: networkID,
      name: "driveby",
      kind: "query",
      joined: true,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: false,
      archived: true,
      unread: 0,
      mentions: 0
    )
    // Presence rows are interleaved with privmsgs so they render as individual
    // system rows; adjacent presence events would collapse into a summary
    // because the channel has `collapsePresenceEvents: true`.
    let messages = [
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000000")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:11:00Z",
        sender: "ava",
        kind: "join",
        content: "",
        displayKind: "sys",
        senderColor: 7
      ),
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
        content: "Nice — the inspector and unread state look right. This is a longer row",
        displayKind: "message",
        mentionsMe: true,
        countsAsUnread: true,
        senderColor: 19
      ),
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000004")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:14:00Z",
        sender: "ava",
        kind: "part",
        content: "brb, coffee",
        displayKind: "sys",
        senderColor: 7
      ),
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000005")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:15:00Z",
        sender: "tove",
        kind: "privmsg",
        content:
          "inline links: https://example.com/lurker and https://news.ycombinator.com/item?id=1",
        displayKind: "message",
        senderColor: 4
      ),
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000006")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:16:00Z",
        sender: "ircfriend",
        kind: "privmsg",
        content: "with a preview card: https://github.com/lepinkainen/lurker",
        displayKind: "message",
        senderColor: 31,
        previews: [
          Preview(
            url: "https://github.com/lepinkainen/lurker",
            kind: "opengraph",
            title: "lepinkainen/lurker: single-user IRC bouncer and clients",
            description: "Go bouncer with web, TUI and native clients.",
            siteName: "GitHub"
          )
        ]
      ),
      Message(
        id: UUID(uuidString: "0198F5F2-A000-7000-8000-000000000007")!,
        networkID: networkID,
        bufferID: channelID,
        ts: "2026-07-23T08:17:00Z",
        sender: "tove",
        kind: "privmsg",
        content: "inline image: https://example.com/screenshot.png",
        displayKind: "message",
        senderColor: 4,
        previews: [
          Preview(
            url: "https://example.com/screenshot.png",
            kind: "image"
          )
        ]
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
      buffers: [status, channel, query, fullChannel, archivedChannel, archivedQuery],
      initialMessages: [
        channelID.uuidString: messages,
        fullChannelID.uuidString: Array(fullMessages.suffix(fullPageSize)),
      ],
      members: [
        channelID.uuidString: [
          Member(
            nick: "shrike", prefix: "@", realname: "Shrike", away: false, self: true, color: 19),
          Member(nick: "tove", prefix: "+", realname: "Tove", away: false, self: false, color: 4),
          Member(nick: "ava", prefix: nil, realname: "Tove", away: false, self: false, color: 7),
          Member(nick: "anna", prefix: nil, realname: nil, away: false, self: false, color: 11),
          Member(nick: "arto", prefix: nil, realname: nil, away: false, self: false, color: 22),
          Member(nick: "alex", prefix: nil, realname: nil, away: true, self: false, color: 9),
          Member(nick: "ircfriend", prefix: nil, realname: nil, away: true, self: false, color: 31),
        ],
        fullChannelID.uuidString: [
          Member(
            nick: "shrike", prefix: "@", realname: "Shrike", away: false, self: true, color: 19),
          Member(nick: "tove", prefix: "+", realname: "Tove", away: false, self: false, color: 4),
          Member(nick: "ava", prefix: nil, realname: "Tove", away: false, self: false, color: 7),
          Member(nick: "anna", prefix: nil, realname: nil, away: false, self: false, color: 11),
          Member(nick: "arto", prefix: nil, realname: nil, away: false, self: false, color: 22),
          Member(nick: "alex", prefix: nil, realname: nil, away: true, self: false, color: 9),
          Member(nick: "ircfriend", prefix: nil, realname: nil, away: true, self: false, color: 31),
        ],
      ]
    )
  }

  func fetchHistory(bufferID: UUID, before: UUID?) async throws -> [Message] {
    guard bufferID == Self.fullChannelID else { return [] }
    let all = Self.fullMessages
    let beforeIndex: Int
    if let before, let idx = all.firstIndex(where: { $0.id == before }) {
      beforeIndex = idx
    } else {
      beforeIndex = all.count
    }
    let lower = max(0, beforeIndex - Self.fullPageSize)
    guard lower < beforeIndex else { return [] }
    return Array(all[lower..<beforeIndex])
  }

  // Mirrors the backend's MAX(pinned)+1 append semantics: each unpinned->pinned
  // transition gets the next counter value, so fixture pinning exercises the
  // same "new pins append to the end" contract as the real server.
  private var nextPinOrder = 1

  func updateBuffer(id: UUID, patch: BufferSettingsPatch) async throws -> BufferSettingsEvent {
    var pinOrder: Int?
    if patch.pinned == true {
      pinOrder = nextPinOrder
      nextPinOrder += 1
    }
    return BufferSettingsEvent(
      id: id,
      showEmbeds: patch.showEmbeds ?? true,
      showPresenceEvents: patch.showPresenceEvents ?? true,
      collapsePresenceEvents: patch.collapsePresenceEvents ?? true,
      pinned: patch.pinned ?? true,
      archived: patch.archived ?? false,
      pinOrder: pinOrder
    )
  }

  // Tests flip this to exercise the optimistic-reorder rollback paths.
  private var failReorders = false
  // Tests flip this to exercise the failed-send composer-restore path.
  private var failSends = false
  private(set) var reorderedNetworkIDs: [UUID]?
  private(set) var reorderedBufferIDs: [UUID]?
  private(set) var reorderedPinnedIDs: [UUID]?

  func setFailReorders(_ fail: Bool) {
    failReorders = fail
  }

  func setFailSends(_ fail: Bool) {
    failSends = fail
  }

  struct FixtureError: Error {}

  func reorderNetworks(ids: [UUID]) async throws -> [Network] {
    if failReorders { throw FixtureError() }
    reorderedNetworkIDs = ids
    // Empty response: the caller's optimistic order stands (tests inject
    // their own networks, which the fixture snapshot knows nothing about).
    return []
  }

  func reorderBuffers(networkID: UUID, ids: [UUID]) async throws -> BufferReorderEvent {
    if failReorders { throw FixtureError() }
    reorderedBufferIDs = ids
    return BufferReorderEvent(
      networkID: networkID,
      buffers: ids.enumerated().map { BufferSortEntry(id: $1, sortOrder: $0) })
  }

  func reorderPinnedBuffers(ids: [UUID]) async throws -> PinnedReorderEvent {
    if failReorders { throw FixtureError() }
    reorderedPinnedIDs = ids
    return PinnedReorderEvent(
      buffers: ids.enumerated().map { PinnedSortEntry(id: $1, pinOrder: $0) })
  }

  func openEvents() async -> AsyncThrowingStream<ServerEvent, Error> {
    AsyncThrowingStream { _ in }
  }

  func send(_: ClientCommand) async throws {
    if failSends { throw FixtureError() }
  }
  private var failPings = false
  private(set) var pingCount = 0
  private(set) var disconnectCount = 0

  func setFailPings(_ fail: Bool) {
    failPings = fail
  }

  func ping() async throws {
    pingCount += 1
    if failPings { throw FixtureError() }
  }

  func disconnect() async {
    disconnectCount += 1
  }

  private(set) var uploadedFilename: String?
  private(set) var uploadCount = 0
  // Tests flip this to hold uploads in flight until released, so they can
  // interleave model actions (buffer switches, second drops) mid-upload.
  private var holdUploads = false
  private var uploadGates: [CheckedContinuation<Void, Never>] = []

  func setHoldUploads(_ hold: Bool) {
    holdUploads = hold
  }

  /// Resumes every held upload and stops holding new ones.
  func releaseUploads() {
    holdUploads = false
    for gate in uploadGates { gate.resume() }
    uploadGates.removeAll()
  }

  func upload(_ data: Data, filename: String, contentType: String) async throws -> URL {
    uploadCount += 1
    uploadedFilename = filename
    if holdUploads {
      await withCheckedContinuation { uploadGates.append($0) }
    }
    return URL(string: "https://fixture.local/uploads/test.jpg")!
  }
}
