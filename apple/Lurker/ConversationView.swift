import Foundation
import SwiftUI

// MARK: - ConversationView

struct ConversationView: View {

  // MARK: Internal

  var body: some View {
    if let buffer = model.selectedBuffer {
      VStack(spacing: 0) {
        ConversationHeader(buffer: buffer, network: model.selectedNetwork)
        Divider()
        SyncBanner()
        // macOS renders the timeline as a single AppKit NSTextView for correct
        // link hit-testing, hand cursor, and cross-row selection; iOS keeps the
        // SwiftUI implementation. See ai-docs/apple.md.
        #if os(macOS)
        MacTimelineContainer(buffer: buffer)
        #else
        TimelineView(buffer: buffer)
        #endif
        Divider()
        ComposerView(buffer: buffer)
      }
      .navigationTitle(
        buffer.kind == "status" ? "\(model.selectedNetwork?.name ?? "") Status" : buffer.name
      )
      #if os(macOS)
      // Hardware-keyboard ack. Key presses bubble from the focused view
      // (usually the composer) up through ancestors, so this fires with no
      // sheet on top; sheets own focus in their own hierarchy and keep
      // their Esc-to-dismiss behavior. `.ignored` when there is nothing to
      // ack preserves default Esc handling.
      .onKeyPress(.escape) {
        guard buffer.markerID != nil || buffer.unread > 0 else { return .ignored }
        model.ackRead(buffer.id)
        return .handled
      }
      #endif
    } else {
      ContentUnavailableView {
        Label("No Conversation Selected", systemImage: "bubble.left.and.bubble.right")
      } description: {
        Text(
          model.connectionState == .notConfigured
            ? "Choose a Lurker server to begin."
            : "Select a channel or conversation in the sidebar."
        )
      } actions: {
        if model.configuredURL == nil {
          Button("Set Up Connection") {
            model.showingConnectionEditor = true
          }
        }
      }
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

}

// MARK: - SyncBanner

/// Thin strip under the header while the displayed state may lag the backend
/// (focus ping in flight, reconnecting, offline). Appearance is debounced so
/// a fast focus ping doesn't flash the banner; hiding is immediate.
private struct SyncBanner: View {

  // MARK: Internal

  var body: some View {
    Group {
      if visible {
        HStack(spacing: 6) {
          ProgressView()
            .controlSize(.small)
          Text(text)
            .font(.callout)
            .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 5)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
        .transition(.move(edge: .top).combined(with: .opacity))
      }
    }
    .task(id: model.outOfSync) {
      if model.outOfSync {
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        withAnimation(.easeOut(duration: 0.15)) { visible = true }
      } else {
        withAnimation(.easeOut(duration: 0.15)) { visible = false }
      }
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model
  @State private var visible = false

  private var text: String {
    switch model.connectionState {
    // reconnecting(0) is the in-flight retry attempt, not a countdown.
    case .connected,
         .connecting,
         .notConfigured,
         .reconnecting(0): "Syncing…"
    case .reconnecting,
         .offline: model.connectionState.label
    }
  }

}

// MARK: - ConversationHeader

private struct ConversationHeader: View {

  // MARK: Internal

  let buffer: Buffer
  let network: Network?

  var body: some View {
    HStack(alignment: .firstTextBaseline, spacing: 10) {
      VStack(alignment: .leading, spacing: 3) {
        HStack(spacing: 6) {
          Text(buffer.kind == "status" ? network?.name ?? "Status" : buffer.name)
            .font(.title3)
          if buffer.kind == "channel", !buffer.joined {
            Text("ARCHIVED")
              .font(.caption.weight(.bold))
              .foregroundStyle(.secondary)
              .padding(.horizontal, Theme.badgeHorizontalPadding)
              .padding(.vertical, 2)
              .background(.quaternary, in: .rect(cornerRadius: 3))
          }
        }
        Text(subtitle)
          .font(.body)
          .foregroundStyle(.secondary)
          .lineLimit(1)
          .truncationMode(.tail)
          .textSelection(.enabled)
      }
      Spacer()
    }
    .padding(.horizontal, Theme.rowHorizontalInset)
    .padding(.vertical, 9)
    .background(.bar)
  }

  // MARK: Private

  private var subtitle: String {
    if buffer.kind == "status" {
      return [network?.host, network?.status].compactMap(\.self).joined(separator: " • ")
    }
    return buffer.topic?.isEmpty == false ? buffer.topic! : network?.name ?? ""
  }

}

// MARK: - TimelineView

private struct TimelineView: View {

  // MARK: Internal

  let buffer: Buffer

  var body: some View {
    ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(spacing: 0) {
          if model.historyLoading.contains(buffer.id) {
            ProgressView()
              .controlSize(.small)
              .padding(10)
          }
          ForEach(items) { item in
            switch item {
            case .day(let id, let title):
              DaySeparator(title: title).id(id)

            case .unread(let id):
              UnreadSeparator().id(id)

            case .message(let message):
              MessageRow(message: message, buffer: buffer).id(message.id)
                .onAppear {
                  if message.id == model.selectedMessages.first?.id {
                    model.loadOlderHistory()
                  }
                }

            case .presence(let id, let messages):
              // A collapsed presence group can be the oldest item in the
              // buffer; without this the load-older trigger never fires.
              PresenceSummary(messages: messages).id(id)
                .onAppear {
                  if messages.first?.id == model.selectedMessages.first?.id {
                    model.loadOlderHistory()
                  }
                }
            }
          }
        }
        .padding(.vertical, 5)
      }
      .safeAreaInset(edge: .top, spacing: 0) {
        // `unread > 0` fallback: keeps the ack affordance available when the
        // server predates `marker_id` (version skew) — without it there is no
        // way to clear the badge at all.
        if buffer.markerID != nil || buffer.unread > 0 {
          UnreadBar(buffer: buffer)
        }
      }
      .defaultScrollAnchor(.bottom)
      .onChange(of: model.selectedMessages.last?.id) { old, new in
        guard old != nil, let new else { return }
        withAnimation(.snappy(duration: 0.18)) {
          proxy.scrollTo(new, anchor: .bottom)
        }
      }
      // After an older page is prepended the viewport would otherwise stay at
      // the top of the grown content, re-triggering the load in a runaway
      // loop. Pin the previously-first message back to the top edge (web does
      // the same with a scrollHeight delta).
      // An anchor is only ever set for the selected buffer, so one addressed
      // elsewhere is stale: drop it without scrolling rather than leaving it to
      // block further load-older calls.
      .onChange(of: model.historyAnchor) { _, anchor in
        guard let anchor else { return }
        if anchor.bufferID == buffer.id {
          proxy.scrollTo(anchor.messageID, anchor: .top)
        }
        model.historyAnchor = nil
      }
      // Teardown before the anchor was consumed (e.g. iOS compact popping back
      // to the sidebar) would otherwise leave it set forever, and load-older is
      // gated on it being nil.
      .onDisappear {
        if model.historyAnchor?.bufferID == buffer.id {
          model.historyAnchor = nil
        }
      }
    }
    // Rebuild the scroll container per buffer so switching channels always
    // re-applies the bottom anchor and lands at the end of the backlog.
    .id(buffer.id)
    .background(Color.lurkerTimelineBackground)
  }

  // MARK: Private

  @Environment(AppModel.self) private var model

  private var items: [TimelineItem] {
    timelineItems(model.selectedMessages, buffer: buffer)
  }

}

/// Derives the renderable timeline (day separators, unread separator, presence
/// grouping) from the visible message list. Shared by the iOS SwiftUI timeline
/// and the macOS NSTextView coordinator so the grouping rules stay
/// single-sourced. `expandedGroups` (macOS) lists collapsed presence runs the
/// user expanded in place: those emit their member rows after the summary. The
/// iOS DisclosureGroup manages its own expansion and passes nothing.
@MainActor
func timelineItems(
  _ messages: [Message],
  buffer: Buffer,
  expandedGroups: Set<UUID> = [],
) -> [TimelineItem] {
  var result = [TimelineItem]()
  var lastDay: String?
  var presence = [Message]()

  func flushPresence() {
    guard !presence.isEmpty else { return }
    if buffer.collapsePresenceEvents, presence.count > 1 {
      result.append(.presence(presence[0].id, presence))
      if expandedGroups.contains(presence[0].id) {
        result.append(contentsOf: presence.map(TimelineItem.message))
      }
    } else {
      result.append(contentsOf: presence.map(TimelineItem.message))
    }
    presence.removeAll(keepingCapacity: true)
  }

  for message in messages {
    let day = dayKey(message.ts)
    if day != lastDay {
      flushPresence()
      result.append(.day("day-\(day)", displayDay(message.ts)))
      lastDay = day
    }
    if buffer.markerID == message.id {
      flushPresence()
      result.append(.unread("unread-\(message.id.uuidString)"))
    }
    if isPresence(message) {
      presence.append(message)
    } else {
      flushPresence()
      result.append(.message(message))
    }
  }
  flushPresence()
  return result
}

// MARK: - TimelineItem

enum TimelineItem: Identifiable, Equatable {
  case day(String, String)
  case unread(String)
  case message(Message)
  case presence(UUID, [Message])

  var id: String {
    switch self {
    case .day(let id, _),
         .unread(let id): id
    case .message(let message): message.id.uuidString
    case .presence(let id, _): "presence-\(id.uuidString)"
    }
  }
}

#if DEBUG
#Preview {
  ConversationView()
    .environment(AppModel.preview())
    .tint(.mint)
    #if os(macOS)
    .frame(width: 700, height: 600)
    #endif
}
#endif
