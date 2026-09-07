import Foundation
import Observation
import SwiftUI
import UniformTypeIdentifiers

// MARK: - ConnectionState

enum ConnectionState: Equatable {
  case notConfigured
  case connecting
  case connected
  case reconnecting(Int)
  case offline(String)

  // MARK: Internal

  var label: String {
    switch self {
    case .notConfigured: "Not configured"
    case .connecting: "Connecting"
    case .connected: "Connected"
    case .reconnecting(let seconds): "Retrying in \(seconds)s"
    case .offline: "Offline"
    }
  }

  var symbol: String {
    switch self {
    case .connected: "checkmark.circle.fill"
    case .connecting,
         .reconnecting: "arrow.trianglehead.2.clockwise.rotate.90"
    case .notConfigured,
         .offline: "exclamationmark.circle.fill"
    }
  }
}

// MARK: - SidebarBufferGroups

struct SidebarBufferGroups {
  let status: [Buffer]
  let channels: [Buffer]
  let queries: [Buffer]
  /// Buffers with the persisted archived flag (any kind), rendered inside the
  /// folded Archives section at the bottom of the network.
  let archived: [Buffer]

  var all: [Buffer] {
    status + channels + queries + archived
  }
}

// MARK: - AppModel

@MainActor
@Observable
final class AppModel {

  // MARK: Lifecycle

  init(
    transport: (any LurkerTransport)? = nil,
    defaults: UserDefaults = .standard,
    runsConnectionLoop: Bool = true,
  ) {
    self.transport = transport
    self.defaults = defaults
    self.runsConnectionLoop = runsConnectionLoop
    selectedBufferID = defaults.string(forKey: Defaults.selectedBuffer).flatMap(
      UUID.init(uuidString:)
    )
    inspectorVisible =
      defaults.object(forKey: Defaults.inspectorVisible) as? Bool ?? Self.defaultInspectorVisible
    notificationsEnabled = defaults.object(forKey: Defaults.notifications) as? Bool ?? true
    archivesOpen = Set(
      (defaults.stringArray(forKey: Defaults.archivesOpen) ?? []).compactMap(UUID.init(uuidString:))
    )
    collapsedNetworks = Set(
      (defaults.stringArray(forKey: Defaults.collapsedNetworks) ?? [])
        .compactMap(UUID.init(uuidString:))
    )
    if transport != nil {
      connectionState = .connecting
    }
  }

  // MARK: Internal

  /// Shared implementation state for the AppModel extensions. Stored properties
  /// stay in the observable class; cross-file helpers use internal access.
  enum Defaults {
    static let serverURL = "mac.serverURL"
    static let selectedBuffer = "mac.selectedBuffer"
    static let inspectorVisible = "mac.inspectorVisible"
    static let notifications = "mac.notifications"
    static let archivesOpen = "mac.archivesOpen"
    static let collapsedNetworks = "mac.collapsedNetworks"
  }

  // The members inspector starts hidden on iOS: `.inspector` presents as a
  // full-screen sheet on iPhone, which would cover the app on first launch.
  #if os(macOS)
  static let defaultInspectorVisible = true
  #else
  static let defaultInspectorVisible = false
  #endif

