import Foundation

struct ServiceIdentity: Codable, Sendable, Equatable {
  let name: String
  let version: String
  let hash: String
  let buildTime: String

  /// "2026-01-01T12:00:00Z (3 days ago)", or the raw string when not ISO 8601
  /// (dev builds report "unknown").
  func buildTimeLabel(now: Date = Date()) -> String {
    guard let date = ISO8601DateFormatter().date(from: buildTime) else { return buildTime }
    let formatter = RelativeDateTimeFormatter()
    formatter.unitsStyle = .full
    let relative = formatter.localizedString(for: date, relativeTo: now)
    return "\(buildTime) (\(relative))"
  }
}

struct TailscaleStatus: Codable, Sendable, Equatable {
  let status: String
}

struct Network: Codable, Identifiable, Sendable, Hashable {
  let id: UUID
  var name: String
  var kind: String
  var host: String
  var port: Int
  var tls: Bool
  var nick: String
  var realname: String? = nil
  var status: String? = nil
  var sortOrder: Int
  var disabled: Bool = false

  private enum CodingKeys: String, CodingKey {
    case id, name, kind, host, port, tls, nick, realname, status, sortOrder, disabled
  }

  init(
    id: UUID,
    name: String,
    kind: String,
    host: String,
    port: Int,
    tls: Bool,
    nick: String,
    realname: String? = nil,
    status: String? = nil,
    sortOrder: Int,
    disabled: Bool = false
  ) {
    self.id = id
    self.name = name
    self.kind = kind
    self.host = host
    self.port = port
    self.tls = tls
    self.nick = nick
    self.realname = realname
    self.status = status
    self.sortOrder = sortOrder
    self.disabled = disabled
  }

  init(from decoder: Decoder) throws {
    let values = try decoder.container(keyedBy: CodingKeys.self)
    id = try values.decode(UUID.self, forKey: .id)
    name = try values.decode(String.self, forKey: .name)
    kind = try values.decode(String.self, forKey: .kind)
    host = try values.decode(String.self, forKey: .host)
    port = try values.decode(Int.self, forKey: .port)
    tls = try values.decode(Bool.self, forKey: .tls)
    nick = try values.decode(String.self, forKey: .nick)
    realname = try values.decodeIfPresent(String.self, forKey: .realname)
    status = try values.decodeIfPresent(String.self, forKey: .status)
    sortOrder = try values.decode(Int.self, forKey: .sortOrder)
    disabled = try values.decodeIfPresent(Bool.self, forKey: .disabled) ?? false
  }
}

struct Buffer: Codable, Identifiable, Sendable, Hashable {
  let id: UUID
  let networkID: UUID
  var name: String
  var kind: String
  var topic: String? = nil
  var joined: Bool
  var lastSeenID: UUID? = nil
  // Server-derived "New messages" marker: id/timestamp of the first unread
  // message that counts. Clients hold no marker state of their own; see
  // ai-docs/behaviors/new-messages-marker.md.
  var markerID: UUID? = nil
  var markerTS: String? = nil
  var showEmbeds: Bool
  var showPresenceEvents: Bool
  var collapsePresenceEvents: Bool
  var pinned: Bool
  // Persisted server-side flag driving the Archives section. Channels are
  // archived automatically on part/kick and unarchived on join; queries are
  // archived manually and unarchived by new activity.
  var archived: Bool = false
  // Manual channel ordering; channels sort (sortOrder, name). Defaults to 0
  // when absent so pre-sort_order backends keep alphabetical order.
  var sortOrder: Int = 0
  // Manual pinned-section ordering; pinned buffers sort (pinOrder, name).
  // Defaults to 0 when absent so pre-pin_order backends keep name order.
  var pinOrder: Int = 0
  var unread: Int
  var mentions: Int

  private enum CodingKeys: String, CodingKey {
    case id, networkID, name, kind, topic, joined, lastSeenID, markerID, markerTS,
      showEmbeds, showPresenceEvents, collapsePresenceEvents, pinned, archived, sortOrder, pinOrder,
      unread, mentions
  }

