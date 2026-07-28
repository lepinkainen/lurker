import Foundation
import Observation
import SwiftUI

enum ConnectionState: Equatable {
  case notConfigured
  case connecting
  case connected
  case reconnecting(Int)
  case offline(String)

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
    case .connecting, .reconnecting: "arrow.trianglehead.2.clockwise.rotate.90"
    case .notConfigured, .offline: "exclamationmark.circle.fill"
    }
  }
}

struct SidebarBufferGroups {
  let status: [Buffer]
  let channels: [Buffer]
  let queries: [Buffer]

  var all: [Buffer] {
    status + channels + queries
  }
}

@MainActor
@Observable
final class AppModel {
  private enum Defaults {
    static let serverURL = "mac.serverURL"
    static let selectedBuffer = "mac.selectedBuffer"
    static let inspectorVisible = "mac.inspectorVisible"
    static let notifications = "mac.notifications"
  }

  var networks: [UUID: Network] = [:]
  var buffers: [UUID: Buffer] = [:]
  var messages: [UUID: [Message]] = [:]
  var members: [UUID: [Member]] = [:]
  var selectedBufferID: UUID?
  var markerAnchors: [UUID: UUID] = [:]
  var historyExhausted: Set<UUID> = []
  var historyLoading: Set<UUID> = []
  var connectionState: ConnectionState = .notConfigured
  var serviceIdentity: ServiceIdentity?
  var inspectorVisible = AppModel.defaultInspectorVisible
  var applicationActive = true
  var showingConnectionEditor = false
  var showingChannelSwitcher = false
  // iOS has no `Settings` scene; settings is presented as an in-app sheet.
  var showingSettings = false
  var composerText = ""
  var composerError: String?
  var notificationsEnabled = true
  var columnVisibility: NavigationSplitViewVisibility = .all
  // iOS compact width: whether ConversationView is pushed over the sidebar.
  var compactConversationVisible = false
  var focusComposerRequest = 0

  // The members inspector starts hidden on iOS: `.inspector` presents as a
  // full-screen sheet on iPhone, which would cover the app on first launch.
  #if os(macOS)
    static let defaultInspectorVisible = true
  #else
    static let defaultInspectorVisible = false
  #endif

  @ObservationIgnored private var transport: (any LurkerTransport)?
  @ObservationIgnored private var connectionTask: Task<Void, Never>?
  @ObservationIgnored private var queuedEvents: [ServerEvent] = []
  @ObservationIgnored private var hydrated = false
  @ObservationIgnored private let defaults: UserDefaults
  @ObservationIgnored private let runsConnectionLoop: Bool

  init(
    transport: (any LurkerTransport)? = nil,
    defaults: UserDefaults = .standard,
    runsConnectionLoop: Bool = true
  ) {
    self.transport = transport
    self.defaults = defaults
    self.runsConnectionLoop = runsConnectionLoop
    selectedBufferID = defaults.string(forKey: Defaults.selectedBuffer).flatMap(
      UUID.init(uuidString:))
    inspectorVisible =
      defaults.object(forKey: Defaults.inspectorVisible) as? Bool ?? Self.defaultInspectorVisible
    notificationsEnabled = defaults.object(forKey: Defaults.notifications) as? Bool ?? true
    if transport != nil {
      connectionState = .connecting
    }
  }

  var configuredURL: URL? {
    guard let raw = defaults.string(forKey: Defaults.serverURL) else { return nil }
    return try? EndpointPolicy.normalize(raw)
  }

