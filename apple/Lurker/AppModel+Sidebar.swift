import Foundation

extension AppModel {

  // MARK: Internal

  var orderedNetworks: [Network] {
    networks.values.sorted {
      $0.sortOrder == $1.sortOrder
        ? $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
        : $0.sortOrder < $1.sortOrder
    }
  }

  var pinnedBuffers: [Buffer] {
    buffers.values
      .filter { $0.pinned && $0.kind == "channel" }
      .sorted(by: pinnedOrder)
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
      collapsedNetworks.map(\.uuidString).sorted(),
      forKey: Defaults.collapsedNetworks,
    )
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

  /// Reorder enabled networks via drag and drop. The backend requires the
  /// complete network id set, so disabled networks are appended in their
  /// current order. Optimistic: applies locally, rolls back on failure.
  @discardableResult
  func reorderNetworks(_ orderedEnabledIDs: [UUID]) -> Task<Void, Never>? {
    guard let transport else { return nil }
    let disabledIDs = orderedNetworks.filter(\.disabled).map(\.id)
    let ids = orderedEnabledIDs + disabledIDs
    // Snapshot only what the optimistic update touches: rolling back a full
    // dictionary copy would wipe WS updates applied while the POST is in
    // flight.
    let previous = ids.compactMap { id in networks[id].map { (id, $0.sortOrder) } }
    for (index, id) in ids.enumerated() {
      networks[id]?.sortOrder = index
    }
    return Task {
      do {
        let updated = try await transport.reorderNetworks(ids: ids)
        for network in updated {
          // The response carries a fresh status snapshot; fall back to the
          // local value only when the server omitted it.
          var merged = network
          merged.status = network.status ?? networks[network.id]?.status
          networks[network.id] = merged
        }
      } catch {
        for (id, sortOrder) in previous {
          networks[id]?.sortOrder = sortOrder
        }
        composerError = error.localizedDescription
      }
    }
  }

  /// Reorder the visible (non-archived) channels of a network.
  /// Optimistic with rollback; the server broadcasts buffer_reorder to other
  /// clients and returns the same event shape here.
  @discardableResult
  func reorderChannels(networkID: UUID, orderedIDs: [UUID]) -> Task<Void, Never>? {
    guard let transport else { return nil }
    // Field-level snapshot, same reasoning as reorderNetworks.
    let previous = orderedIDs.compactMap { id in buffers[id].map { (id, $0.sortOrder) } }
    for (index, id) in orderedIDs.enumerated() {
      buffers[id]?.sortOrder = index
    }
    return Task {
      do {
        let event = try await transport.reorderBuffers(networkID: networkID, ids: orderedIDs)
        apply(.bufferReorder(event))
      } catch {
        for (id, sortOrder) in previous {
          buffers[id]?.sortOrder = sortOrder
        }
        composerError = error.localizedDescription
      }
    }
  }

  func nextBuffer(unreadOnly: Bool = false, mentionsOnly: Bool = false, direction: Int = 1) {
    let order = sidebarBufferOrder()
    var candidates = order
    if unreadOnly {
      candidates = candidates.filter { buffers[$0]?.unread ?? 0 > 0 }
    }
    if mentionsOnly {
      candidates = candidates.filter { buffers[$0]?.mentions ?? 0 > 0 }
    }
    guard !candidates.isEmpty else { return }
    guard let selected = selectedBufferID, let pos = order.firstIndex(of: selected) else {
      selectBuffer(direction > 0 ? candidates.first! : candidates.last!)
      return
    }
    // Selected buffer may not itself be a candidate (e.g. it has no unread
    // while navigating unread-only): walk relative to its sidebar position
    // rather than its (nonexistent) index within `candidates`, so up/down
    // land on the nearest candidate above/below rather than wrapping to the
    // global first/last.
    let position = Dictionary(uniqueKeysWithValues: order.enumerated().map { ($1, $0) })
    let next =
      direction > 0
        ? (candidates.first { (position[$0] ?? Int.max) > pos } ?? candidates.first!)
        : (candidates.last { (position[$0] ?? Int.min) < pos } ?? candidates.last!)
    if next != selected {
      selectBuffer(next)
    }
  }

  func focusStatusBuffer() {
    guard
      let networkID = selectedBuffer?.networkID,
      let status = buffers.values.first(where: { $0.networkID == networkID && $0.kind == "status" })
    else {
      return
    }
    selectBuffer(status.id)
  }

  /// Reorder the pinned section via drag and drop. Optimistic with rollback;
  /// the server broadcasts pinned_reorder to other clients and returns the
  /// same event shape here.
  @discardableResult
  func reorderPinnedBuffers(_ orderedIDs: [UUID]) -> Task<Void, Never>? {
    guard let transport else { return nil }
    // Field-level snapshot, same reasoning as reorderNetworks.
    let previous = orderedIDs.compactMap { id in buffers[id].map { (id, $0.pinOrder) } }
    for (index, id) in orderedIDs.enumerated() {
      buffers[id]?.pinOrder = index
    }
    return Task {
      do {
        let event = try await transport.reorderPinnedBuffers(ids: orderedIDs)
        apply(.pinnedReorder(event))
      } catch {
        for (id, pinOrder) in previous {
          buffers[id]?.pinOrder = pinOrder
        }
        composerError = error.localizedDescription
      }
    }
  }

  func sidebarBuffers(for networkID: UUID) -> SidebarBufferGroups {
    // Pinned channels stay listed under their network in addition to the
    // Pinned section.
    let values = buffers.values.filter { $0.networkID == networkID }
    return SidebarBufferGroups(
      status: values.filter { $0.kind == "status" }.sorted(by: bufferOrder),
      // Channels honor manual ordering (sortOrder, then name); other groups
      // stay purely alphabetical.
      channels: values.filter { $0.kind == "channel" && !$0.archived }.sorted(by: channelOrder),
      queries: values.filter { $0.kind == "query" && !$0.archived }.sorted(by: bufferOrder),
      archived: values.filter { $0.kind != "status" && $0.archived }.sorted(by: bufferOrder),
    )
  }

  func restoreSelection() {
    if let selectedBufferID, buffers[selectedBufferID] != nil {
      return
    }
    guard let fallback = sidebarBufferOrder().first else {
      selectedBufferID = nil
      composerText = ""
      return
    }
    applySelection(fallback)
  }

  // MARK: Private

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

}

private func bufferOrder(_ lhs: Buffer, _ rhs: Buffer) -> Bool {
  lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
}

private func channelOrder(_ lhs: Buffer, _ rhs: Buffer) -> Bool {
  lhs.sortOrder == rhs.sortOrder ? bufferOrder(lhs, rhs) : lhs.sortOrder < rhs.sortOrder
}

private func pinnedOrder(_ lhs: Buffer, _ rhs: Buffer) -> Bool {
  lhs.pinOrder == rhs.pinOrder ? bufferOrder(lhs, rhs) : lhs.pinOrder < rhs.pinOrder
}