  init(
    id: UUID,
    networkID: UUID,
    name: String,
    kind: String,
    topic: String? = nil,
    joined: Bool,
    lastSeenID: UUID? = nil,
    markerID: UUID? = nil,
    markerTS: String? = nil,
    showEmbeds: Bool,
    showPresenceEvents: Bool,
    collapsePresenceEvents: Bool,
    pinned: Bool,
    archived: Bool = false,
    sortOrder: Int = 0,
    pinOrder: Int = 0,
    unread: Int,
    mentions: Int
  ) {
    self.id = id
    self.networkID = networkID
    self.name = name
    self.kind = kind
    self.topic = topic
    self.joined = joined
    self.lastSeenID = lastSeenID
    self.markerID = markerID
    self.markerTS = markerTS
    self.showEmbeds = showEmbeds
    self.showPresenceEvents = showPresenceEvents
    self.collapsePresenceEvents = collapsePresenceEvents
    self.pinned = pinned
    self.archived = archived
    self.sortOrder = sortOrder
    self.pinOrder = pinOrder
    self.unread = unread
    self.mentions = mentions
  }

  init(from decoder: Decoder) throws {
    let values = try decoder.container(keyedBy: CodingKeys.self)
    id = try values.decode(UUID.self, forKey: .id)
    networkID = try values.decode(UUID.self, forKey: .networkID)
    name = try values.decode(String.self, forKey: .name)
    kind = try values.decode(String.self, forKey: .kind)
    topic = try values.decodeIfPresent(String.self, forKey: .topic)
    joined = try values.decode(Bool.self, forKey: .joined)
    lastSeenID = try values.decodeIfPresent(UUID.self, forKey: .lastSeenID)
    markerID = try values.decodeIfPresent(UUID.self, forKey: .markerID)
    markerTS = try values.decodeIfPresent(String.self, forKey: .markerTS)
    showEmbeds = try values.decode(Bool.self, forKey: .showEmbeds)
    showPresenceEvents = try values.decode(Bool.self, forKey: .showPresenceEvents)
    collapsePresenceEvents = try values.decode(Bool.self, forKey: .collapsePresenceEvents)
    pinned = try values.decode(Bool.self, forKey: .pinned)
    archived = try values.decodeIfPresent(Bool.self, forKey: .archived) ?? false
    sortOrder = try values.decodeIfPresent(Int.self, forKey: .sortOrder) ?? 0
    pinOrder = try values.decodeIfPresent(Int.self, forKey: .pinOrder) ?? 0
    unread = try values.decode(Int.self, forKey: .unread)
    mentions = try values.decode(Int.self, forKey: .mentions)
  }
}

struct MircSegment: Codable, Sendable, Hashable {
  let text: String
  var bold: Bool? = nil
  var italic: Bool? = nil
  var underline: Bool? = nil
  var strike: Bool? = nil
  var fg: Int? = nil
}

struct NetsplitInfo: Codable, Sendable, Hashable {
  let serverA: String
  let serverB: String
}

struct Preview: Codable, Sendable, Hashable, Identifiable {
  var id: String { url }
  let url: String
  let kind: String
  var title: String? = nil
  var description: String? = nil
  var imageURL: String? = nil
  var siteName: String? = nil
}

struct Message: Codable, Identifiable, Sendable, Hashable {
  let id: UUID
  let networkID: UUID
  let bufferID: UUID
  let ts: String
  var sender: String
  var userhost: String? = nil
  var kind: String
  var target: String? = nil
  var content: String
  var displayKind: String
  var isSelf: Bool? = nil
  var mentionsMe: Bool? = nil
  var countsAsUnread: Bool? = nil
  var senderColor: Int? = nil
  var highlight: Bool? = nil
  var highlightPattern: String? = nil
  var netsplit: NetsplitInfo? = nil
  var segments: [MircSegment]? = nil
  var previews: [Preview]? = nil
}

/// Event kinds that represent user-presence changes — the rows the timeline
/// collapses into presence-summary runs and hides when a buffer's
/// `show_presence_events` is off. Mirror of `irc.presenceKinds` (Go) and
/// `PRESENCE_KINDS` (web/src/messages.ts); the shared contract fixture is
/// `testdata/semantic-kinds.json`.
let presenceKinds: Set<String> = [
  "join", "part", "quit", "nick", "away", "back", "account", "chghost",
]

struct Member: Codable, Identifiable, Sendable, Hashable {
  var id: String { nick.lowercased() }
  let nick: String
  var prefix: String? = nil
  var realname: String? = nil
  var away: Bool
  var `self`: Bool
  /// IRCv3 bot mode (https://ircv3.net/specs/extensions/bot-mode). Optional
  /// so snapshots from a backend predating the flag still decode.
  var bot: Bool? = nil
  var color: Int? = nil
}

struct StateSnapshot: Codable, Sendable {
  var networks: [Network]
  var buffers: [Buffer]
  var initialMessages: [String: [Message]]
  var members: [String: [Member]]?
}

