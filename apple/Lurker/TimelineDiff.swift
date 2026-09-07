#if os(macOS)

import Foundation

/// Pure block-level diff between the rendered timeline and the model's new
/// item list. AppKit-free so it unit-tests without a view.
enum TimelineDiff: Equatable {
  case none
  case rebuild
  /// `replacements` are indices whose id survived but whose content changed
  /// (preview arrival, netsplit tag, presence run growth); `appendFrom` is
  /// the first index of a strict-suffix append, nil when lengths match.
  case incremental(replacements: [Int], appendFrom: Int?)

  // MARK: Internal

  static func compute(old: [TimelineItem], new: [TimelineItem]) -> TimelineDiff {
    if old.isEmpty && new.isEmpty {
      return .none
    }
    if old.isEmpty || new.isEmpty {
      return .rebuild
    }
    let oldIDs = old.map(\.id)
    let newIDs = new.map(\.id)
    if oldIDs == newIDs {
      let replacements = old.indices.filter { old[$0] != new[$0] }
      return replacements.isEmpty
        ? .none
        : .incremental(replacements: replacements, appendFrom: nil)
    }
    if newIDs.count > oldIDs.count, Array(newIDs.prefix(oldIDs.count)) == oldIDs {
      let replacements = old.indices.filter { old[$0] != new[$0] }
      return .incremental(replacements: replacements, appendFrom: oldIDs.count)
    }
    return .rebuild
  }
}

extension TimelineItem {
  /// The message id a scroll anchor resolves to: a message row's own id, or
  /// a collapsed presence run's first member (its rendered identity).
  var anchorMessageID: UUID? {
    switch self {
    case .message(let message): message.id
    case .presence(let id, _): id
    case .day,
         .unread: nil
    }
  }
}

#endif
