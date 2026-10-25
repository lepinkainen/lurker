import Foundation
import Testing

@testable import Lurker

// MARK: - ConcurrencyRegressionTests

@MainActor
@Suite(.timeLimit(.minutes(1)))
struct ConcurrencyRegressionTests {

  // MARK: Internal

  @Test(arguments: ["", "a newer draft"])
  func `failed send returns to its originating draft after switching channels`(newerDraft: String) async throws {
    let gate = RequestGate<Void>()
    var transport = ControlledTransport()
    transport.sendAction = { try await gate.request() }
    let model = makeModel(transport: transport)
    model.applySnapshot(FixtureTransport.snapshot())
    model.selectBuffer(FixtureTransport.channelID)
    model.composerText = "only for the first channel"
    let send = try #require(model.sendComposer())
    await gate.waitUntilRequested()

    model.composerText = newerDraft
    model.selectBuffer(FixtureTransport.fullChannelID)
    await gate.finish(.failure(URLError(.networkConnectionLost)))
    await send.value

    #expect(model.composerText.isEmpty)
    #expect(model.composerError == nil)
    #expect(model.commandFailures.first?.content == "only for the first channel")
    model.selectBuffer(FixtureTransport.channelID)
    #expect(model.composerText == (newerDraft.isEmpty ? "only for the first channel" : newerDraft))
  }

  @Test(arguments: ["join", "delete_buffer", "mark_read"])
  func `failed commands remain visible after changing buffers`(type: String) async throws {
    let gate = RequestGate<Void>()
    var transport = ControlledTransport()
    transport.sendAction = { try await gate.request() }
    let model = makeModel(transport: transport)
    model.applySnapshot(FixtureTransport.snapshot())
    model.selectBuffer(FixtureTransport.channelID)
    let destination = try #require(model.selectedBuffer?.name)
    let task = try #require(model.send(ClientCommand(type: type, bufferID: FixtureTransport.channelID)))
    await gate.waitUntilRequested()
    model.selectBuffer(FixtureTransport.fullChannelID)
    model.composerText = "new channel draft"
    await gate.finish(.failure(URLError(.networkConnectionLost)))
    await task.value

    #expect(model.composerText == "new channel draft")
    #expect(model.composerError == nil)
    let failure = try #require(model.commandFailures.first)
    #expect(failure.message.contains(destination))
    #expect(failure.message.contains(URLError(.networkConnectionLost).localizedDescription))
  }

  @Test(arguments: [false, true])
  func `failed send remains recoverable after replacing the server`(cancelled: Bool) async throws {
    let gate = RequestGate<Void>()
    var transport = ControlledTransport()
    transport.sendAction = { try await gate.request() }
    let model = makeModel(transport: transport)
    model.defaults.set("http://localhost:8080", forKey: AppModel.Defaults.serverURL)
    model.applySnapshot(FixtureTransport.snapshot())
    model.selectBuffer(FixtureTransport.channelID)
    model.composerText = "message for the old server"
    let task = try #require(model.sendComposer())
    await gate.waitUntilRequested()
    model.stop()
    model.resetServerState()
    model.defaults.set("http://localhost:9090", forKey: AppModel.Defaults.serverURL)
    model.transport = FixtureTransport()
    // Reuse the UUID to prove recovery cannot contaminate the new server.
    model.applySnapshot(FixtureTransport.snapshot())
    model.selectBuffer(FixtureTransport.channelID)
    model.composerText = "new server draft"
    await gate.finish(.failure(URLError(cancelled ? .cancelled : .networkConnectionLost)))
    await task.value

    #expect(model.composerText == "new server draft")
    #expect(model.composerError == nil)
    let failure = try #require(model.commandFailures.first)
    #expect(failure.content == "message for the old server")
    #expect(failure.message.contains("localhost:8080"))
    #expect(!failure.message.contains("localhost:9090"))
    model.selectBuffer(FixtureTransport.fullChannelID)
    model.selectBuffer(FixtureTransport.channelID)
    #expect(model.composerText == "new server draft")
  }

