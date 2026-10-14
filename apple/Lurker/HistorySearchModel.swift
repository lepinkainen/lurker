import Foundation
import Observation

/// Request state shared by the macOS and iOS history search interfaces.
@MainActor
@Observable
final class HistorySearchModel {

  // MARK: Lifecycle

  init(debounce: @escaping @MainActor () async throws -> Void = {
    try await Task.sleep(for: .milliseconds(250))
  }) {
    self.debounce = debounce
  }

  // MARK: Internal

  var results = [Message]()
  var exhausted = false
  var searching = false
  var unavailable = false
  var expandedID: UUID?
  var context = [Message]()
  var selection: UUID?

  /// Whitespace-separated search terms; the view highlights these and the
  /// model treats an empty list as "nothing to search".
  static func terms(_ query: String) -> [String] {
    query.split(whereSeparator: \.isWhitespace).map(String.init)
  }

  @discardableResult
  func loadMoreIfLast(
    _ message: Message,
    query: String,
    bufferID: UUID?,
    transport: (any LurkerTransport)?,
  ) -> Task<Void, Never>? {
    guard
      message.id == results.last?.id, !exhausted, !searching, !unavailable,
      query == activeQuery, bufferID == activeBufferID
    else { return nil }
    // Reserve the page before scheduling its task so repeated row appearances
    // cannot launch the same cursor twice.
    searching = true
    let task = Task { await search(query: query, bufferID: bufferID, transport: transport, before: message.id) }
    paginationTask = task
    return task
  }

  func search(
    query: String,
    bufferID: UUID?,
    transport: (any LurkerTransport)?,
    before: UUID? = nil,
  ) async {
    guard !Task.isCancelled else { return }
    if before == nil {
      searchGeneration += 1
      contextGeneration += 1
      paginationTask?.cancel()
      paginationTask = nil
      activeQuery = query
      activeBufferID = bufferID
    }
    let generation = searchGeneration
    searching = true
    defer {
      if generation == searchGeneration {
        searching = false
        if before != nil {
          paginationTask = nil
        }
      }
    }
    if before == nil {
      // Debounce: a fresh keystroke replaces this task via .task(id:).
      do { try await debounce() } catch { return }
    }
    guard !Task.isCancelled, generation == searchGeneration else { return }
    guard !Self.terms(query).isEmpty else {
      unavailable = false
      expandedID = nil
      context = []
      results = []
      exhausted = true
      return
    }
    guard let transport else {
      unavailable = true
      return
    }
    unavailable = false
    if before == nil {
      expandedID = nil
      context = []
    }
    do {
      let page = try await transport.searchMessages(query: query, bufferID: bufferID, before: before)
      guard !Task.isCancelled, generation == searchGeneration else { return }
      results = before == nil ? page : results + page
      exhausted = page.count < 50
      if before == nil, selection == nil || !results.contains(where: { $0.id == selection }) {
        selection = results.first?.id
      }
    } catch is CancellationError {
      return
    } catch {
      guard !Task.isCancelled, generation == searchGeneration else { return }
      if before == nil {
        results = []
      }
      exhausted = true
    }
  }

  func loadContext(transport: (any LurkerTransport)?) async {
    guard !Task.isCancelled else { return }
    contextGeneration += 1
    let generation = contextGeneration
    context = []
    guard
      let expandedID, let transport,
      let hit = results.first(where: { $0.id == expandedID })
    else { return }
    let messages = (try? await transport.fetchContext(bufferID: hit.bufferID, around: hit.id)) ?? []
    guard !Task.isCancelled, generation == contextGeneration, self.expandedID == expandedID else { return }
    context = messages
  }

  /// SwiftUI owns first-page/context tasks. This model owns pagination tasks.
  func cancel() {
    searchGeneration += 1
    contextGeneration += 1
    paginationTask?.cancel()
    paginationTask = nil
    searching = false
  }

  // MARK: Private

  @ObservationIgnored private let debounce: @MainActor () async throws -> Void
  @ObservationIgnored private var searchGeneration = 0
  @ObservationIgnored private var contextGeneration = 0
  @ObservationIgnored private var paginationTask: Task<Void, Never>?
  @ObservationIgnored private var activeQuery: String?
  @ObservationIgnored private var activeBufferID: UUID?

}