  var networks = [UUID: Network]()
  var buffers = [UUID: Buffer]()
  var messages = [UUID: [Message]]()
  var members = [UUID: [Member]]()
  /// Lowercased nicks flagged with IRCv3 bot mode. Only member lists carry
  /// the flag, so it is remembered here for message rows too. Sticky within a
  /// session: a member list rebuilt before the server's WHO reply lands would
  /// otherwise flip the glyph back.
  private(set) var botNicks = Set<String>()
  /// Lowercased nicks known to have an avatar image, keyed like `botNicks`.
  /// Member lists carry the flag directly on `Member`, but message rows only
  /// have a sender string, so it is remembered here too — mirrors `botNicks`
  /// for the same reason. Updated by member lists and by `avatar` events.
  var avatarNicks = Set<String>()
  var selectedBufferID: UUID?
  var historyExhausted = Set<UUID>()
  var historyLoading = Set<UUID>()
  // Set after older history is prepended; the timeline scrolls this message
  // back to the top edge so the viewport doesn't jump to the new content and
  // re-trigger the load (runaway pagination). Consumed (nil'd) by the view.
  var historyAnchor: HistoryAnchor?
  var connectionState = ConnectionState.notConfigured
  // True while an app-focus ping is probing a nominally-connected socket; the
  // displayed state can't be trusted until the probe resolves.
  var syncing = false
  var serviceIdentity: ServiceIdentity?
  var inspectorVisible = AppModel.defaultInspectorVisible
  var applicationActive = true
  var showingConnectionEditor = false
  var showingChannelSwitcher = false
  // Latest /list result; non-nil presents the channel-list sheet.
  var channelList: ChannelListEvent?
  // iOS has no `Settings` scene; settings is presented as an in-app sheet.
  var showingSettings = false
  var composerText = ""
  var composerError: String?
  // True while an attached image upload is in flight (disables the attach
  // affordance and shows a small progress indicator in the composer).
  var isUploading = false
  // Per-buffer sent-line history and drafts (in-memory, web parity).
  var inputHistory = InputHistory()
  // Per-network Archives fold state; folded by default, persisted across
  // launches like the other sidebar-adjacent Defaults.
  var archivesOpen = Set<UUID>()
  // Per-network sidebar collapse; expanded by default, persisted.
  var collapsedNetworks = Set<UUID>()
  var notificationsEnabled = true
  var columnVisibility = NavigationSplitViewVisibility.all
  // iOS compact width: whether ConversationView is pushed over the sidebar.
  var compactConversationVisible = false
  var focusComposerRequest = 0
  /// Retained so tests can await the focus ping deterministically.
  @ObservationIgnored var verifyTask: Task<Void, Never>?

  /// Set on app focus while offline: the reconnect countdown polls this each
  /// second and retries immediately instead of waiting out the backoff.
  var skipReconnectDelay = false

  @ObservationIgnored var transport: (any LurkerTransport)?
  @ObservationIgnored var connectionTask: Task<Void, Never>?
  @ObservationIgnored var queuedEvents = [ServerEvent]()
  @ObservationIgnored var hydrated = false
  @ObservationIgnored let defaults: UserDefaults
  @ObservationIgnored let runsConnectionLoop: Bool

  var configuredURL: URL? {
    guard let raw = defaults.string(forKey: Defaults.serverURL) else { return nil }
    return try? EndpointPolicy.normalize(raw)
  }

  var selectedBuffer: Buffer? {
    selectedBufferID.flatMap { buffers[$0] }
  }

  var selectedNetwork: Network? {
    selectedBuffer.flatMap { networks[$0.networkID] }
  }

  var selectedMessages: [Message] {
    guard let selectedBufferID else { return [] }
    return visibleMessages(messages[selectedBufferID] ?? [], in: buffers[selectedBufferID])
  }

  var selectedMembers: [Member] {
    guard let selectedBufferID else { return [] }
    return (members[selectedBufferID] ?? []).sorted {
      if $0.prefix != $1.prefix {
        return memberRank($0.prefix) < memberRank($1.prefix)
      }
      return $0.nick.localizedCaseInsensitiveCompare($1.nick) == .orderedAscending
    }
  }

  var mentionTotal: Int {
    buffers.values.reduce(0) { $0 + $1.mentions }
  }

  /// True whenever the displayed state may lag the backend: a focus ping is
  /// in flight, or the connection is anywhere but steady-state connected.
  /// `.notConfigured` is excluded — that's an empty state, not a stale one.
  var outOfSync: Bool {
    if syncing {
      return true
    }
    switch connectionState {
    case .connected,
         .notConfigured: return false
    case .connecting,
         .reconnecting,
         .offline: return true
    }
  }

  func selectBuffer(_ id: UUID) {
    guard buffers[id] != nil else { return }
    // Every open path (sidebar tap, channel switcher, notification, next-buffer)
    // funnels here, so this is where the compact-width conversation push happens.
    compactConversationVisible = true
    guard selectedBufferID != id else { return }
    // Opening a buffer never acks it: badge, divider, and unread bar clear
    // together only on an explicit ack (bar tap / Esc). See
    // ai-docs/behaviors/new-messages-marker.md.
    applySelection(id)
  }

  func setApplicationActive(_ active: Bool) {
    applicationActive = active
    if active {
      verifyConnection()
    }
  }

  func setInspectorVisible(_ visible: Bool) {
    inspectorVisible = visible
    defaults.set(visible, forKey: Defaults.inspectorVisible)
  }