  @Test(arguments: ["", "newer draft"])
  func `failed send recovery preserves history browsing and the stored draft`(draft: String) async throws {
    let gate = RequestGate<Void>()
    var transport = ControlledTransport()
    transport.sendAction = { try await gate.request() }
    let model = makeModel(transport: transport)
    model.applySnapshot(FixtureTransport.snapshot())
    let origin = FixtureTransport.channelID
    model.selectBuffer(origin)
    model.composerText = "failed message"
    let task = try #require(model.sendComposer())
    await gate.waitUntilRequested()
    let recalled = model.inputHistory.navigateUp(buffer: origin, current: draft)
    model.composerText = try #require(recalled)
    model.selectBuffer(FixtureTransport.fullChannelID)
    await gate.finish(.failure(URLError(.networkConnectionLost)))
    await task.value

    // Recovery must not reset the browse cursor just to inspect the draft.
    let expected = draft.isEmpty ? "failed message" : draft
    #expect(model.inputHistory.navigateDown(buffer: origin) == expected)
    model.selectBuffer(origin)
    #expect(model.composerText == expected)
  }

  @Test
  func `search keeps expanded context until debounce completes`() async throws {
    let gate = RequestGate<Void>()
    let hit = try #require(FixtureTransport.fullMessages.first)
    let state = HistorySearchModel(debounce: { try await gate.request() })
    state.results = [hit]
    state.expandedID = hit.id
    state.context = [hit]
    let task = Task { await state.search(query: "new", bufferID: nil, transport: FixtureTransport()) }
    await gate.waitUntilRequested()
    #expect(state.expandedID == hit.id)
    #expect(state.context.map(\.id) == [hit.id])
    await gate.finish(.success(()))
    await task.value
    #expect(state.expandedID == nil)
    #expect(state.context.isEmpty)
  }

  @Test
  func `missing search transport does not claim an empty completed search`() async throws {
    let state = HistorySearchModel(debounce: { })
    await state.search(query: "message", bufferID: nil, transport: nil)
    #expect(state.unavailable)
    #expect(!state.exhausted)
    #expect(!state.searching)

    let hit = try #require(FixtureTransport.fullMessages.first)
    state.results = [hit]
    state.expandedID = hit.id
    state.context = [hit]
    await state.search(query: "another", bufferID: nil, transport: nil)
    #expect(state.results.map(\.id) == [hit.id])
    #expect(state.expandedID == hit.id)
    #expect(state.context.map(\.id) == [hit.id])

    await state.search(query: "another", bufferID: nil, transport: FixtureTransport())
    #expect(!state.unavailable)
    #expect(state.exhausted)
    #expect(state.results.isEmpty)
  }

  @Test
  func `search pagination appends one page and rejects duplicate row appearances`() async throws {
    let gate = RequestGate<[Message]>()
    let firstPage = Array(FixtureTransport.fullMessages.prefix(50))
    let nextPage = Array(FixtureTransport.fullMessages.dropFirst(50).prefix(3))
    var transport = ControlledTransport()
    transport.searchAction = { _, _, before in
      before == nil ? firstPage : try await gate.request()
    }
    let state = HistorySearchModel(debounce: { })
    await state.search(query: "message", bufferID: nil, transport: transport)
    let last = try #require(firstPage.last)
    let page = try #require(state.loadMoreIfLast(last, query: "message", bufferID: nil, transport: transport))
    #expect(state.loadMoreIfLast(last, query: "message", bufferID: nil, transport: transport) == nil)
    await gate.waitUntilRequested()
    #expect(state.searching)
    await gate.finish(.success(nextPage))
    await page.value
    #expect(state.results.map(\.id) == (firstPage + nextPage).map(\.id))
    #expect(state.exhausted)
    #expect(!state.searching)
  }

  @Test
  func `obsolete page cannot clear the replacement search loading state`() async throws {
    let pageGate = RequestGate<[Message]>()
    let replacementGate = RequestGate<[Message]>()
    let firstPage = Array(FixtureTransport.fullMessages.prefix(50))
    var transport = ControlledTransport()
    transport.searchAction = { query, _, before in
      if query == "new" {
        return try await replacementGate.request()
      }
      return before == nil ? firstPage : try await pageGate.request()
    }
    let state = HistorySearchModel(debounce: { })
    await state.search(query: "old", bufferID: nil, transport: transport)
    let last = try #require(firstPage.last)
    let page = try #require(state.loadMoreIfLast(last, query: "old", bufferID: nil, transport: transport))
    await pageGate.waitUntilRequested()
    let replacement = Task { await state.search(query: "new", bufferID: nil, transport: transport) }
    await replacementGate.waitUntilRequested()
    await pageGate.finish(.failure(URLError(.cancelled)))
    await page.value
    #expect(state.searching)
    await replacementGate.finish(.success([]))
    await replacement.value
    #expect(!state.searching)
    #expect(state.results.isEmpty)
  }

