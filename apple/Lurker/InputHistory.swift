import Foundation

/// Per-buffer composer input history, mirroring the web client's
/// input-history.ts: up to 100 sent lines per buffer, arrow-up/down browsing
/// with the in-progress draft stashed on first ↑ and restored when stepping
/// past the newest entry. In-memory only — history does not persist across
/// launches (web parity).
struct InputHistory {
  static let maxEntries = 100

  struct BufferState {
    var entries: [String] = []
    var draft: String = ""
    /// Index into `entries` while browsing; nil when not browsing.
    var index: Int? = nil
  }

  private var perBuffer: [UUID: BufferState] = [:]

  /// Record a sent plain-message line. Slash commands are never recorded
  /// (web parity). Resets any in-progress browse.
  mutating func record(_ text: String, buffer: UUID) {
    var state = perBuffer[buffer] ?? BufferState()
    state.entries.append(text)
    if state.entries.count > Self.maxEntries {
      state.entries.removeFirst(state.entries.count - Self.maxEntries)
    }
    state.draft = ""
    state.index = nil
    perBuffer[buffer] = state
  }

  /// Step to an older entry. On the first ↑ the current composer text is
  /// stashed as the draft. Returns the text to show, or nil when there is no
  /// history (the key should then fall through to normal caret behavior).
  mutating func navigateUp(buffer: UUID, current: String) -> String? {
    var state = perBuffer[buffer] ?? BufferState()
    guard !state.entries.isEmpty else { return nil }
    if let index = state.index {
      state.index = max(0, index - 1)
    } else {
      state.draft = current
      state.index = state.entries.count - 1
    }
    perBuffer[buffer] = state
    return state.entries[state.index!]
  }

  /// Step to a newer entry; past the newest restores the stashed draft and
  /// ends browsing. Returns nil when not browsing.
  mutating func navigateDown(buffer: UUID) -> String? {
    var state = perBuffer[buffer] ?? BufferState()
    guard let index = state.index else { return nil }
    if index >= state.entries.count - 1 {
      state.index = nil
      perBuffer[buffer] = state
      return state.draft
    }
    state.index = index + 1
    perBuffer[buffer] = state
    return state.entries[state.index!]
  }

  /// Preserve the composer text when switching away from a buffer. Ignored
  /// while browsing so the stashed draft is not clobbered (web parity).
  mutating func stashDraft(_ text: String, buffer: UUID) {
    var state = perBuffer[buffer] ?? BufferState()
    guard state.index == nil else { return }
    state.draft = text
    perBuffer[buffer] = state
  }

  /// The draft to restore when entering a buffer; also ends any browse.
  mutating func restoreDraft(buffer: UUID) -> String {
    var state = perBuffer[buffer] ?? BufferState()
    state.index = nil
    perBuffer[buffer] = state
    return state.draft
  }
}