  func toggleSidebar() {
    columnVisibility = columnVisibility == .detailOnly ? .all : .detailOnly
  }

  func focusComposer() {
    focusComposerRequest += 1
  }

  func setNotificationsEnabled(_ enabled: Bool) {
    notificationsEnabled = enabled
    defaults.set(enabled, forKey: Defaults.notifications)
  }

  /// Manual archive/unarchive (queries; channels normally flow through
  /// part/join, which the server mirrors into the archived flag).
  func setArchived(_ bufferID: UUID, _ archived: Bool) {
    updateBuffer(bufferID, BufferSettingsPatch(archived: archived))
  }

  /// Permanently delete an archived buffer. State updates arrive via the
  /// buffer_deleted broadcast — nothing optimistic here.
  func deleteBuffer(_ bufferID: UUID) {
    send(ClientCommand(type: "delete_buffer", bufferID: bufferID))
  }

  /// Returns the in-flight send so tests can await the failure path.
  @discardableResult
  func sendComposer() -> Task<Void, Never>? {
    guard let buffer = selectedBuffer else { return nil }
    let value = composerText.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty else { return nil }
    switch SlashCommands.parse(value, buffer: buffer) {
    case .invalid(let error):
      composerError = error
      return nil

    case .command(let command):
      // Only plain messages enter arrow-up history; slash commands do not
      // (web parity: recordSentInput).
      if !value.hasPrefix("/") {
        inputHistory.record(value, buffer: buffer.id)
      }
      composerError = nil
      composerText = ""
      return send(command)
    }
  }

  /// Normalizes and uploads a picked/dropped image (HEIC etc. are transcoded
  /// to JPEG client-side; the backend does not decode HEIC), then appends the
  /// returned URL to the initiating buffer's composer text, ready to send.
  /// One upload at a time: picks/drops while one is in flight are ignored.
  func attachImage(_ rawData: Data, sourceType: UTType?) async {
    guard let transport, let bufferID = selectedBufferID, !isUploading else { return }
    isUploading = true
    defer { isUploading = false }
    // Full-resolution decode + JPEG re-encode is too heavy for the main
    // actor (a large phone photo freezes scrolling and input); hop off.
    let normalized = await Task.detached(priority: .userInitiated) {
      ImageEncoding.normalize(rawData, sourceUTType: sourceType)
    }.value
    guard let normalized else {
      composerError = "Unsupported image"
      return
    }
    do {
      let url = try await transport.upload(
        normalized.data,
        filename: normalized.filename,
        contentType: normalized.contentType,
      )
      // The user may have switched buffers during the upload: the URL
      // belongs to the buffer the image was dropped on, not whichever is
      // visible now.
      if selectedBufferID == bufferID {
        appendToComposer(url.absoluteString)
        composerError = nil
      } else {
        inputHistory.appendToDraft(url.absoluteString, buffer: bufferID)
      }
    } catch {
      composerError = error.localizedDescription
    }
  }

  /// Arrow-up/down history browsing from the composer. Returns true when the
  /// key was consumed (text replaced), false to let the caret move normally.
  func navigateHistory(up: Bool) -> Bool {
    guard let bufferID = selectedBufferID else { return false }
    let replacement =
      up
        ? inputHistory.navigateUp(buffer: bufferID, current: composerText)
        : inputHistory.navigateDown(buffer: bufferID)
    guard let replacement else { return false }
    composerText = replacement
    return true
  }

  func command(_ command: ClientCommand) {
    send(command)
  }

  /// Soft-ignore: `nick`'s messages in `networkID` stay visible but stop
  /// raising the unread/activity dot. Mentions and highlights still badge —
  /// only the plain unread count and "New messages" marker are suppressed.
  /// Contrast with the (currently web-only) hard `ignore`, which drops
  /// messages entirely.
  func mute(nick: String, in networkID: UUID) {
    command(ClientCommand(type: "mute", networkID: networkID, target: nick))
  }

  func unmute(nick: String, in networkID: UUID) {
    command(ClientCommand(type: "unmute", networkID: networkID, target: nick))
  }