struct BufferSettingsPatch: Codable, Sendable {
  var showEmbeds: Bool? = nil
  var showPresenceEvents: Bool? = nil
  var collapsePresenceEvents: Bool? = nil
  var pinned: Bool? = nil
  var archived: Bool? = nil
}

struct BufferSettingsEvent: Codable, Sendable {
  let id: UUID
  let showEmbeds: Bool
  let showPresenceEvents: Bool
  let collapsePresenceEvents: Bool
  let pinned: Bool
  let archived: Bool
  // Absent on pre-pin_order backends.
  var pinOrder: Int? = nil
}

struct BufferUpdateEvent: Decodable, Sendable {
  let id: UUID
  var networkID: UUID?
  var topic: String?
  var joined: Bool?
  var archived: Bool?
  var lastSeenID: UUID?
  /// Double optional: the mark_read variant always carries the `marker_id` key
  /// (JSON null means "caught up — clear the marker"), while the topic/joined
  /// variant omits it entirely (unchanged). Outer nil = key absent, inner nil =
  /// explicit null.
  var markerID: UUID??
  var markerTS: String?
  var unread: Int?
  var mentions: Int?

  private enum CodingKeys: String, CodingKey {
    case id, networkID, topic, joined, archived, lastSeenID, markerID, markerTS, unread, mentions
  }

  init(from decoder: Decoder) throws {
    let values = try decoder.container(keyedBy: CodingKeys.self)
    id = try values.decode(UUID.self, forKey: .id)
    networkID = try values.decodeIfPresent(UUID.self, forKey: .networkID)
    topic = try values.decodeIfPresent(String.self, forKey: .topic)
    joined = try values.decodeIfPresent(Bool.self, forKey: .joined)
    archived = try values.decodeIfPresent(Bool.self, forKey: .archived)
    lastSeenID = try values.decodeIfPresent(UUID.self, forKey: .lastSeenID)
    markerID =
      values.contains(.markerID)
      ? .some(try values.decodeIfPresent(UUID.self, forKey: .markerID))
      : .none
    markerTS = try values.decodeIfPresent(String.self, forKey: .markerTS)
    unread = try values.decodeIfPresent(Int.self, forKey: .unread)
    mentions = try values.decodeIfPresent(Int.self, forKey: .mentions)
  }
}

struct BufferCreatedEvent: Codable, Sendable {
  let id: UUID
  let networkID: UUID
  let name: String
  let kind: String
  // Absent on pre-sort_order backends.
  var sortOrder: Int?
}

struct BufferDeletedEvent: Codable, Sendable {
  let id: UUID
  let networkID: UUID
}

/// One (buffer, sort_order) pair in a `buffer_reorder` event.
struct BufferSortEntry: Codable, Sendable, Hashable {
  let id: UUID
  let sortOrder: Int
}

/// Broadcast after POST /api/networks/{id}/buffers/reorder; carries the
/// resulting order of ALL channel buffers of the network.
struct BufferReorderEvent: Codable, Sendable {
  let networkID: UUID
  let buffers: [BufferSortEntry]
}

/// One (buffer, pin_order) pair in a `pinned_reorder` event.
struct PinnedSortEntry: Codable, Sendable, Hashable {
  let id: UUID
  let pinOrder: Int
}

/// Broadcast after POST /api/buffers/pinned/reorder; carries the resulting
/// order of ALL pinned buffers.
struct PinnedReorderEvent: Codable, Sendable {
  let buffers: [PinnedSortEntry]
}

struct NetworkReorderResponse: Codable, Sendable {
  let networks: [Network]
}

struct MemberListEvent: Codable, Sendable {
  let networkID: UUID
  let bufferID: UUID
  let members: [Member]
}

struct HistoryBackfillEvent: Codable, Sendable {
  let networkID: UUID
  let bufferID: UUID
}

struct PreviewEvent: Codable, Sendable {
  let messageID: UUID
  let networkID: UUID
  let bufferID: UUID
  let previews: [Preview]
}

struct HistoryResult: Codable, Sendable {
  var reqID: String?
  let bufferID: UUID
  let messages: [Message]
}

struct NetworkStateEvent: Codable, Sendable {
  let networkID: UUID
  let state: String
}

struct NetsplitEvent: Codable, Sendable {
  let networkID: UUID
  let bufferID: UUID
  let netsplit: NetsplitInfo
  let messageIDs: [UUID]
}

struct CommandResponse: Codable, Sendable {
  var reqID: String?
  var message: String?
}

struct ChannelListEntry: Decodable, Sendable, Hashable, Identifiable {
  let name: String
  let count: Int
  var topic: String? = nil

  var id: String { name }
}