  @Test
  func `late search page cannot append to a replacement query`() async throws {
    let pageGate = RequestGate<[Message]>()
    let firstPage = Array(FixtureTransport.fullMessages.prefix(50))
    let newHit = try #require(FixtureTransport.fullMessages.last)
    var transport = ControlledTransport()
    transport.searchAction = { query, _, before in
      if before != nil {
        return try await pageGate.request()
      }
      return query == "old" ? firstPage : [newHit]
    }
    let state = HistorySearchModel(debounce: { })
    await state.search(query: "old", bufferID: nil, transport: transport)
    let last = try #require(firstPage.last)
    let oldPage = try #require(state.loadMoreIfLast(
      last,
      query: "old",
      bufferID: nil,
      transport: transport,
    ))
    await pageGate.waitUntilRequested()

    await state.search(query: "new", bufferID: nil, transport: transport)
    #expect(state.results.map(\.id) == [newHit.id])
    await pageGate.finish(.success(firstPage))
    await oldPage.value

    #expect(state.results.map(\.id) == [newHit.id])
    #expect(state.exhausted)
    #expect(!state.searching)
  }

  @Test(arguments: [false, true])
  func `cancelled first search cannot replace newer results`(fails: Bool) async throws {
    let gate = RequestGate<[Message]>()
    let hit = try #require(FixtureTransport.fullMessages.last)
    var transport = ControlledTransport()
    transport.searchAction = { query, _, _ in
      query == "old" ? try await gate.request() : [hit]
    }
    let state = HistorySearchModel(debounce: { })
    let oldSearch = Task { await state.search(query: "old", bufferID: nil, transport: transport) }
    await gate.waitUntilRequested()
    oldSearch.cancel()
    await state.search(query: "new", bufferID: nil, transport: transport)
    if fails {
      await gate.finish(.failure(URLError(.cancelled)))
    } else {
      await gate.finish(.success([]))
    }
    await oldSearch.value

    #expect(state.results.map(\.id) == [hit.id])
  }

  @Test
  func `stopped focus ping cannot clear replacement ping state`() async throws {
    let oldGate = RequestGate<Void>()
    let newGate = RequestGate<Void>()
    var oldTransport = ControlledTransport()
    oldTransport.pingAction = { try await oldGate.request() }
    let model = makeModel(transport: oldTransport)
    model.connectionState = .connected
    model.verifyConnection()
    let oldPing = try #require(model.verifyTask)
    await oldGate.waitUntilRequested()
    model.stop()
    #expect(!model.syncing)
    #expect(oldPing.isCancelled)

    var newTransport = ControlledTransport()
    newTransport.pingAction = { try await newGate.request() }
    model.transport = newTransport
    model.verifyConnection()
    let newPing = try #require(model.verifyTask)
    await newGate.waitUntilRequested()
    await oldGate.finish(.failure(URLError(.cancelled)))
    await oldPing.value
    #expect(model.syncing)
    await newGate.finish(.success(()))
    await newPing.value
    #expect(!model.syncing)
  }

  @Test(arguments: [false, true])
  func `stopped connection cannot overwrite replacement state`(fails: Bool) async {
    let gate = RequestGate<StateSnapshot>()
    var oldTransport = ControlledTransport()
    oldTransport.stateAction = { try await gate.request() }
    let model = makeModel(transport: oldTransport)
    // Drive the production loop without registering notification callbacks.
    let oldLoop = Task { await model.connectionLoop() }
    model.connectionTask = oldLoop
    await gate.waitUntilRequested()
    model.stop()
    model.resetServerState()

    // Replacement hydration has finished before the old stream yields.
    model.transport = FixtureTransport()
    let replacement = FixtureTransport.snapshot()
    model.applySnapshot(replacement)
    model.connectionState = .connected
    if fails {
      await gate.finish(.failure(URLError(.cancelled)))
    } else {
      await gate.finish(.success(StateSnapshot(networks: [], buffers: [], initialMessages: [:], members: [:])))
    }
    await oldLoop.value

    #expect(model.connectionState == .connected)
    #expect(Set(model.buffers.keys) == Set(replacement.buffers.map(\.id)))
  }

  @Test(arguments: [false, true])
  func `cancelled context request cannot overwrite a newly expanded hit`(fails: Bool) async throws {
    let gate = RequestGate<[Message]>()
    let hits = Array(FixtureTransport.fullMessages.prefix(2))
    let first = try #require(hits.first)
    let second = try #require(hits.last)
    var transport = ControlledTransport()
    transport.contextAction = { _, id in
      id == first.id ? try await gate.request() : [second]
    }
    let state = HistorySearchModel(debounce: { })
    state.results = hits
    state.expandedID = first.id
    let oldContext = Task { await state.loadContext(transport: transport) }
    await gate.waitUntilRequested()
    // Mirrors SwiftUI replacing .task(id: expandedID).
    oldContext.cancel()
    state.expandedID = second.id
    await state.loadContext(transport: transport)
    #expect(state.context.map(\.id) == [second.id])

    if fails {
      await gate.finish(.failure(URLError(.cancelled)))
    } else {
      await gate.finish(.success([first]))
    }
    await oldContext.value
    #expect(state.context.map(\.id) == [second.id])
  }

  // MARK: Private

  private func makeModel(transport: any LurkerTransport) -> AppModel {
    AppModel(transport: transport, defaults: isolatedDefaults())
  }

}