  @discardableResult
  func loadOlderHistory() -> Task<Void, Never>? {
    guard
      let id = selectedBufferID,
      !historyLoading.contains(id),
      // A pending anchor means the previous page's reposition hasn't landed
      // yet. The freshly prepended top rows can fire their load-older
      // `onAppear` in the same render pass that sets the anchor, before the
      // scroll moves the viewport off them, so without this a single scroll to
      // the top can start a second fetch.
      historyAnchor == nil,
      !historyExhausted.contains(id),
      let transport
    else {
      return nil
    }
    historyLoading.insert(id)
    // Two different ids: the fetch cursor must be the raw store head (the true
    // oldest known message), but the scroll anchor has to be an id the
    // timeline actually renders. Hidden presence events are not in the list at
    // all, and a collapsed presence run renders under its first member's id —
    // both of which resolve to the first *visible* message (see
    // ConversationView.items). Anchoring on the raw head would silently no-op
    // in `proxy.scrollTo` and leave the viewport at the top of the grown
    // content, re-triggering the load.
    let before = messages[id]?.first?.id
    let anchorID = visibleMessages(messages[id] ?? [], in: buffers[id]).first?.id
    let generation = selectionGeneration
    return Task {
      do {
        let older = try await transport.fetchHistory(bufferID: id, before: before)
        mergeMessages(older, into: id)
        if older.isEmpty {
          historyExhausted.insert(id)
        } else if let anchorID, generation == selectionGeneration {
          // Generation guard: if the selection moved away (even if it came
          // back to this buffer) the timeline was rebuilt bottom-anchored, and
          // repositioning it to a stale pagination point would yank the
          // viewport away from the newest messages.
          historyAnchor = HistoryAnchor(bufferID: id, messageID: anchorID)
        }
      } catch {
        composerError = error.localizedDescription
      }
      historyLoading.remove(id)
    }
  }

  func updateBuffer(_ id: UUID, _ patch: BufferSettingsPatch) {
    guard let transport else { return }
    Task {
      do {
        apply(try await transport.updateBuffer(id: id, patch: patch))
      } catch {
        composerError = error.localizedDescription
      }
    }
  }

  func previewImageURL(_ preview: Preview) -> URL? {
    normalizedImageURL(preview.imageURL)
  }

  /// URL for a kind == "image" preview, where `url` itself is the image.
  /// Same policy as thumbnails: https-absolute or server-relative only (ATS
  /// blocks plain http regardless).
  func inlineImageURL(_ preview: Preview) -> URL? {
    normalizedImageURL(preview.url)
  }

  /// Explicit user ack — the only way the marker, badges, and unread bar
  /// clear. Optimistically drops them locally; the server persists the new
  /// `last_seen_id` and broadcasts `buffer_update` to every client.
  func ackRead(_ bufferID: UUID) {
    guard var buffer = buffers[bufferID], let last = messages[bufferID]?.last else { return }
    buffer.lastSeenID = last.id
    buffer.markerID = nil
    buffer.markerTS = nil
    buffer.unread = 0
    buffer.mentions = 0
    buffers[bufferID] = buffer
    updateBadge()
    send(ClientCommand(type: "mark_read", bufferID: bufferID, messageID: last.id))
  }

  /// Whether the nick is known to be an IRCv3 bot on the selected buffer's
  /// network (every call site renders the selected buffer's content).
  /// Case-insensitive, matching the server's own nick folding.
  func isBot(_ nick: String) -> Bool {
    guard
      !nick.isEmpty,
      let bufferID = selectedBufferID,
      let networkID = buffers[bufferID]?.networkID
    else { return false }
    return botNicks.contains(nickKey(networkID, nick))
  }

  /// Whether the nick is known to have an avatar image on the selected
  /// buffer's network. Mirrors `isBot` exactly.
  func hasAvatar(_ nick: String) -> Bool {
    guard
      !nick.isEmpty,
      let bufferID = selectedBufferID,
      let networkID = buffers[bufferID]?.networkID
    else { return false }
    return avatarNicks.contains(nickKey(networkID, nick))
  }

  /// Builds the `/api/avatar` URL for a nick on a network. `size` is clamped
  /// server-side to {16,32,64,128,256}; 64 covers a ~14pt avatar box up to
  /// retina scales. `nil` when no server is configured.
  func avatarURL(networkID: UUID, nick: String, size: Int = 64) -> URL? {
    guard let base = configuredURL else { return nil }
    var components = URLComponents(
      url: base.appending(path: "api/avatar"),
      resolvingAgainstBaseURL: false,
    )
    components?.queryItems = [
      URLQueryItem(name: "network", value: networkID.uuidString),
      URLQueryItem(name: "nick", value: nick),
      URLQueryItem(name: "size", value: String(size)),
    ]
    return components?.url
  }

