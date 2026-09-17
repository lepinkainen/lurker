import Foundation

extension AppModel {

  // MARK: Internal

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

  /// On app focus: probe a nominally-connected socket with a WS ping so a
  /// dead TCP connection is noticed now rather than after the OS timeout,
  /// and cut any reconnect backoff short — the client should be usable by
  /// the time the user starts typing.
  func verifyConnection() {
    switch connectionState {
    case .connected:
      guard let transport, !syncing else { return }
      syncing = true
      verifyTask = Task {
        do {
          try await transport.ping()
        } catch {
          // Cancelling the socket makes the receive loop throw, which sends
          // connectionLoop into its normal reconnect path.
          await transport.disconnect()
        }
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
    return Task {
      do {
        try await transport.send(command)
      } catch {
        composerError = error.localizedDescription
        // A failed plain message goes back into the composer instead of
        // vanishing — the composer was cleared optimistically before the
        // send. Only if the user hasn't started typing something new.
        if command.type == "send", let content = command.content, composerText.isEmpty {
          composerText = content
        }
      }
    }
  }

  // MARK: Private

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