// MARK: - RequestGate

/// Holds one response until the test releases it, even if its caller is canceled.
/// This models a response already queued when cancellation arrives.
private actor RequestGate<Value: Sendable> {

  // MARK: Internal

  func request() async throws -> Value {
    try await withCheckedThrowingContinuation { continuation in
      response = continuation
      for waiter in waiters { waiter.resume() }
      waiters.removeAll()
    }
  }

  func waitUntilRequested() async {
    if response != nil {
      return
    }
    await withCheckedContinuation { waiters.append($0) }
  }

  func finish(_ result: Result<Value, Error>) {
    response?.resume(with: result)
    response = nil
  }

  // MARK: Private

  private var response: CheckedContinuation<Value, Error>?
  private var waiters = [CheckedContinuation<Void, Never>]()

}

// MARK: - ControlledTransport

private struct ControlledTransport: LurkerTransport {

  // MARK: Internal

  var sendAction: @Sendable () async throws -> Void = { }
  var pingAction: @Sendable () async throws -> Void = { }
  var stateAction: @Sendable () async throws -> StateSnapshot = { FixtureTransport.snapshot() }
  var searchAction: @Sendable (String, UUID?, UUID?) async throws -> [Message] = { _, _, _ in [] }
  var contextAction: @Sendable (UUID, UUID) async throws -> [Message] = { _, _ in [] }

  func validateServer() async throws -> ServiceIdentity {
    FixtureTransport.identity
  }

  func send(_: ClientCommand) async throws {
    try await sendAction()
  }

  func searchMessages(query: String, bufferID: UUID?, before: UUID?) async throws -> [Message] {
    try await searchAction(query, bufferID, before)
  }

  func fetchContext(bufferID: UUID, around: UUID) async throws -> [Message] {
    try await contextAction(bufferID, around)
  }

  /// The hello answer: yields the gated snapshot as the `state` frame, or
  /// fails the stream when the gate fails.
  func openEvents() async -> AsyncThrowingStream<ServerEvent, Error> {
    let stateAction = stateAction
    return AsyncThrowingStream { continuation in
      Task {
        do {
          continuation.yield(.state(try await stateAction()))
        } catch {
          continuation.finish(throwing: error)
        }
      }
    }
  }

  func disconnect() async { }
  func ping() async throws {
    try await pingAction()
  }

  func fetchHistory(bufferID: UUID, before: UUID?) async throws -> [Message] {
    try await fixture.fetchHistory(bufferID: bufferID, before: before)
  }

  func updateBuffer(id: UUID, patch: BufferSettingsPatch) async throws -> BufferSettingsEvent {
    try await fixture.updateBuffer(id: id, patch: patch)
  }

  func reorderNetworks(ids: [UUID]) async throws -> [Network] {
    try await fixture.reorderNetworks(ids: ids)
  }

  func reorderBuffers(networkID: UUID, ids: [UUID]) async throws -> BufferReorderEvent {
    try await fixture.reorderBuffers(networkID: networkID, ids: ids)
  }

  func reorderPinnedBuffers(ids: [UUID]) async throws -> PinnedReorderEvent {
    try await fixture.reorderPinnedBuffers(ids: ids)
  }

  func upload(_ data: Data, filename: String, contentType: String) async throws -> URL {
    try await fixture.upload(data, filename: filename, contentType: contentType)
  }

  // MARK: Private

  private let fixture = FixtureTransport()

}