  /// Shared selection change: stashes the outgoing buffer's draft and
  /// restores the incoming one's. Passive paths (selection restore after a
  /// snapshot or buffer deletion) use this directly so drafts never leak
  /// between buffers, without selectBuffer's compact-width push.
  func applySelection(_ id: UUID) {
    if let previous = selectedBufferID {
      inputHistory.stashDraft(composerText, buffer: previous)
    }
    selectedBufferID = id
    // Any pending older-history reposition belongs to the buffer we are
    // leaving; the incoming (and later the returning) timeline is rebuilt
    // bottom-anchored, so an anchor surviving the switch would yank its
    // viewport back to an old pagination point. `selectionGeneration` also
    // makes in-flight loadOlderHistory fetches drop their anchor on arrival.
    historyAnchor = nil
    selectionGeneration += 1
    composerText = inputHistory.restoreDraft(buffer: id)
    composerError = nil
    defaults.set(id.uuidString, forKey: Defaults.selectedBuffer)
  }

  /// Bot nicks are keyed per network — the same nick can be a bot on one
  /// network and a human on another. Member lists are authoritative
  /// snapshots of the server-side tracker, so an explicit bot=false clears
  /// the entry (a human taking over a bot's nick stops rendering as a bot).
  func noteBots(_ list: [Member], networkID: UUID) {
    for member in list {
      let key = nickKey(networkID, member.nick)
      if member.bot == true {
        botNicks.insert(key)
      } else {
        botNicks.remove(key)
      }
    }
  }

  /// Member lists are authoritative snapshots of the server-side tracker,
  /// same as `noteBots`: an explicit hasAvatar=false clears the entry.
  func noteAvatars(_ list: [Member], networkID: UUID) {
    for member in list {
      let key = nickKey(networkID, member.nick)
      if member.hasAvatar == true {
        avatarNicks.insert(key)
      } else {
        avatarNicks.remove(key)
      }
    }
  }

  func nickKey(_ networkID: UUID, _ nick: String) -> String {
    "\(networkID.uuidString):\(nick.lowercased())"
  }

  func updateBadge() {
    NotificationManager.shared.setBadge(mentionTotal)
  }

  func resetServerState() {
    networks.removeAll()
    buffers.removeAll()
    messages.removeAll()
    members.removeAll()
    botNicks.removeAll()
    avatarNicks.removeAll()
    historyExhausted.removeAll()
    historyAnchor = nil
    channelList = nil
    selectedBufferID = nil
    hydrated = false
  }

  // MARK: Private

  /// Bumped on every selection change so an in-flight older-history fetch can
  /// tell that its anchor is stale by the time it resolves.
  @ObservationIgnored private var selectionGeneration = 0

  /// Appends text to the visible composer, space-padded from any existing
  /// content, but always at the end since the SwiftUI TextField here has no
  /// caret tracking.
  private func appendToComposer(_ text: String) {
    composerText = InputHistory.appending(text, to: composerText)
  }

  private func normalizedImageURL(_ raw: String?) -> URL? {
    guard let raw, !raw.isEmpty else { return nil }
    if let absolute = URL(string: raw), absolute.scheme == "https" {
      return absolute
    }
    guard raw.hasPrefix("/"), let base = configuredURL else { return nil }
    return URL(string: raw, relativeTo: base)?.absoluteURL
  }

  private func visibleMessages(_ values: [Message], in buffer: Buffer?) -> [Message] {
    guard let buffer else { return values }
    if buffer.showPresenceEvents {
      return values
    }
    return values.filter { !presenceKinds.contains($0.kind) }
  }

}

// MARK: - HistoryAnchor

/// Identifies the message that was at the top of a buffer before an older
/// history page was prepended, so the timeline can pin it back to the top
/// edge of the viewport.
struct HistoryAnchor: Equatable {
  let bufferID: UUID
  let messageID: UUID
}

private func memberRank(_ prefix: String?) -> Int {
  switch prefix {
  case "@",
       "&",
       "~": 0
  case "%": 1
  case "+": 2
  default: 3
  }
}
