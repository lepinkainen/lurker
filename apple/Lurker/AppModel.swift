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
  // Buffers with the persisted archived flag (any kind), rendered inside the
  // folded Archives section at the bottom of the network.
  let archived: [Buffer]

  var all: [Buffer] {
    status + channels + queries + archived
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
    static let archivesOpen = "mac.archivesOpen"
    static let collapsedNetworks = "mac.collapsedNetworks"
  }

  var networks: [UUID: Network] = [:]
  var buffers: [UUID: Buffer] = [:]
  var messages: [UUID: [Message]] = [:]
  var members: [UUID: [Member]] = [:]
  var selectedBufferID: UUID?
  var historyExhausted: Set<UUID> = []
  var historyLoading: Set<UUID> = []
  var connectionState: ConnectionState = .notConfigured
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
  // Per-buffer sent-line history and drafts (in-memory, web parity).
  var inputHistory = InputHistory()
  // Per-network Archives fold state; folded by default, persisted across
  // launches like the other sidebar-adjacent Defaults.
  var archivesOpen: Set<UUID> = []
  // Per-network sidebar collapse; expanded by default, persisted.
  var collapsedNetworks: Set<UUID> = []
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
    // Opening a buffer never acks it: badge, divider, and unread bar clear
    // together only on an explicit ack (bar tap / Esc). See
    // ai-docs/behaviors/new-messages-marker.md.
    if let previous = selectedBufferID {
      inputHistory.stashDraft(composerText, buffer: previous)
    }
    selectedBufferID = id
    composerText = inputHistory.restoreDraft(buffer: id)
    composerError = nil
    defaults.set(id.uuidString, forKey: Defaults.selectedBuffer)
  }

  func setApplicationActive(_ active: Bool) {
    applicationActive = active
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

  func toggleArchives(_ networkID: UUID) {
    if !archivesOpen.insert(networkID).inserted {
      archivesOpen.remove(networkID)
    }
    defaults.set(archivesOpen.map(\.uuidString).sorted(), forKey: Defaults.archivesOpen)
  }

  func toggleNetworkCollapsed(_ networkID: UUID) {
    if !collapsedNetworks.insert(networkID).inserted {
      collapsedNetworks.remove(networkID)
    }
    defaults.set(
      collapsedNetworks.map(\.uuidString).sorted(), forKey: Defaults.collapsedNetworks)
  }

  /// Unread/mention totals across every buffer of a network (status, pinned,
  /// and archived included) for the collapsed-header badge, mirroring the
  /// web's collapsed-network aggregation.
  func networkAggregateCounts(_ networkID: UUID) -> (unread: Int, mentions: Int) {
    buffers.values.filter { $0.networkID == networkID }
      .reduce(into: (unread: 0, mentions: 0)) { acc, buffer in
        acc.unread += buffer.unread
        acc.mentions += buffer.mentions
      }
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

  func sendComposer() {
    guard let buffer = selectedBuffer else { return }
    let value = composerText.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty else { return }
    switch SlashCommands.parse(value, buffer: buffer) {
    case .invalid(let error):
      composerError = error
    case .command(let command):
      // Only plain messages enter arrow-up history; slash commands do not
      // (web parity: recordSentInput).
      if !value.hasPrefix("/") {
        inputHistory.record(value, buffer: buffer.id)
      }
      composerError = nil
      composerText = ""
      send(command)
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

  /// Reorder enabled networks via drag and drop. The backend requires the
  /// complete network id set, so disabled networks are appended in their
  /// current order. Optimistic: applies locally, rolls back on failure.
  @discardableResult
  func reorderNetworks(_ orderedEnabledIDs: [UUID]) -> Task<Void, Never>? {
    guard let transport else { return nil }
    let disabledIDs = orderedNetworks.filter(\.disabled).map(\.id)
    let ids = orderedEnabledIDs + disabledIDs
    let previous = networks
    for (index, id) in ids.enumerated() {
      networks[id]?.sortOrder = index
    }
    return Task {
      do {
        let updated = try await transport.reorderNetworks(ids: ids)
        for network in updated {
          // Keep live status: the REST response has no runtime state.
          var merged = network
          merged.status = networks[network.id]?.status
          networks[network.id] = merged
        }
      } catch {
        networks = previous
        composerError = error.localizedDescription
      }
    }
  }

  /// Reorder the visible (non-pinned, non-archived) channels of a network.
  /// Optimistic with rollback; the server broadcasts buffer_reorder to other
  /// clients and returns the same event shape here.
  @discardableResult
  func reorderChannels(networkID: UUID, orderedIDs: [UUID]) -> Task<Void, Never>? {
    guard let transport else { return nil }
    let previous = buffers
    for (index, id) in orderedIDs.enumerated() {
      buffers[id]?.sortOrder = index
    }
    return Task {
      do {
        let event = try await transport.reorderBuffers(networkID: networkID, ids: orderedIDs)
        apply(.bufferReorder(event))
      } catch {
        buffers = previous
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
    normalizedImageURL(preview.imageURL)
  }

  /// URL for a kind == "image" preview, where `url` itself is the image.
  /// Same policy as thumbnails: https-absolute or server-relative only (ATS
  /// blocks plain http regardless).
  func inlineImageURL(_ preview: Preview) -> URL? {
    normalizedImageURL(preview.url)
  }

  private func normalizedImageURL(_ raw: String?) -> URL? {
    guard let raw, !raw.isEmpty else { return nil }
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
    restoreSelection()
    updateBadge()
  }

  // Internal (not private) so unit tests can drive server events directly.
  func apply(_ event: ServerEvent) {
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
    case .bufferDeleted(let event):
      removeBuffer(event.id)
    case .bufferUpdate(let event):
      guard var buffer = buffers[event.id] else { return }
      if let topic = event.topic { buffer.topic = topic }
      if let joined = event.joined { buffer.joined = joined }
      if let archived = event.archived { buffer.archived = archived }
      if let lastSeenID = event.lastSeenID { buffer.lastSeenID = lastSeenID }
      // `marker_id` key present (mark_read variant): take it — inner nil means
      // caught up, which clears the marker. Key absent: unchanged.
      if let markerID = event.markerID {
        buffer.markerID = markerID
        buffer.markerTS = markerID == nil ? nil : event.markerTS
      }
      if let unread = event.unread { buffer.unread = unread }
      if let mentions = event.mentions { buffer.mentions = mentions }
      buffers[event.id] = buffer
      updateBadge()
    case .bufferSettings(let event):
      apply(event)
    case .bufferReorder(let event):
      for entry in event.buffers {
        buffers[entry.id]?.sortOrder = entry.sortOrder
      }
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
    case .channelList(let event):
      // Web parity (channel-list.ts): a result for a different network starts
      // fresh; entries accumulate in case the server ever streams batches.
      if var current = channelList, current.networkID == event.networkID, !current.done {
        current = ChannelListEvent(
          networkID: event.networkID,
          entries: (current.entries ?? []) + (event.entries ?? []),
          done: event.done)
        channelList = current
      } else {
        channelList = event
      }
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
    buffer.archived = event.archived
    buffers[event.id] = buffer
  }

  /// Handles a buffer_deleted broadcast: drop the buffer and all per-buffer
  /// state; if it was selected, fall back like at startup.
  private func removeBuffer(_ id: UUID) {
    guard buffers.removeValue(forKey: id) != nil else { return }
    messages.removeValue(forKey: id)
    members.removeValue(forKey: id)
    historyExhausted.remove(id)
    historyLoading.remove(id)
    if selectedBufferID == id {
      selectedBufferID = nil
      restoreSelection()
    }
    updateBadge()
  }

  private func apply(_ message: Message) {
    let wasKnown = messages[message.bufferID]?.contains(where: { $0.id == message.id }) == true
    mergeMessages([message], into: message.bufferID)
    guard !wasKnown, var buffer = buffers[message.bufferID] else { return }

    // Unread bookkeeping applies to every buffer, including the selected one
    // while the app is active — viewing never acks. Server-authoritative
    // counts arrive on buffer_update / snapshot; this keeps badges live
    // between syncs.
    guard message.countsAsUnread == true, message.isSelf != true else { return }
    let isUnseen = buffer.lastSeenID.map { message.id.uuidString > $0.uuidString } ?? true
    guard isUnseen else { return }

    if buffer.markerID == nil {
      buffer.markerID = message.id
      buffer.markerTS = message.ts
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

  private func mergeMessages(_ incoming: [Message], into bufferID: UUID) {
    var byID = Dictionary(uniqueKeysWithValues: (messages[bufferID] ?? []).map { ($0.id, $0) })
    for message in incoming {
      byID[message.id] = message
    }
    messages[bufferID] = byID.values.sorted(by: messageOrder)
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

  private func restoreSelection() {
    if let selectedBufferID, buffers[selectedBufferID] != nil {
      return
    }
    selectedBufferID = sidebarBufferOrder().first
  }

  private func sidebarBufferOrder() -> [UUID] {
    var result = pinnedBuffers.map(\.id)
    for network in orderedNetworks where !network.disabled {
      let groups = sidebarBuffers(for: network.id)
      // Collapsed networks keep only their status buffer navigable (the
      // header still represents it), mirroring the web's visible order.
      if collapsedNetworks.contains(network.id) {
        result.append(contentsOf: groups.status.map(\.id))
        continue
      }
      var ids = (groups.status + groups.channels + groups.queries).map(\.id)
      // Folded archives are invisible; keyboard navigation and selection
      // restore skip them (mirrors the web's visible-sidebar order).
      if archivesOpen.contains(network.id) {
        ids.append(contentsOf: groups.archived.map(\.id))
      }
      result.append(contentsOf: ids)
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
      // Channels honor manual ordering (sortOrder, then name); other groups
      // stay purely alphabetical.
      channels: values.filter { $0.kind == "channel" && !$0.archived }.sorted(by: channelOrder),
      queries: values.filter { $0.kind == "query" && !$0.archived }.sorted(by: bufferOrder),
      archived: values.filter { $0.kind != "status" && $0.archived }.sorted(by: bufferOrder)
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
    historyExhausted.removeAll()
    channelList = nil
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

private func channelOrder(_ lhs: Buffer, _ rhs: Buffer) -> Bool {
  lhs.sortOrder == rhs.sortOrder ? bufferOrder(lhs, rhs) : lhs.sortOrder < rhs.sortOrder
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
        // Parted channels double as archived fixtures (server archives on part).
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
              pinned: false, archived: !channel.joined, unread: channel.unread,
              mentions: channel.mentions))
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
