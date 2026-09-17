import Foundation
import SwiftUI

// MARK: - Timeline formatting helpers

func isPresence(_ message: Message) -> Bool {
  presenceKinds.contains(message.kind)
}

@MainActor
func attributedBody(_ message: Message) -> AttributedString {
  var result = AttributedString()
  let segments =
    message.segments?.isEmpty == false ? message.segments! : [MircSegment(text: message.content)]
  let plainText = segments.map(\.text).joined()
  for segment in segments {
    var value = AttributedString(segment.text)
    if segment.bold == true {
      value.font = Theme.Fonts.message.bold()
    }
    if segment.italic == true {
      value.font = Theme.Fonts.message.italic()
    }
    if segment.underline == true {
      value.underlineStyle = .single
    }
    if segment.strike == true {
      value.strikethroughStyle = .single
    }
    if let foreground = segment.fg {
      value.foregroundColor = mircColor(foreground)
    }
    result.append(value)
  }
  if let detector = TimelineFormatters.linkDetector {
    for match in detector.matches(
      in: plainText,
      range: NSRange(plainText.startIndex..., in: plainText),
    ) {
      guard
        let url = match.url,
        let stringRange = Range(match.range, in: plainText),
        let attributedRange = Range(stringRange, in: result)
      else {
        continue
      }
      result[attributedRange].link = url
      result[attributedRange].foregroundColor = Color.lurkerLink
      result[attributedRange].underlineStyle = .single
    }
  }
  return result
}

func mircColor(_ value: Int) -> Color {
  let palette: [Color] = [
    .white,
    .black,
    .blue,
    .green,
    .red,
    .brown,
    .purple,
    .orange,
    .yellow,
    .green,
    .teal,
    .cyan,
    .blue,
    .pink,
    .gray,
    .secondary,
  ]
  return palette.indices.contains(value) ? palette[value] : .primary
}

/// Grouping key for day separators, in the user's local calendar day —
/// `displayDay` labels the separator in local time, so the key must bucket
/// the same way or days straddling UTC midnight get mislabeled separators.
@MainActor
func dayKey(_ raw: String, calendar: Calendar = .current) -> String {
  guard let date = parseTimestamp(raw) else { return String(raw.prefix(10)) }
  let parts = calendar.dateComponents([.year, .month, .day], from: date)
  return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
}

@MainActor
func displayDay(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else { return dayKey(raw) }
  return date.formatted(.dateTime.weekday(.wide).month(.wide).day().year())
}

@MainActor
func displayTime(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else {
    return String(raw.dropFirst(11).prefix(5))
  }
  return date.formatted(date: .omitted, time: .shortened)
}

@MainActor
func parseTimestamp(_ raw: String) -> Date? {
  if let date = try? Date(raw, strategy: .iso8601) {
    return date
  }
  return TimelineFormatters.iso8601.date(from: raw)
}

// MARK: - TimelineFormatters

@MainActor
enum TimelineFormatters {
  static let linkDetector = try? NSDataDetector(
    types: NSTextCheckingResult.CheckingType.link.rawValue
  )
  static let iso8601 = ISO8601DateFormatter()
}
