import Foundation

// MARK: - CommandFailure

struct CommandFailure: Identifiable {
  let id = UUID()
  let message: String
  let content: String?
}

extension AppModel {

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
    connectionGeneration += 1
    connectionTask?.cancel()
    connectionTask = nil
    verifyTask?.cancel()
    verifyTask = nil
    syncing = false
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

  /// On app focus: probe a nominally-connected socket with a WS ping so a
  /// dead TCP connection is noticed now rather than after the OS timeout,
  /// and cut any reconnect backoff short — the client should be usable by
  /// the time the user starts typing.
  func verifyConnection() {
    switch connectionState {
    case .connected:
      guard let transport, !syncing else { return }
      let generation = connectionGeneration
      syncing = true
      verifyTask = Task {
        do {
          try await transport.ping()
        } catch {
          guard isCurrent(generation) else { return }
          // Cancelling the socket makes the receive loop throw, which sends
          // connectionLoop into its normal reconnect path.
          await transport.disconnect()
        }
        guard isCurrent(generation) else { return }
        syncing = false
      }

    case .reconnecting,
         .offline:
      skipReconnectDelay = true

    case .connecting,
         .notConfigured:
      break
    }
  }

  @discardableResult
  func send(_ command: ClientCommand) -> Task<Void, Never>? {
    guard let transport else { return nil }
    let origin = selectedBufferID
    let generation = connectionGeneration
    let destination = command.channel ?? command.target
      ?? command.bufferID.flatMap { buffers[$0]?.name }
      ?? selectedBuffer?.name ?? "server"
    let server = configuredURL?.absoluteString
    return Task {
      do {
        try await transport.send(command)
      } catch {
        // A failed send still needs recovery after its connection is replaced.
        // Keep that content in a separate notice, never in the new server's draft.
        let sameSession = generation == connectionGeneration
        if sameSession, selectedBufferID == origin {
          composerError = error.localizedDescription
          if command.type == "send", let content = command.content, composerText.isEmpty {
            composerText = content
          }
        } else {
          if sameSession, let origin, command.type == "send", let content = command.content {
            inputHistory.recoverDraft(content, buffer: origin)
          }
          let action = command.type.replacingOccurrences(of: "_", with: " ").capitalized
          let location = server.map { "\(destination) on \($0)" } ?? destination
          commandFailures.append(CommandFailure(
            message: "\(action) failed for \(location): \(error.localizedDescription)",
            content: command.type == "send" ? command.content : nil,
          ))
        }
      }
    }
  }

  /// False once the task was cancelled or `stop()` replaced the connection;
  /// every await in the connection path re-checks this before publishing state.
  func isCurrent(_ generation: Int) -> Bool {
    !Task.isCancelled && generation == connectionGeneration
  }

  /// Internal so transport tests can exercise the loop without notification setup.
  ///
  /// Everything arrives over the stream: the transport's hello handshake
  /// yields either a replay of what this client missed or a `state`
  /// snapshot, then live events, all in one order with the transport
  /// dropping anything already applied. There is no separate state fetch
  /// to race the stream (ai-docs/websocket-protocol.md).
  func connectionLoop() async {
    guard let transport else { return }
    let generation = connectionGeneration
    var attempt = 0
    while !Task.isCancelled {
      do {
        connectionState = attempt == 0 ? .connecting : .reconnecting(0)
        // history_result frames are socket-local (never replayed): forget
        // requests the old socket may have lost, or paging stays blocked
        // for those buffers after a replay-only resume.
        historyLoading.removeAll(keepingCapacity: true)
        let stream = await transport.openEvents()
        guard isCurrent(generation) else { return }
        let receiver = Task { @MainActor [weak self] in
          for try await event in stream {
            guard let self, isCurrent(generation) else { return }
            receive(event)
          }
        }
        defer { receiver.cancel() }
        let identity = try await transport.validateServer()
        guard isCurrent(generation) else { return }
        serviceIdentity = identity
        connectionState = .connected
        attempt = 0
        try await receiver.value
        throw LurkerAPIError.disconnected
      } catch is CancellationError {
        return
      } catch {
        // URLSession cancellation can arrive as URLError.cancelled, not just
        // CancellationError. An old session must not publish reconnect state.
        guard isCurrent(generation) else { return }
        await transport.disconnect()
        guard isCurrent(generation) else { return }
        attempt += 1
        let delay = min(30, 1 << min(attempt - 1, 5))
        connectionState = .offline(error.localizedDescription)
        for remaining in stride(from: delay, through: 1, by: -1) {
          if skipReconnectDelay {
            break
          }
          connectionState = .reconnecting(remaining)
          try? await Task.sleep(for: .seconds(1))
          if Task.isCancelled {
            return
          }
        }
        skipReconnectDelay = false
      }
    }
  }

}