struct ChannelListEvent: Decodable, Sendable {
  let networkID: UUID
  // Go serializes an empty result as JSON null.
  let entries: [ChannelListEntry]?
  let done: Bool
}

enum ServerEvent: Sendable {
  case message(Message)
  case bufferCreated(BufferCreatedEvent)
  case bufferDeleted(BufferDeletedEvent)
  case bufferUpdate(BufferUpdateEvent)
  case bufferSettings(BufferSettingsEvent)
  case bufferReorder(BufferReorderEvent)
  case pinnedReorder(PinnedReorderEvent)
  case networkState(NetworkStateEvent)
  case history(HistoryResult)
  case historyBackfill(HistoryBackfillEvent)
  case preview(PreviewEvent)
  case members(MemberListEvent)
  case netsplit(NetsplitEvent)
  case channelList(ChannelListEvent)
  case ack(CommandResponse)
  case error(CommandResponse)
  case ignored(String)
}

extension ServerEvent: Decodable {
  private struct Envelope: Decodable {
    let type: String
  }

  init(from decoder: Decoder) throws {
    let type = try Envelope(from: decoder).type
    switch type {
    case "message": self = .message(try Message(from: decoder))
    case "buffer_created": self = .bufferCreated(try BufferCreatedEvent(from: decoder))
    case "buffer_deleted": self = .bufferDeleted(try BufferDeletedEvent(from: decoder))
    case "buffer_update": self = .bufferUpdate(try BufferUpdateEvent(from: decoder))
    case "buffer_settings": self = .bufferSettings(try BufferSettingsEvent(from: decoder))
    case "buffer_reorder": self = .bufferReorder(try BufferReorderEvent(from: decoder))
    case "pinned_reorder": self = .pinnedReorder(try PinnedReorderEvent(from: decoder))
    case "network_state": self = .networkState(try NetworkStateEvent(from: decoder))
    case "history_result": self = .history(try HistoryResult(from: decoder))
    case "history_backfill": self = .historyBackfill(try HistoryBackfillEvent(from: decoder))
    case "preview": self = .preview(try PreviewEvent(from: decoder))
    case "member_list": self = .members(try MemberListEvent(from: decoder))
    case "netsplit": self = .netsplit(try NetsplitEvent(from: decoder))
    case "channel_list": self = .channelList(try ChannelListEvent(from: decoder))
    case "ack": self = .ack(try CommandResponse(from: decoder))
    case "error": self = .error(try CommandResponse(from: decoder))
    default: self = .ignored(type)
    }
  }
}

extension JSONDecoder {
  static func lurker() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .custom { path in
      let key = path.last?.stringValue ?? ""
      return LurkerCodingKey(stringValue: WireKeyTransform.decode(key))!
    }
    return decoder
  }
}

extension JSONEncoder {
  static func lurker() -> JSONEncoder {
    let encoder = JSONEncoder()
    encoder.keyEncodingStrategy = .custom { path in
      let key = path.last?.stringValue ?? ""
      return LurkerCodingKey(stringValue: WireKeyTransform.encode(key))!
    }
    return encoder
  }
}

private struct LurkerCodingKey: CodingKey {
  let stringValue: String
  let intValue: Int?

  init?(stringValue: String) {
    self.stringValue = stringValue
    intValue = nil
  }

  init?(intValue: Int) {
    stringValue = String(intValue)
    self.intValue = intValue
  }
}

private enum WireKeyTransform {
  private static let decodedAcronyms = [
    "buffer_id": "bufferID",
    "image_url": "imageURL",
    "last_seen_id": "lastSeenID",
    "marker_id": "markerID",
    "marker_ts": "markerTS",
    "message_id": "messageID",
    "message_ids": "messageIDs",
    "network_id": "networkID",
    "req_id": "reqID",
  ]

  private static let encodedAcronyms = Dictionary(
    uniqueKeysWithValues: decodedAcronyms.map { ($0.value, $0.key) }
  )

  static func decode(_ key: String) -> String {
    if let mapped = decodedAcronyms[key] { return mapped }
    let parts = key.split(separator: "_", omittingEmptySubsequences: false)
    guard let first = parts.first, parts.count > 1 else { return key }
    return first + parts.dropFirst().map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined()
  }

  static func encode(_ key: String) -> String {
    if let mapped = encodedAcronyms[key] { return mapped }
    return key.reduce(into: "") { result, character in
      if character.isUppercase {
        if !result.isEmpty { result.append("_") }
        result.append(character.lowercased())
      } else {
        result.append(character)
      }
    }
  }
}