  var orderedNetworks: [Network] {
    networks.values.sorted {
      $0.sortOrder == $1.sortOrder
        ? $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
        : $0.sortOrder < $1.sortOrder
    }
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

  func start() {
    guard runsConnectionLoop else { return }
    guard connectionTask == nil else { return }
    if transport == nil {
      guard let url = configuredURL else {
        connectionState = .notConfigured
        showingConnectionEditor = true
        return
      }
      transport = LurkerAPI(baseURL: url)
    }
    if !ProcessInfo.isPreviewOrUITest {
      NotificationManager.shared.configure { [weak self] id in
        self?.selectBuffer(id)
      }
    }
    connectionTask = Task { [weak self] in
      await self?.connectionLoop()
    }
  }

  func stop() {
    connectionTask?.cancel()
    connectionTask = nil
    if let transport {
      Task { await transport.disconnect() }
    }
  }

  func saveServer(_ raw: String) async throws {
    let url = try EndpointPolicy.normalize(raw)
    let candidate = LurkerAPI(baseURL: url)
    let identity = try await candidate.validateServer()
    defaults.set(url.absoluteString, forKey: Defaults.serverURL)
    stop()
    resetServerState()
    transport = candidate
    serviceIdentity = identity
    showingConnectionEditor = false
    start()
  }

  func selectBuffer(_ id: UUID) {
    guard buffers[id] != nil else { return }
    // Every open path (sidebar tap, channel switcher, notification, next-buffer)
    // funnels here, so this is where the compact-width conversation push happens.
    compactConversationVisible = true
    guard selectedBufferID != id else { return }
    if applicationActive, let previous = selectedBufferID {
      markRead(previous)
    }
    selectedBufferID = id
    defaults.set(id.uuidString, forKey: Defaults.selectedBuffer)
    if applicationActive {
      markRead(id)
    }
  }

  func setApplicationActive(_ active: Bool) {
    applicationActive = active
    if active, let selectedBufferID {
      markRead(selectedBufferID)
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

  func sendComposer() {
    guard let buffer = selectedBuffer else { return }
    let value = composerText.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty else { return }
    switch SlashCommands.parse(value, buffer: buffer) {
    case .invalid(let error):
      composerError = error
    case .command(let command):
      composerError = nil
      composerText = ""
      send(command)
    }
  }

  func command(_ command: ClientCommand) {
    send(command)
  }

  func loadOlderHistory() {
    guard let id = selectedBufferID,
      !historyLoading.contains(id),
      !historyExhausted.contains(id),
      let transport
    else {
      return
    }
    historyLoading.insert(id)
    let before = messages[id]?.first?.id
    Task {
      do {
        let older = try await transport.fetchHistory(bufferID: id, before: before)
        mergeMessages(older, into: id)
        if older.isEmpty {
          historyExhausted.insert(id)
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

  func nextBuffer(unreadOnly: Bool = false, mentionsOnly: Bool = false, direction: Int = 1) {
    var candidates = sidebarBufferOrder()
    if unreadOnly {
      candidates = candidates.filter { buffers[$0]?.unread ?? 0 > 0 }
    }
    if mentionsOnly {
      candidates = candidates.filter { buffers[$0]?.mentions ?? 0 > 0 }
    }
    guard !candidates.isEmpty else { return }
    let index =
      selectedBufferID.flatMap { candidates.firstIndex(of: $0) } ?? (direction > 0 ? -1 : 0)
    let next = (index + direction + candidates.count) % candidates.count
    selectBuffer(candidates[next])
  }

  func focusStatusBuffer() {
    guard let networkID = selectedBuffer?.networkID,
      let status = buffers.values.first(where: { $0.networkID == networkID && $0.kind == "status" })
    else {
      return
    }
    selectBuffer(status.id)
  }

  func previewImageURL(_ preview: Preview) -> URL? {
    guard let raw = preview.imageURL, !raw.isEmpty else { return nil }
    if let absolute = URL(string: raw), absolute.scheme == "https" {
      return absolute
    }
    guard raw.hasPrefix("/"), let base = configuredURL else { return nil }
    return URL(string: raw, relativeTo: base)?.absoluteURL
  }

  private func connectionLoop() async {
    guard let transport else { return }
    var attempt = 0
    while !Task.isCancelled {
      do {
        connectionState = attempt == 0 ? .connecting : .reconnecting(0)
        hydrated = false
        queuedEvents.removeAll(keepingCapacity: true)
        let stream = await transport.openEvents()
        let receiver = Task { @MainActor [weak self] in
          for try await event in stream {
            self?.receive(event)
          }
        }
        defer { receiver.cancel() }
        serviceIdentity = try await transport.validateServer()
        applySnapshot(try await transport.fetchState())
        hydrated = true
        for event in queuedEvents {
          apply(event)
        }
        queuedEvents.removeAll(keepingCapacity: true)
        connectionState = .connected
        attempt = 0
        try await receiver.value
        throw LurkerAPIError.disconnected
      } catch is CancellationError {
        return
      } catch {
        await transport.disconnect()
        hydrated = false
        attempt += 1
        let delay = min(30, 1 << min(attempt - 1, 5))
        connectionState = .offline(error.localizedDescription)
        for remaining in stride(from: delay, through: 1, by: -1) {
          connectionState = .reconnecting(remaining)
          try? await Task.sleep(for: .seconds(1))
          if Task.isCancelled { return }
        }
      }
    }
  }

  private func receive(_ event: ServerEvent) {
    guard hydrated else {
      queuedEvents.append(event)
      return
    }
    apply(event)
  }

  func applySnapshot(_ snapshot: StateSnapshot) {
    historyExhausted.removeAll(keepingCapacity: true)
    historyLoading.removeAll(keepingCapacity: true)
    networks = Dictionary(uniqueKeysWithValues: snapshot.networks.map { ($0.id, $0) })
    buffers = Dictionary(uniqueKeysWithValues: snapshot.buffers.map { ($0.id, $0) })
    messages = Dictionary(
      uniqueKeysWithValues: snapshot.initialMessages.compactMap { key, value in
        UUID(uuidString: key).map { ($0, value.sorted(by: messageOrder)) }
      })
    members = Dictionary(
      uniqueKeysWithValues: (snapshot.members ?? [:]).compactMap { key, value in
        UUID(uuidString: key).map { ($0, value) }
      })
    establishUnreadMarkers()
    restoreSelection()
    updateBadge()
  }

  private func apply(_ event: ServerEvent) {
    switch event {
    case .message(let message):
      apply(message)
    case .bufferCreated(let event):
      if buffers[event.id] == nil {
        buffers[event.id] = Buffer(
          id: event.id,
          networkID: event.networkID,
          name: event.name,
          kind: event.kind,
          topic: nil,
          joined: event.kind == "channel",
          lastSeenID: nil,
          createdAt: event.createdAt,
          showEmbeds: true,
          showPresenceEvents: true,
          collapsePresenceEvents: false,
          pinned: false,
          unread: 0,
          mentions: 0
        )
      }
    case .bufferUpdate(let event):
      guard var buffer = buffers[event.id] else { return }
      if let topic = event.topic { buffer.topic = topic }
      if let joined = event.joined { buffer.joined = joined }
      if let lastSeenID = event.lastSeenID { buffer.lastSeenID = lastSeenID }
      if let unread = event.unread { buffer.unread = unread }
      if let mentions = event.mentions { buffer.mentions = mentions }
      buffers[event.id] = buffer
      updateBadge()
    case .bufferSettings(let event):
      apply(event)
    case .networkState(let event):
      guard var network = networks[event.networkID] else { return }
      network.status = event.state
      networks[event.networkID] = network
    case .history(let event):
      mergeMessages(event.messages, into: event.bufferID)
      if event.messages.isEmpty { historyExhausted.insert(event.bufferID) }
    case .preview(let event):
      guard var list = messages[event.bufferID],
        let index = list.firstIndex(where: { $0.id == event.messageID })
      else {
        return
      }
      list[index].previews = event.previews
      messages[event.bufferID] = list
    case .members(let event):
      members[event.bufferID] = event.members
    case .netsplit(let event):
      guard var list = messages[event.bufferID] else { return }
      let ids = Set(event.messageIDs)
      for index in list.indices where ids.contains(list[index].id) {
        list[index].netsplit = event.netsplit
      }
      messages[event.bufferID] = list
    case .error(let response):
      composerError = response.message ?? "The server rejected the command."
    case .ack, .ignored:
      break
    }
  }

  private func apply(_ event: BufferSettingsEvent) {
    guard var buffer = buffers[event.id] else { return }
    buffer.showEmbeds = event.showEmbeds
    buffer.showPresenceEvents = event.showPresenceEvents
    buffer.collapsePresenceEvents = event.collapsePresenceEvents
    buffer.pinned = event.pinned
    buffers[event.id] = buffer
  }

  private func apply(_ message: Message) {
    let wasKnown = messages[message.bufferID]?.contains(where: { $0.id == message.id }) == true
    mergeMessages([message], into: message.bufferID)
    guard !wasKnown, var buffer = buffers[message.bufferID] else { return }

    if selectedBufferID == message.bufferID, applicationActive {
      markRead(message.bufferID)
      return
    }
    if message.countsAsUnread == true, message.isSelf != true {
      if buffer.unread == 0 {
        markerAnchors[buffer.id] = message.id
      }
      buffer.unread += 1
      if message.mentionsMe == true || message.highlight == true {
        buffer.mentions += 1
        if !applicationActive, notificationsEnabled {
          NotificationManager.shared.post(
            message: message,
            buffer: buffer,
            network: networks[buffer.networkID]
          )
        }
      }
      buffers[buffer.id] = buffer
      updateBadge()
    }
  }

  private func mergeMessages(_ incoming: [Message], into bufferID: UUID) {
    var byID = Dictionary(uniqueKeysWithValues: (messages[bufferID] ?? []).map { ($0.id, $0) })
    for message in incoming {
      byID[message.id] = message
    }
    messages[bufferID] = byID.values.sorted(by: messageOrder)
  }

  private func markRead(_ bufferID: UUID) {
    guard var buffer = buffers[bufferID], let last = messages[bufferID]?.last else { return }
    buffer.lastSeenID = last.id
    buffer.unread = 0
    buffer.mentions = 0
    buffers[bufferID] = buffer
    markerAnchors.removeValue(forKey: bufferID)
    updateBadge()
    send(ClientCommand(type: "mark_read", bufferID: bufferID, messageID: last.id))
  }

  private func send(_ command: ClientCommand) {
    guard let transport else { return }
    Task {
      do {
        try await transport.send(command)
      } catch {
        composerError = error.localizedDescription
      }
    }
  }

  private func establishUnreadMarkers() {
    markerAnchors.removeAll(keepingCapacity: true)
    for buffer in buffers.values where buffer.unread > 0 {
      let anchor = messages[buffer.id]?.first {
        guard let seen = buffer.lastSeenID else { return true }
        return $0.id.uuidString > seen.uuidString
      }
      if let anchor {
        markerAnchors[buffer.id] = anchor.id
      }
    }
  }

  private func restoreSelection() {
    if let selectedBufferID, buffers[selectedBufferID] != nil {
      return
    }
    selectedBufferID = sidebarBufferOrder().first
  }

  private func sidebarBufferOrder() -> [UUID] {
    var result = pinnedBuffers.map(\.id)
    for network in orderedNetworks where !network.disabled {
      result.append(contentsOf: sidebarBuffers(for: network.id).all.map(\.id))
    }
    var seen = Set<UUID>()
    return result.filter { seen.insert($0).inserted }
  }

  var pinnedBuffers: [Buffer] {
    buffers.values
      .filter { $0.pinned && $0.kind == "channel" }
      .sorted(by: bufferOrder)
  }

  func sidebarBuffers(for networkID: UUID) -> SidebarBufferGroups {
    let values = buffers.values.filter {
      $0.networkID == networkID && !($0.kind == "channel" && $0.pinned)
    }
    return SidebarBufferGroups(
      status: values.filter { $0.kind == "status" }.sorted(by: bufferOrder),
      channels: values.filter { $0.kind == "channel" }.sorted(by: bufferOrder),
      queries: values.filter { $0.kind == "query" }.sorted(by: bufferOrder)
    )
  }

  private func visibleMessages(_ values: [Message], in buffer: Buffer?) -> [Message] {
    guard let buffer else { return values }
    if buffer.showPresenceEvents { return values }
    return values.filter { !presenceKinds.contains($0.kind) }
  }

  private func updateBadge() {
    NotificationManager.shared.setBadge(mentionTotal)
  }

  private func resetServerState() {
    networks.removeAll()
    buffers.removeAll()
    messages.removeAll()
    members.removeAll()
    markerAnchors.removeAll()
    historyExhausted.removeAll()
    selectedBufferID = nil
    hydrated = false
  }
}

private func memberRank(_ prefix: String?) -> Int {
  switch prefix {
  case "@", "&", "~": 0
  case "%": 1
  case "+": 2
  default: 3
  }
}

private func messageOrder(_ lhs: Message, _ rhs: Message) -> Bool {
  lhs.id.uuidString < rhs.id.uuidString
}

private func bufferOrder(_ lhs: Buffer, _ rhs: Buffer) -> Bool {
  lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
}

#if DEBUG
  @MainActor
  extension AppModel {
    /// A fully hydrated, "connected and joined" model for SwiftUI previews.
    /// Populates state synchronously instead of running the async connection loop,
    /// so previews render fully populated in a single pass with no transport,
    /// async work, or mark-read side effects.
    static func preview() -> AppModel {
      let model = AppModel(transport: FixtureTransport(), runsConnectionLoop: false)
      model.applySnapshot(FixtureTransport.snapshot())
      model.serviceIdentity = FixtureTransport.identity
      model.connectionState = .connected
      model.hydrated = true
      model.selectedBufferID = FixtureTransport.channelID
      return model
    }

    /// A multi-network fixture for the sidebar preview: several servers, each with
    /// a status buffer plus a handful of channels/queries carrying varied unread and
    /// mention counts. Hand-built here (not via `FixtureTransport`) so it can grow
    /// without disturbing the UI-test fixture.
    static func previewSidebar() -> AppModel {
      var networks: [Network] = []
      var buffers: [Buffer] = []
      var firstChannelID: UUID?

      func addNetwork(
        _ name: String, sort: Int, status: String,
        channels: [(name: String, unread: Int, mentions: Int, joined: Bool)],
        queries: [String] = []
      ) {
        let netID = UUID()
        networks.append(
          Network(
            id: netID, name: name, kind: "irc", host: "irc.\(name.lowercased()).net",
            port: 6697, tls: true, nick: "shrike", status: status, sortOrder: sort))
        buffers.append(
          Buffer(
            id: UUID(), networkID: netID, name: name, kind: "status", joined: true,
            showEmbeds: false, showPresenceEvents: true, collapsePresenceEvents: false,
            pinned: false, unread: 0, mentions: 0))
        for channel in channels {
          let id = UUID()
          if firstChannelID == nil { firstChannelID = id }
          buffers.append(
            Buffer(
              id: id, networkID: netID, name: channel.name, kind: "channel", joined: channel.joined,
              showEmbeds: true, showPresenceEvents: true, collapsePresenceEvents: true,
              pinned: false, unread: channel.unread, mentions: channel.mentions))
        }
        for query in queries {
          buffers.append(
            Buffer(
              id: UUID(), networkID: netID, name: query, kind: "query", joined: true,
              showEmbeds: true, showPresenceEvents: true, collapsePresenceEvents: false,
              pinned: false, unread: 0, mentions: 0))
        }
      }

      addNetwork(
        "Libera", sort: 0, status: "connected",
        channels: [
          (name: "#general", unread: 0, mentions: 0, joined: true),
          (name: "#dev", unread: 3, mentions: 0, joined: true),
          (name: "#swift", unread: 0, mentions: 0, joined: true),
        ],
        queries: ["tove"])
      addNetwork(
        "OFTC", sort: 1, status: "connected",
        channels: [
          (name: "#tor", unread: 12, mentions: 2, joined: true),
          (name: "#debian", unread: 0, mentions: 0, joined: true),
        ])
      addNetwork(
        "Rizon", sort: 2, status: "connecting",
        channels: [
          (name: "#anime", unread: 99, mentions: 5, joined: true),
          (name: "#help", unread: 0, mentions: 0, joined: false),
        ])

      let model = AppModel(transport: FixtureTransport(), runsConnectionLoop: false)
      model.applySnapshot(
        StateSnapshot(networks: networks, buffers: buffers, initialMessages: [:], members: nil))
      model.serviceIdentity = FixtureTransport.identity
      model.connectionState = .connected
      model.hydrated = true
      model.selectedBufferID = firstChannelID
      return model
    }
  }
#endif

extension ProcessInfo {
  static var isPreview: Bool {
    isPreviewEnvironment(processInfo.environment)
  }

  static var isUITest: Bool {
    processInfo.arguments.contains("-ui-testing")
  }

  /// True in Xcode Previews or UI tests, where AppKit/UserNotifications APIs
  /// (`UNUserNotificationCenter.current()`, `NSApp.dockTile`) crash or misbehave.
  static var isPreviewOrUITest: Bool {
    isPreview || isUITest
  }

  static func isPreviewEnvironment(_ environment: [String: String]) -> Bool {
    environment["XCODE_RUNNING_FOR_PREVIEWS"] == "1"
      || environment["XCODE_RUNNING_FOR_PLAYGROUNDS"] == "1"
  }
}
