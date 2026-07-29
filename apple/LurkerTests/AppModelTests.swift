import Foundation
import Testing

@testable import Lurker

@Suite
@MainActor
struct AppModelTests {
  @Test func previewUsesSynchronousConnectedFixture() {
    let model = AppModel.preview()

    #expect(model.connectionState == .connected)
    #expect(model.networks[FixtureTransport.networkID]?.status == "connected")
    #expect(model.selectedBufferID == FixtureTransport.channelID)

    model.start()

    #expect(model.connectionState == .connected)
  }

  @Test func recognizesCurrentAndLegacyXcodePreviewEnvironments() {
    #expect(
      ProcessInfo.isPreviewEnvironment([
        "XCODE_RUNNING_FOR_PLAYGROUNDS": "1"
      ]))
    #expect(
      ProcessInfo.isPreviewEnvironment([
        "XCODE_RUNNING_FOR_PREVIEWS": "1"
      ]))
    #expect(!ProcessInfo.isPreviewEnvironment([:]))
  }

  @Test func sidebarAndKeyboardNavigationShareOneOrder() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let status = buffer("*status*", kind: "status", networkID: networkID)
    let pinned = buffer("#zeta", networkID: networkID, pinned: true)
    let joined = buffer("#alpha", networkID: networkID)
    let query = buffer("bob", kind: "query", networkID: networkID)
    let notJoined = buffer("#old", networkID: networkID, joined: false)

    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [status, pinned, joined, query, notJoined].map { ($0.id, $0) })

    #expect(model.pinnedBuffers.map(\.id) == [pinned.id])
    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.all.map(\.id) == [status.id, joined.id, notJoined.id, query.id])

    model.selectedBufferID = status.id
    model.nextBuffer()
    #expect(model.selectedBufferID == joined.id)
    model.nextBuffer()
    #expect(model.selectedBufferID == notJoined.id)
    model.nextBuffer()
    #expect(model.selectedBufferID == query.id)
  }

  // Archived buffers (any kind) leave the channel/query groups for the
  // archived bucket; keyboard navigation skips them while the fold is closed
  // and includes them once opened.
  @Test func archivedBuffersFoldOutOfGroupsAndNavigation() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let status = buffer("*status*", kind: "status", networkID: networkID)
    let active = buffer("#alpha", networkID: networkID)
    let query = buffer("bob", kind: "query", networkID: networkID)
    let archivedChannel = buffer("#old", networkID: networkID, joined: false, archived: true)
    let archivedQuery = buffer("spammer", kind: "query", networkID: networkID, archived: true)

    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [status, active, query, archivedChannel, archivedQuery].map {
        ($0.id, $0)
      })

    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.channels.map(\.id) == [active.id])
    #expect(groups.queries.map(\.id) == [query.id])
    #expect(groups.archived.map(\.id) == [archivedChannel.id, archivedQuery.id])

    // Folded (default): navigation cycles without entering the archives.
    model.selectedBufferID = query.id
    model.nextBuffer()
    #expect(model.selectedBufferID == status.id)

    // Open: archived buffers become reachable after the queries.
    model.toggleArchives(networkID)
    model.selectedBufferID = query.id
    model.nextBuffer()
    #expect(model.selectedBufferID == archivedChannel.id)
    model.nextBuffer()
    #expect(model.selectedBufferID == archivedQuery.id)
  }

  @Test func bufferDeletedDropsStateAndReselects() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let doomed = buffer("spammer", kind: "query", networkID: networkID, archived: true)
    let survivor = buffer("#alpha", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [doomed.id: doomed, survivor.id: survivor]
    model.messages[doomed.id] = [message(1, in: doomed, networkID: networkID)]
    model.members[doomed.id] = []
    model.selectedBufferID = doomed.id

    model.apply(.bufferDeleted(BufferDeletedEvent(id: doomed.id, networkID: networkID)))

    #expect(model.buffers[doomed.id] == nil)
    #expect(model.messages[doomed.id] == nil)
    #expect(model.members[doomed.id] == nil)
    #expect(model.selectedBufferID == survivor.id)
  }

  @Test func bufferUpdateArchivedMovesBufferBetweenGroups() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer("#alpha", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]

    let archived = try? JSONDecoder.lurker().decode(
      ServerEvent.self,
      from: Data(
        """
        {"type":"buffer_update","id":"\(target.id.uuidString)",
         "network_id":"\(networkID.uuidString)","joined":false,"archived":true}
        """.utf8))
    #expect(archived != nil)
    if let archived { model.apply(archived) }

    #expect(model.buffers[target.id]?.archived == true)
    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.channels.isEmpty)
    #expect(groups.archived.map(\.id) == [target.id])
  }

  @Test func archivesFoldStatePersistsAcrossRelaunch() {
    let defaults = isolatedDefaults()
    let networkID = UUID()
    let first = AppModel(transport: FixtureTransport(), defaults: defaults)
    first.toggleArchives(networkID)

    let second = AppModel(transport: FixtureTransport(), defaults: defaults)
    #expect(second.archivesOpen.contains(networkID))
  }

  @Test func networkCollapseStatePersistsAcrossRelaunch() {
    let defaults = isolatedDefaults()
    let networkID = UUID()
    let first = AppModel(transport: FixtureTransport(), defaults: defaults)
    first.toggleNetworkCollapsed(networkID)

    let second = AppModel(transport: FixtureTransport(), defaults: defaults)
    #expect(second.collapsedNetworks.contains(networkID))

    second.toggleNetworkCollapsed(networkID)
    let third = AppModel(transport: FixtureTransport(), defaults: defaults)
    #expect(!third.collapsedNetworks.contains(networkID))
  }

  // A collapsed network keeps only its status buffer in the navigation order:
  // the header still represents the status buffer, channels/queries are hidden.
  @Test func collapsedNetworkNavigatesOnlyToStatus() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let status = buffer("*status*", kind: "status", networkID: networkID)
    let channel = buffer("#alpha", networkID: networkID)
    let query = buffer("bob", kind: "query", networkID: networkID)
    let otherNetworkID = UUID()
    let otherStatus = buffer("*status*", kind: "status", networkID: otherNetworkID)

    model.networks[networkID] = network(id: networkID)
    model.networks[otherNetworkID] = network(id: otherNetworkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [status, channel, query, otherStatus].map { ($0.id, $0) })

    model.toggleNetworkCollapsed(networkID)
    model.selectedBufferID = status.id
    model.nextBuffer()
    #expect(model.selectedBufferID == otherStatus.id)

    model.toggleNetworkCollapsed(networkID)
    model.selectedBufferID = status.id
    model.nextBuffer()
    #expect(model.selectedBufferID == channel.id)
  }

  @Test func channelsSortBySortOrderThenName() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    var alpha = buffer("#alpha", networkID: networkID)
    var beta = buffer("#beta", networkID: networkID)
    let gamma = buffer("#gamma", networkID: networkID)
    alpha.sortOrder = 2
    beta.sortOrder = 1
    // gamma keeps default 0: sorts first, ties would fall back to name.
    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [alpha, beta, gamma].map { ($0.id, $0) })

    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.channels.map(\.id) == [gamma.id, beta.id, alpha.id])
  }

  @Test func bufferDecodesWithoutSortOrderAsZero() throws {
    // Old backends omit sort_order; the client must default to 0 so ordering
    // stays alphabetical (version-skew guard).
    let json = """
      {"id":"\(UUID().uuidString)","network_id":"\(UUID().uuidString)","name":"#a",
       "kind":"channel","joined":true,"created_at":"2026-01-01T00:00:00Z",
       "show_embeds":true,"show_presence_events":true,"collapse_presence_events":false,
       "pinned":false,"archived":false,"unread":0,"mentions":0}
      """
    let decoded = try JSONDecoder.lurker().decode(Buffer.self, from: Data(json.utf8))
    #expect(decoded.sortOrder == 0)
  }

  @Test func applyBufferReorderUpdatesSortOrders() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let alpha = buffer("#alpha", networkID: networkID)
    let beta = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(uniqueKeysWithValues: [alpha, beta].map { ($0.id, $0) })

    model.apply(
      .bufferReorder(
        BufferReorderEvent(
          networkID: networkID,
          buffers: [
            BufferSortEntry(id: beta.id, sortOrder: 0),
            BufferSortEntry(id: alpha.id, sortOrder: 1),
          ])))

    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.channels.map(\.id) == [beta.id, alpha.id])
  }

  @Test func reorderChannelsRollsBackOnFailure() async {
    let transport = FixtureTransport()
    await transport.setFailReorders(true)
    let model = AppModel(transport: transport, defaults: isolatedDefaults())
    let networkID = UUID()
    let alpha = buffer("#alpha", networkID: networkID)
    let beta = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(uniqueKeysWithValues: [alpha, beta].map { ($0.id, $0) })

    await model.reorderChannels(networkID: networkID, orderedIDs: [beta.id, alpha.id])?.value

    #expect(model.buffers[beta.id]?.sortOrder == 0)
    #expect(model.buffers[alpha.id]?.sortOrder == 0)
    #expect(model.composerError != nil)
  }

  @Test func reorderChannelsAppliesServerOrder() async {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let alpha = buffer("#alpha", networkID: networkID)
    let beta = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(uniqueKeysWithValues: [alpha, beta].map { ($0.id, $0) })

    await model.reorderChannels(networkID: networkID, orderedIDs: [beta.id, alpha.id])?.value

    #expect(model.buffers[beta.id]?.sortOrder == 0)
    #expect(model.buffers[alpha.id]?.sortOrder == 1)
    #expect(model.composerError == nil)
  }

  @Test func reorderNetworksSendsCompleteSetIncludingDisabled() async {
    let transport = FixtureTransport()
    let model = AppModel(transport: transport, defaults: isolatedDefaults())
    let firstID = UUID()
    let secondID = UUID()
    let disabledID = UUID()
    var first = network(id: firstID)
    var second = network(id: secondID)
    var off = network(id: disabledID)
    first.sortOrder = 0
    second.sortOrder = 1
    off.sortOrder = 2
    off.disabled = true
    model.networks = [firstID: first, secondID: second, disabledID: off]

    await model.reorderNetworks([secondID, firstID])?.value

    let sent = await transport.reorderedNetworkIDs
    #expect(sent == [secondID, firstID, disabledID])
    #expect(model.networks[secondID]?.sortOrder == 0)
    #expect(model.networks[firstID]?.sortOrder == 1)
  }

  @Test func reorderRollbackPreservesInFlightUpdates() async {
    let transport = FixtureTransport()
    await transport.setFailReorders(true)
    let model = AppModel(transport: transport, defaults: isolatedDefaults())
    let networkID = UUID()
    let alpha = buffer("#alpha", networkID: networkID)
    let beta = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(uniqueKeysWithValues: [alpha, beta].map { ($0.id, $0) })

    let task = model.reorderChannels(networkID: networkID, orderedIDs: [beta.id, alpha.id])
    // A WS update lands while the reorder POST is in flight; the failure
    // rollback must revert only sortOrder, not this.
    let update = try? JSONDecoder.lurker().decode(
      ServerEvent.self,
      from: Data(
        """
        {"type":"buffer_update","id":"\(alpha.id.uuidString)",
         "network_id":"\(networkID.uuidString)","unread":7}
        """.utf8))
    #expect(update != nil)
    if let update { model.apply(update) }
    await task?.value

    #expect(model.buffers[alpha.id]?.unread == 7)
    #expect(model.buffers[alpha.id]?.sortOrder == 0)
    #expect(model.buffers[beta.id]?.sortOrder == 0)
  }

  @Test func deletingSelectedBufferDoesNotLeakDraft() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let doomed = buffer("spammer", kind: "query", networkID: networkID, archived: true)
    let survivor = buffer("#alpha", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [doomed.id: doomed, survivor.id: survivor]
    model.selectBuffer(doomed.id)
    model.composerText = "half-typed message"

    model.apply(.bufferDeleted(BufferDeletedEvent(id: doomed.id, networkID: networkID)))

    #expect(model.selectedBufferID == survivor.id)
    #expect(model.composerText == "")
  }

  @Test func reorderNetworksRollsBackOnFailure() async {
    let transport = FixtureTransport()
    await transport.setFailReorders(true)
    let model = AppModel(transport: transport, defaults: isolatedDefaults())
    let firstID = UUID()
    let secondID = UUID()
    var first = network(id: firstID)
    var second = network(id: secondID)
    first.sortOrder = 0
    second.sortOrder = 1
    model.networks = [firstID: first, secondID: second]

    await model.reorderNetworks([secondID, firstID])?.value

    #expect(model.networks[firstID]?.sortOrder == 0)
    #expect(model.networks[secondID]?.sortOrder == 1)
    #expect(model.composerError != nil)
  }

  @Test func sendComposerRecordsPlainMessagesNotSlashCommands() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let chan = buffer("#alpha", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [chan.id: chan]
    model.selectedBufferID = chan.id

    model.composerText = "hello there"
    model.sendComposer()
    model.composerText = "/me waves"
    model.sendComposer()

    #expect(model.navigateHistory(up: true))
    #expect(model.composerText == "hello there")
    #expect(!model.navigateHistory(up: true) || model.composerText == "hello there")
  }

  @Test func bufferSwitchPreservesDraftsPerBuffer() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let first = buffer("#alpha", networkID: networkID)
    let second = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [first.id: first, second.id: second]

    model.selectBuffer(first.id)
    model.composerText = "draft in alpha"
    model.selectBuffer(second.id)
    #expect(model.composerText == "")
    model.composerText = "draft in beta"
    model.selectBuffer(first.id)
    #expect(model.composerText == "draft in alpha")
    model.selectBuffer(second.id)
    #expect(model.composerText == "draft in beta")
  }

  @Test func inlineImageURLAcceptsHTTPSOnly() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let secure = Preview(url: "https://example.com/cat.png", kind: "image")
    let insecure = Preview(url: "http://example.com/cat.png", kind: "image")
    #expect(model.inlineImageURL(secure)?.absoluteString == "https://example.com/cat.png")
    #expect(model.inlineImageURL(insecure) == nil)
  }

  @Test func networkAggregateCountsSumAllBuffers() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let status = buffer("*status*", kind: "status", networkID: networkID, unread: 1)
    let pinned = buffer("#pinned", networkID: networkID, pinned: true, unread: 2, mentions: 1)
    let channel = buffer("#alpha", networkID: networkID, unread: 3)
    let archived = buffer(
      "#old", networkID: networkID, joined: false, archived: true, unread: 4, mentions: 2)
    let foreign = buffer("#other", networkID: UUID(), unread: 100, mentions: 100)

    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [status, pinned, channel, archived, foreign].map { ($0.id, $0) })

    let counts = model.networkAggregateCounts(networkID)
    #expect(counts.unread == 10)
    #expect(counts.mentions == 3)
  }

  @Test func selectBufferPushesCompactConversation() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let channel = buffer("#alpha", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [channel.id: channel]

    #expect(!model.compactConversationVisible)
    model.selectBuffer(channel.id)
    #expect(model.compactConversationVisible)

    // After back-navigation pops, reselecting the same buffer must push again.
    model.compactConversationVisible = false
    model.selectBuffer(channel.id)
    #expect(model.compactConversationVisible)
  }

  @Test func inspectorDismissalPersistsAcrossRelaunch() {
    let defaults = isolatedDefaults()
    let first = AppModel(transport: FixtureTransport(), defaults: defaults)
    first.setInspectorVisible(false)

    let second = AppModel(transport: FixtureTransport(), defaults: defaults)
    #expect(!second.inspectorVisible)
  }

  // Opening a buffer, switching away from it, or foregrounding the app must
  // never ack: badge, divider, and unread bar clear only on explicit ack.
  @Test func selectingAndForegroundingNeverMarkRead() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let markerID = uuid(5)
    let unreadBuffer = buffer(
      "#alpha", networkID: networkID, markerID: markerID, markerTS: "2026-07-23T08:12:00Z",
      unread: 3, mentions: 1)
    let other = buffer("#beta", networkID: networkID)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [unreadBuffer.id: unreadBuffer, other.id: other]
    model.messages[unreadBuffer.id] = [message(5, in: unreadBuffer, networkID: networkID)]

    model.selectBuffer(unreadBuffer.id)
    model.setApplicationActive(false)
    model.setApplicationActive(true)
    model.selectBuffer(other.id)

    let after = model.buffers[unreadBuffer.id]
    #expect(after?.unread == 3)
    #expect(after?.mentions == 1)
    #expect(after?.markerID == markerID)
    #expect(after?.lastSeenID == nil)
  }

  @Test func channelListEventPresentsAndAccumulates() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()

    model.apply(
      .channelList(
        ChannelListEvent(
          networkID: networkID,
          entries: [ChannelListEntry(name: "#go-nuts", count: 412, topic: "Go talk")],
          done: false)))
    #expect(model.channelList?.entries?.count == 1)
    #expect(model.channelList?.done == false)

    // A second batch for the same in-flight list accumulates.
    model.apply(
      .channelList(
        ChannelListEvent(
          networkID: networkID,
          entries: [ChannelListEntry(name: "#quiet", count: 2)],
          done: true)))
    #expect(model.channelList?.entries?.count == 2)
    #expect(model.channelList?.done == true)

    // A result for another network replaces the finished one.
    let otherNetwork = UUID()
    model.apply(
      .channelList(ChannelListEvent(networkID: otherNetwork, entries: nil, done: true)))
    #expect(model.channelList?.networkID == otherNetwork)
    #expect(model.channelList?.entries == nil)
  }

  @Test func ackReadClearsMarkerAndCountsAndAdvancesLastSeen() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer(
      "#alpha", networkID: networkID, markerID: uuid(2), markerTS: "2026-07-23T08:12:00Z",
      unread: 2, mentions: 1)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]
    model.messages[target.id] = [
      message(1, in: target, networkID: networkID),
      message(2, in: target, networkID: networkID),
      message(3, in: target, networkID: networkID),
    ]

    model.ackRead(target.id)

    let after = model.buffers[target.id]
    #expect(after?.lastSeenID == uuid(3))
    #expect(after?.markerID == nil)
    #expect(after?.markerTS == nil)
    #expect(after?.unread == 0)
    #expect(after?.mentions == 0)
  }

  @Test func ackReadWithoutLoadedMessagesIsANoOp() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer(
      "#alpha", networkID: networkID, markerID: uuid(2), unread: 2, mentions: 1)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]

    model.ackRead(target.id)

    #expect(model.buffers[target.id]?.markerID == uuid(2))
    #expect(model.buffers[target.id]?.unread == 2)
  }

  // Live arrival counts in every buffer — including the selected one while the
  // app is active — and the marker sticks to the first unread message.
  @Test func liveArrivalCountsUnreadEvenInSelectedActiveBuffer() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer("#alpha", networkID: networkID, lastSeenID: uuid(1))
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]
    model.selectBuffer(target.id)
    model.setApplicationActive(true)

    let first = message(2, in: target, networkID: networkID, ts: "2026-07-23T08:13:00Z")
    model.apply(.message(first))
    model.apply(.message(message(3, in: target, networkID: networkID, mentionsMe: true)))

    let after = model.buffers[target.id]
    #expect(after?.unread == 2)
    #expect(after?.mentions == 1)
    #expect(after?.markerID == first.id)
    #expect(after?.markerTS == first.ts)
    #expect(after?.lastSeenID == uuid(1))
  }

  @Test func selfSeenAndNonCountingMessagesNeverCount() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer("#alpha", networkID: networkID, lastSeenID: uuid(5))
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]

    // Self-authored, already-seen (id <= last_seen_id), and non-counting kinds.
    model.apply(.message(message(6, in: target, networkID: networkID, isSelf: true)))
    model.apply(.message(message(4, in: target, networkID: networkID)))
    model.apply(.message(message(7, in: target, networkID: networkID, countsAsUnread: false)))

    let after = model.buffers[target.id]
    #expect(after?.unread == 0)
    #expect(after?.markerID == nil)
    #expect(model.messages[target.id]?.count == 3)
  }

  // A remote ack arrives as buffer_update with marker_id: null — it must clear
  // the marker here too; an update without the key leaves the marker alone.
  @Test func bufferUpdateAppliesMarkerPresenceSemantics() throws {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let target = buffer(
      "#alpha", networkID: networkID, markerID: uuid(2), markerTS: "2026-07-23T08:12:00Z",
      unread: 2, mentions: 1)
    model.networks[networkID] = network(id: networkID)
    model.buffers = [target.id: target]

    let unchanged = try JSONDecoder.lurker().decode(
      ServerEvent.self,
      from: Data(
        """
        {"type":"buffer_update","id":"\(target.id.uuidString)",
         "network_id":"\(networkID.uuidString)","topic":"still unread"}
        """.utf8))
    model.apply(unchanged)
    #expect(model.buffers[target.id]?.markerID == uuid(2))
    #expect(model.buffers[target.id]?.topic == "still unread")

    let cleared = try JSONDecoder.lurker().decode(
      ServerEvent.self,
      from: Data(
        """
        {"type":"buffer_update","id":"\(target.id.uuidString)",
         "network_id":"\(networkID.uuidString)",
         "last_seen_id":"\(uuid(3).uuidString)",
         "marker_id":null,"unread":0,"mentions":0}
        """.utf8))
    model.apply(cleared)
    let after = model.buffers[target.id]
    #expect(after?.markerID == nil)
    #expect(after?.markerTS == nil)
    #expect(after?.lastSeenID == uuid(3))
    #expect(after?.unread == 0)
    #expect(after?.mentions == 0)
  }

  @Test func snapshotHydratesMarkerFields() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    model.applySnapshot(FixtureTransport.snapshot())

    let channel = model.buffers[FixtureTransport.channelID]
    #expect(channel?.markerID == UUID(uuidString: "0198F5F2-A000-7000-8000-000000000001"))
    #expect(channel?.markerTS == "2026-07-23T08:12:00Z")

    let full = model.buffers[FixtureTransport.fullChannelID]
    let firstUnread = FixtureTransport.fullMessages[
      FixtureTransport.fullTotal - FixtureTransport.fullUnread]
    #expect(full?.markerID == firstUnread.id)
    #expect(full?.markerTS == firstUnread.ts)
  }

  @Test func freshSnapshotResetsPaginationState() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let bufferID = UUID()
    model.historyExhausted.insert(bufferID)
    model.historyLoading.insert(bufferID)

    model.applySnapshot(
      StateSnapshot(networks: [], buffers: [], initialMessages: [:], members: [:]))

    #expect(model.historyExhausted.isEmpty)
    #expect(model.historyLoading.isEmpty)
  }

  private func isolatedDefaults() -> UserDefaults {
    UserDefaults(suiteName: "xyz.endymion.lurker.tests.\(UUID().uuidString)")!
  }

  private func network(id: UUID) -> Network {
    Network(
      id: id,
      name: "Libera",
      kind: "irc",
      host: "irc.libera.chat",
      port: 6697,
      tls: true,
      nick: "shrike",
      sortOrder: 0
    )
  }

  private func buffer(
    _ name: String,
    kind: String = "channel",
    networkID: UUID,
    joined: Bool = true,
    pinned: Bool = false,
    archived: Bool = false,
    lastSeenID: UUID? = nil,
    markerID: UUID? = nil,
    markerTS: String? = nil,
    unread: Int = 0,
    mentions: Int = 0
  ) -> Buffer {
    Buffer(
      id: UUID(),
      networkID: networkID,
      name: name,
      kind: kind,
      joined: joined,
      lastSeenID: lastSeenID,
      markerID: markerID,
      markerTS: markerTS,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: pinned,
      archived: archived,
      unread: unread,
      mentions: mentions
    )
  }

  /// Deterministic UUIDs whose lexicographic `uuidString` order matches the
  /// numeric order, mirroring the server's sortable message ids.
  private func uuid(_ ordinal: Int) -> UUID {
    UUID(uuidString: String(format: "0198F5F3-B000-7000-8000-%012X", ordinal))!
  }

  private func message(
    _ ordinal: Int,
    in buffer: Buffer,
    networkID: UUID,
    ts: String = "2026-07-23T08:12:00Z",
    isSelf: Bool? = nil,
    mentionsMe: Bool? = nil,
    countsAsUnread: Bool? = true
  ) -> Message {
    Message(
      id: uuid(ordinal),
      networkID: networkID,
      bufferID: buffer.id,
      ts: ts,
      sender: isSelf == true ? "shrike" : "tove",
      kind: "privmsg",
      content: "line \(ordinal)",
      displayKind: "message",
      isSelf: isSelf,
      mentionsMe: mentionsMe,
      countsAsUnread: countsAsUnread
    )
  }
}
