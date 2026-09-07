import Foundation
import SwiftUI

// MARK: - DaySeparator

struct DaySeparator: View {

  // MARK: Internal

  let title: String

  var body: some View {
    HStack {
      line
      Text(title)
        .font(.footnote.weight(.medium))
        .foregroundStyle(.secondary)
      line
    }
    .padding(.horizontal, Theme.rowHorizontalInset)
    .padding(.vertical, 9)
    .accessibilityElement(children: .combine)
  }

  // MARK: Private

  private var line: some View {
    Rectangle()
      .fill(Color.lurkerSeparator)
      .frame(height: 1)
  }

}

// MARK: - UnreadSeparator

struct UnreadSeparator: View {
  var body: some View {
    HStack {
      Rectangle().frame(height: 1)
      Text("New Messages")
        .font(.footnote.weight(.semibold))
      Rectangle().frame(height: 1)
    }
    .foregroundStyle(.orange)
    .padding(.horizontal, Theme.rowHorizontalInset)
    .padding(.vertical, 5)
    .accessibilityElement(children: .ignore)
    .accessibilityLabel("New messages begin here")
  }
}

// MARK: - UnreadBar

/// Floating control pinned above the timeline whenever the selected buffer has
/// a server-derived marker. Tapping it is the primary ack affordance: it clears
/// the marker, divider, and badges everywhere.
struct UnreadBar: View {

  // MARK: Internal

  let buffer: Buffer

  var body: some View {
    Button {
      model.ackRead(buffer.id)
    } label: {
      Text(label)
        .font(.footnote.weight(.semibold))
        .foregroundStyle(.orange)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 6)
        .contentShape(.rect)
    }
    .buttonStyle(.plain)
    .background(.ultraThinMaterial)
    .overlay(alignment: .bottom) {
      Rectangle()
        .fill(.orange.opacity(0.35))
        .frame(height: 1)
    }
    .accessibilityLabel(label)
    .accessibilityHint("Marks this conversation as read")
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

  private var label: String {
    // The count is unreliable at the server cap or when the marker message is
    // outside loaded history; fall back to the age of the boundary.
    let markerLoaded =
      buffer.markerID.map { id in
        model.messages[buffer.id]?.contains { $0.id == id } == true
      } ?? false
    if buffer.unread >= 1 && buffer.unread < 1000 && markerLoaded {
      return buffer.unread == 1 ? "1 new message" : "\(buffer.unread) new messages"
    }
    if let raw = buffer.markerTS, let date = parseTimestamp(raw) {
      return "new since \(sinceText(date))"
    }
    return buffer.unread == 1 ? "1 new message" : "\(buffer.unread) new messages"
  }

  private func sinceText(_ date: Date) -> String {
    let time = date.formatted(date: .omitted, time: .shortened)
    if Calendar.current.isDateInToday(date) {
      return time
    }
    if Calendar.current.isDateInYesterday(date) {
      return "yesterday \(time)"
    }
    return date.formatted(date: .abbreviated, time: .shortened)
  }

}

// MARK: - PresenceSummary

struct PresenceSummary: View {

  // MARK: Internal

  let messages: [Message]

  var body: some View {
    DisclosureGroup(isExpanded: $expanded) {
      ForEach(messages) { message in
        MessageRow(message: message, buffer: nil)
      }
    } label: {
      Label(summary, systemImage: "person.2.wave.2")
        .font(.footnote.monospaced())
        .foregroundStyle(.secondary)
        .padding(.vertical, 3)
    }
    .padding(.horizontal, Theme.rowHorizontalInset)
  }

  // MARK: Private

  @State private var expanded = false

  private var summary: String {
    presenceSummaryText(messages)
  }

}

/// Label for a collapsed presence run ("3 join • 1 part", or the netsplit
/// variant), shared by the SwiftUI DisclosureGroup and the macOS NSTextView
/// timeline.
func presenceSummaryText(_ messages: [Message]) -> String {
  if let split = messages.compactMap(\.netsplit).first {
    return "\(messages.count) users affected by netsplit \(split.serverA) ↔ \(split.serverB)"
  }
  let kinds = Dictionary(grouping: messages, by: \.kind).mapValues(\.count)
  return kinds.sorted(by: { $0.key < $1.key })
    .lazy.map { "\($0.value) \($0.key)" }
    .joined(separator: " • ")
}
