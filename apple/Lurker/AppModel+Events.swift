import Foundation

extension AppModel {

  // MARK: Internal

  func applySnapshot(_ snapshot: StateSnapshot) {
    historyExhausted.removeAll(keepingCapacity: true)
    historyLoading.removeAll(keepingCapacity: true)
    historyAnchor = nil
    networks = Dictionary(uniqueKeysWithValues: snapshot.networks.map { ($0.id, $0) })
    buffers = Dictionary(uniqueKeysWithValues: snapshot.buffers.map { ($0.id, $0) })
    messages = Dictionary(
      uniqueKeysWithValues: snapshot.initialMessages.compactMap { key, value in
        UUID(uuidString: key).map { ($0, value.sorted(by: messageOrder)) }
      }
    )
    members = Dictionary(
      uniqueKeysWithValues: (snapshot.members ?? [:]).compactMap { key, value in
        UUID(uuidString: key).map { ($0, value) }
      }
    )
    for (bufferID, list) in members {
      if let networkID = buffers[bufferID]?.networkID {
        noteBots(list, networkID: networkID)
      }
    }
    restoreSelection()
    updateBadge()
  }

  /// Internal (not private) so unit tests can drive server events directly.
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
          // Status windows carry server-generated content; no link previews.
          showEmbeds: event.kind != "status",
          showPresenceEvents: true,
          collapsePresenceEvents: false,
          pinned: false,
          sortOrder: event.sortOrder ?? 0,
          unread: 0,
          mentions: 0,
        )
      }

    case .bufferDeleted(let event):
      removeBuffer(event.id)

    case .bufferUpdate(let event):
      guard var buffer = buffers[event.id] else { return }
      if let topic = event.topic {
        buffer.topic = topic
      }
      if let joined = event.joined {
        buffer.joined = joined
      }
      if let archived = event.archived {
        buffer.archived = archived
      }
      if let lastSeenID = event.lastSeenID {
        buffer.lastSeenID = lastSeenID
      }
      // `marker_id` key present (mark_read variant): take it — inner nil means
      // caught up, which clears the marker. Key absent: unchanged.
      if let markerID = event.markerID {
        buffer.markerID = markerID
        buffer.markerTS = markerID == nil ? nil : event.markerTS
      }
      if let unread = event.unread {
        buffer.unread = unread
      }
      if let mentions = event.mentions {
        buffer.mentions = mentions
      }
      buffers[event.id] = buffer
      updateBadge()

    case .bufferSettings(let event):
      apply(event)

    case .bufferReorder(let event):
      for entry in event.buffers {
        buffers[entry.id]?.sortOrder = entry.sortOrder
      }

    case .pinnedReorder(let event):
      for entry in event.buffers {
        buffers[entry.id]?.pinOrder = entry.pinOrder
      }

    case .networkState(let event):
      guard var network = networks[event.networkID] else { return }
      network.status = event.state
      networks[event.networkID] = network

    case .networkCreated(let event),
         .networkUpdated(let event):
      networks[event.network.id] = event.network

    case .networkDeleted(let event):
      networks.removeValue(forKey: event.id)
      for id in buffers.values.filter({ $0.networkID == event.id }).map(\.id) {
        removeBuffer(id)
      }

    case .networkReorder(let event):
      for entry in event.networks {
        networks[entry.id]?.sortOrder = entry.sortOrder
      }

    case .history(let event):
      mergeMessages(event.messages, into: event.bufferID)
      if event.messages.isEmpty {
        historyExhausted.insert(event.bufferID)
      }

    case .historyBackfill(let event):
      refetchBackfilledHistory(event.bufferID)

    case .preview(let event):
      guard
        var list = messages[event.bufferID],
        let index = list.firstIndex(where: { $0.id == event.messageID })
      else {
        return
      }
      list[index].previews = event.previews
      messages[event.bufferID] = list

    case .members(let event):
      members[event.bufferID] = event.members
      noteBots(event.members, networkID: event.networkID)
      noteAvatars(event.members, networkID: event.networkID)

    case .avatar(let event):
      let key = nickKey(event.networkID, event.nick)
      if event.hasAvatar {
        avatarNicks.insert(key)
      } else {
        avatarNicks.remove(key)
      }

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
          done: event.done,
        )
        channelList = current
      } else {
        channelList = event
      }

    case .error(let response):
      composerError = response.message ?? "The server rejected the command."

    case .ack,
         .ignored:
      break
    }
  }

  func receive(_ event: ServerEvent) {
    guard hydrated else {
      queuedEvents.append(event)
      return
    }
    apply(event)
  }

  func apply(_ event: BufferSettingsEvent) {
    guard var buffer = buffers[event.id] else { return }
    buffer.showEmbeds = event.showEmbeds
    buffer.showPresenceEvents = event.showPresenceEvents
    buffer.collapsePresenceEvents = event.collapsePresenceEvents
    buffer.pinned = event.pinned
    buffer.archived = event.archived
    if let pinOrder = event.pinOrder {
      buffer.pinOrder = pinOrder
    }
    buffers[event.id] = buffer
  }

  func mergeMessages(_ incoming: [Message], into bufferID: UUID) {
    var byID = Dictionary(uniqueKeysWithValues: (messages[bufferID] ?? []).map { ($0.id, $0) })
    for message in incoming {
      byID[message.id] = message
    }
    messages[bufferID] = byID.values.sorted(by: messageOrder)
  }

  // MARK: Private

  /// Handles a buffer_deleted broadcast: drop the buffer and all per-buffer
  /// state; if it was selected, fall back like at startup.
  private func removeBuffer(_ id: UUID) {
    guard buffers.removeValue(forKey: id) != nil else { return }
    messages.removeValue(forKey: id)
    members.removeValue(forKey: id)
    historyExhausted.remove(id)
    historyLoading.remove(id)
    if historyAnchor?.bufferID == id {
      historyAnchor = nil
    }
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

    // Muted senders still badge mentions but never count as unread or
    // anchor the marker (same rule as the server's tallyUnread).
    if message.muted != true {
      if buffer.markerID == nil {
        buffer.markerID = message.id
        buffer.markerTS = message.ts
      }
      buffer.unread += 1
    }
    if message.mentionsMe == true || message.highlight == true {
      buffer.mentions += 1
      if !applicationActive, notificationsEnabled {
        NotificationManager.shared.post(
          message: message,
          buffer: buffer,
          network: networks[buffer.networkID],
        )
      }
    }
    buffers[buffer.id] = buffer
    updateBadge()
  }

  /// A CHATHISTORY replay inserted older messages server-side without live
  /// message events (history_backfill). Refetch the recent window and merge;
  /// the recovered rows count as unread, mirroring apply(_ message:). Buffers
  /// never loaded just see the rows on their normal first load.
  private func refetchBackfilledHistory(_ bufferID: UUID) {
    guard messages[bufferID] != nil, let transport else { return }
    Task {
      guard let recent = try? await transport.fetchHistory(bufferID: bufferID, before: nil) else {
        return
      }
      let known = Set((messages[bufferID] ?? []).map(\.id))
      mergeMessages(recent, into: bufferID)
      guard var buffer = buffers[bufferID] else { return }
      var changed = false
      for message in recent where !known.contains(message.id) {
        guard message.countsAsUnread == true, message.isSelf != true else { continue }
        let isUnseen = buffer.lastSeenID.map { message.id.uuidString > $0.uuidString } ?? true
        guard isUnseen else { continue }
        changed = true
        if message.mentionsMe == true || message.highlight == true {
          buffer.mentions += 1
        }
        guard message.muted != true else { continue }
        buffer.unread += 1
        // Recovered messages predate any live arrivals, so the marker moves
        // back to the earliest of them.
        if buffer.markerID.map({ message.id.uuidString < $0.uuidString }) ?? true {
          buffer.markerID = message.id
          buffer.markerTS = message.ts
        }
        changed = true
      }
      if changed {
        buffers[bufferID] = buffer
        updateBadge()
      }
    }
  }

}

private func messageOrder(_ lhs: Message, _ rhs: Message) -> Bool {
  lhs.id.uuidString < rhs.id.uuidString
}
