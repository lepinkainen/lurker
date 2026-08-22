import Foundation
import SwiftUI
import UniformTypeIdentifiers

#if !os(macOS)
  import PhotosUI
#endif

struct ConversationView: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    if let buffer = model.selectedBuffer {
      VStack(spacing: 0) {
        ConversationHeader(buffer: buffer, network: model.selectedNetwork)
        Divider()
        SyncBanner()
        TimelineView(buffer: buffer)
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
            : "Select a channel or conversation in the sidebar.")
      } actions: {
        if model.configuredURL == nil {
          Button("Set Up Connection") {
            model.showingConnectionEditor = true
          }
        }
      }
    }
  }
}

/// Thin strip under the header while the displayed state may lag the backend
/// (focus ping in flight, reconnecting, offline). Appearance is debounced so
/// a fast focus ping doesn't flash the banner; hiding is immediate.
private struct SyncBanner: View {
  @Environment(AppModel.self) private var model
  @State private var visible = false

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

  private var text: String {
    switch model.connectionState {
    // reconnecting(0) is the in-flight retry attempt, not a countdown.
    case .connected, .connecting, .notConfigured, .reconnecting(0): "Syncing…"
    case .reconnecting, .offline: model.connectionState.label
    }
  }
}

private struct ConversationHeader: View {
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

  private var subtitle: String {
    if buffer.kind == "status" {
      return [network?.host, network?.status].compactMap(\.self).joined(separator: " • ")
    }
    return buffer.topic?.isEmpty == false ? buffer.topic! : network?.name ?? ""
  }
}

private struct TimelineView: View {
  @Environment(AppModel.self) private var model
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
        if model.historyAnchor?.bufferID == buffer.id { model.historyAnchor = nil }
      }
    }
    // Rebuild the scroll container per buffer so switching channels always
    // re-applies the bottom anchor and lands at the end of the backlog.
    .id(buffer.id)
    .background(Color.lurkerTimelineBackground)
  }

  private var items: [TimelineItem] {
    var result: [TimelineItem] = []
    var lastDay: String?
    var presence: [Message] = []

    func flushPresence() {
      guard !presence.isEmpty else { return }
      if buffer.collapsePresenceEvents, presence.count > 1 {
        result.append(.presence(presence[0].id, presence))
      } else {
        result.append(contentsOf: presence.map(TimelineItem.message))
      }
      presence.removeAll(keepingCapacity: true)
    }

    for message in model.selectedMessages {
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
}

private enum TimelineItem: Identifiable {
  case day(String, String)
  case unread(String)
  case message(Message)
  case presence(UUID, [Message])

  var id: String {
    switch self {
    case .day(let id, _), .unread(let id): id
    case .message(let message): message.id.uuidString
    case .presence(let id, _): "presence-\(id.uuidString)"
    }
  }
}

private struct DaySeparator: View {
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

  private var line: some View {
    Rectangle()
      .fill(Color.lurkerSeparator)
      .frame(height: 1)
  }
}

private struct UnreadSeparator: View {
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

/// Floating control pinned above the timeline whenever the selected buffer has
/// a server-derived marker. Tapping it is the primary ack affordance: it clears
/// the marker, divider, and badges everywhere.
private struct UnreadBar: View {
  @Environment(AppModel.self) private var model
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
    if Calendar.current.isDateInToday(date) { return time }
    if Calendar.current.isDateInYesterday(date) { return "yesterday \(time)" }
    return date.formatted(date: .abbreviated, time: .shortened)
  }
}

private struct PresenceSummary: View {
  let messages: [Message]
  @State private var expanded = false

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

  private var summary: String {
    if let split = messages.compactMap(\.netsplit).first {
      return "\(messages.count) users affected by netsplit \(split.serverA) ↔ \(split.serverB)"
    }
    let kinds = Dictionary(grouping: messages, by: \.kind).mapValues(\.count)
    return kinds.sorted(by: { $0.key < $1.key })
      .map { "\($0.value) \($0.key)" }
      .joined(separator: " • ")
  }
}

private struct MessageRow: View {
  @Environment(AppModel.self) private var model
  let message: Message
  let buffer: Buffer?

  var body: some View {
    layout
      .padding(.horizontal, 11)
      .padding(.vertical, 2)
      .background(highlightColor)
      .contextMenu {
        Button("Copy Message") {
          Clipboard.copy(message.content)
        }
        if !message.sender.isEmpty {
          Button("Copy Nickname") {
            Clipboard.copy(message.sender)
          }
          Divider()
          Button("Mute \(message.sender)") {
            model.mute(nick: message.sender, in: message.networkID)
          }
          Button("Unmute \(message.sender)") {
            model.unmute(nick: message.sender, in: message.networkID)
          }
        }
      }
      .accessibilityElement(children: .combine)
      .accessibilityLabel("\(message.sender), \(displayTime(message.ts)), \(message.content)")
  }

  // macOS: fixed timestamp gutter, then a content-sized left-aligned nick with
  // the body immediately following (nicks vary in width; bodies don't align into
  // a column — matches the web client). iOS: those columns eat ~160pt of a 402pt
  // screen, so the nick moves to its own line above the body with the timestamp
  // trailing it, and the body wraps at full width.
  @ViewBuilder private var layout: some View {
    #if os(macOS)
      HStack(alignment: .firstTextBaseline, spacing: 8) {
        timestamp
          .frame(width: 42, alignment: .trailing)
        if message.displayKind == "sys" {
          systemIcon
            .frame(width: 14)
          systemBody
        } else {
          HStack(alignment: .firstTextBaseline, spacing: 4) {
            NickAvatar(
              nick: message.sender, colorIndex: message.senderColor,
              isBot: model.isBot(message.sender),
              networkID: message.networkID, hasAvatar: model.hasAvatar(message.sender)
            )
            .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            Text(message.sender)
              .font(Theme.Fonts.nick.weight(message.isSelf == true ? .bold : .medium))
              .foregroundStyle(nickPaletteColor(message.senderColor))
              .lineLimit(1)
              .truncationMode(.tail)
          }
          .help(message.userhost ?? message.sender)
          VStack(alignment: .leading, spacing: 6) {
            messageBody
            embeds
          }
        }
        Spacer(minLength: 4)
      }
    #else
      if message.displayKind == "sys" {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
          systemIcon
          systemBody
          Spacer(minLength: 4)
          timestamp
        }
      } else {
        VStack(alignment: .leading, spacing: 2) {
          HStack(alignment: .firstTextBaseline, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 4) {
              NickAvatar(
                nick: message.sender, colorIndex: message.senderColor,
                isBot: model.isBot(message.sender),
                networkID: message.networkID, hasAvatar: model.hasAvatar(message.sender)
              )
              .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
              Text(message.sender)
                .font(Theme.Fonts.nick.weight(message.isSelf == true ? .bold : .semibold))
                .foregroundStyle(nickPaletteColor(message.senderColor))
                .lineLimit(1)
                .truncationMode(.tail)
            }
            Spacer(minLength: 4)
            timestamp
          }
          messageBody
          embeds
        }
      }
    #endif
  }

  private var timestamp: some View {
    Text(displayTime(message.ts))
      .font(Theme.Fonts.timestamp)
      .foregroundStyle(.tertiary)
      .accessibilityHidden(true)
  }

  private var systemIcon: some View {
    Image(systemName: systemSymbol)
      .font(.caption)
      .foregroundStyle(.secondary)
  }

  private var systemBody: some View {
    Text(systemText)
      .font(Theme.Fonts.message)
      .foregroundStyle(.secondary)
      .textSelection(.enabled)
  }

  private var messageBody: some View {
    Text(attributedBody(message))
      .font(Theme.Fonts.message)
      .foregroundStyle(message.displayKind == "action" ? .purple : .primary)
      .textSelection(.enabled)
  }

  @ViewBuilder private var embeds: some View {
    if buffer?.showEmbeds != false {
      ForEach(message.previews ?? []) { preview in
        PreviewCard(preview: preview)
      }
    }
  }

  private var highlightColor: Color {
    message.mentionsMe == true || message.highlight == true ? .orange.opacity(0.10) : .clear
  }

  private var systemSymbol: String {
    switch message.kind {
    case "join": "arrow.right"
    case "part", "quit": "arrow.left"
    case "kick": "figure.fall"
    case "topic": "text.quote"
    case "connected": "bolt.horizontal.circle"
    case "disconnected": "bolt.slash"
    case "away": "moon.zzz"
    case "back": "sun.max"
    case "nick": "person.text.rectangle"
    case "account": "person.crop.circle.badge.checkmark"
    case "chghost": "at"
    default: "info.circle"
    }
  }

  private var systemText: String {
    let target = message.target ?? ""
    switch message.kind {
    case "away":
      return message.content.isEmpty
        ? "\(message.sender) is away" : "\(message.sender) is away (\(message.content))"
    case "back":
      return "\(message.sender) is back"
    case "nick" where !target.isEmpty:
      return "\(message.sender) is now known as \(target)"
    case "account":
      return message.content.isEmpty
        ? "\(message.sender) logged out" : "\(message.sender) logged in as \(message.content)"
    case "chghost":
      return "\(message.sender) changed host to \(message.content)"
    default:
      return [message.sender, message.content.isEmpty ? message.kind : message.content]
        .filter { !$0.isEmpty }
        .joined(separator: " ")
    }
  }
}

private struct PreviewCard: View {
  @Environment(AppModel.self) private var model
  let preview: Preview

  @ViewBuilder
  var body: some View {
    // Backend preview kinds are "image" (render the URL itself inline) and
    // "opengraph" (card); anything else is dropped (web parity: preview.ts).
    switch preview.kind {
    case "image":
      // Image URLs the client refuses to load inline (plain http) still get
      // the card so the preview isn't silently dropped.
      if let imageURL = model.inlineImageURL(preview) {
        linked { InlineImageView(url: imageURL) }
      } else {
        linked { card }
      }
    case "opengraph":
      linked { card }
    default:
      EmptyView()
    }
  }

  @ViewBuilder private func linked(@ViewBuilder content: () -> some View) -> some View {
    if let destination = URL(string: preview.url) {
      Link(destination: destination) {
        content()
      }
      .buttonStyle(.plain)
      #if os(macOS)
        .pointerStyle(.link)
      #endif
    } else {
      content()
    }
  }

  private var card: some View {
    HStack(spacing: 10) {
      if let imageURL = model.previewImageURL(preview) {
        CachedAsyncImage(url: imageURL) { image in
          image.resizable().scaledToFill()
        } placeholder: {
          Color.secondary.opacity(0.08)
        }
        .frame(width: 72, height: 54)
        .clipped()
        .clipShape(.rect(cornerRadius: 6))
      }
      VStack(alignment: .leading, spacing: 2) {
        Text(preview.siteName ?? URL(string: preview.url)?.host() ?? "Link")
          .font(.caption.weight(.semibold))
          .foregroundStyle(.secondary)
        Text(preview.title ?? preview.description ?? preview.url)
          .font(.body)
          .foregroundStyle(.primary)
          .lineLimit(2)
      }
      Spacer(minLength: 0)
    }
    .padding(8)
    .frame(maxWidth: 430)
    .background(.quaternary.opacity(0.5), in: .rect(cornerRadius: 8))
    .overlay {
      RoundedRectangle(cornerRadius: 8)
        .stroke(.separator, lineWidth: 0.5)
    }
  }
}

/// Full inline rendering for kind == "image" previews: the preview URL is
/// the image (web parity: renderImagePreview, max 480×320, contain-fit).
private struct InlineImageView: View {
  let url: URL

  var body: some View {
    CachedAsyncImage(
      url: url,
      content: { image in
        image
          .resizable()
          .scaledToFit()
          .frame(maxWidth: 480, maxHeight: 320, alignment: .leading)
          .clipShape(.rect(cornerRadius: 8))
          .overlay {
            RoundedRectangle(cornerRadius: 8)
              .stroke(.separator, lineWidth: 0.5)
          }
      },
      placeholder: {
        // Fixed-size placeholder: server width/height are usually 0 for
        // image previews, so they can't drive layout.
        Color.secondary.opacity(0.08)
          .frame(width: 240, height: 135)
          .clipShape(.rect(cornerRadius: 8))
      },
      failure: {
        // Broken image: nothing — the raw link stays in the message text.
        EmptyView()
      }
    )
  }
}

private struct ComposerPopupHeightKey: PreferenceKey {
  static let defaultValue: CGFloat = 0
  static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
    value = max(value, nextValue())
  }
}

private struct ComposerView: View {
  @Environment(AppModel.self) private var model
  let buffer: Buffer
  @FocusState private var focused: Bool
  @State private var popupSelection = 0
  // The popup derives from the text, so Esc-dismissal needs an explicit flag;
  // any text change re-arms it.
  @State private var popupDismissed = false
  // Measured height of the autocomplete panel, used to float it above the bar.
  @State private var popupHeight: CGFloat = 0
  #if os(macOS)
    @State private var importingImage = false
  #else
    @State private var photoItem: PhotosPickerItem?
  #endif

  var body: some View {
    @Bindable var model = model
    // Resolve once per render; the key handlers resolve once per event.
    let popup = self.popup
    VStack(alignment: .leading, spacing: 4) {
      if let error = model.composerError {
        Text(error)
          .font(.body)
          .foregroundStyle(.red)
          .transition(.move(edge: .bottom).combined(with: .opacity))
      }
      HStack(alignment: .bottom, spacing: 8) {
        Text(model.selectedNetwork?.nick ?? "you")
          .font(Theme.Fonts.nick.weight(.semibold))
          .foregroundStyle(.white)
        //          .padding(.bottom, 6)
        TextField(placeholder, text: $model.composerText, axis: .vertical)
          .textFieldStyle(.plain)
          .font(Theme.Fonts.message)
          .lineLimit(1...5)
          .focused($focused)
          .onSubmit { model.sendComposer() }
          .disabled(!canSend)
          .onKeyPress(keys: [.tab, .return]) { press in
            guard press.modifiers.isEmpty else { return .ignored }
            return acceptSelection() ? .handled : .ignored
          }
          .onKeyPress(keys: [.upArrow, .downArrow]) { press in
            // Modified arrows (⌥↑ etc.) belong to the menu-bar shortcuts.
            // Arrow keys always arrive with implicit flags set (.function,
            // .numericPad), so "bare" means no real chord modifiers — a plain
            // isEmpty check rejects every arrow press and kills history
            // browsing entirely.
            let chordModifiers: EventModifiers = [.command, .option, .control, .shift]
            guard press.modifiers.intersection(chordModifiers).isEmpty else {
              return .ignored
            }
            return handleArrow(up: press.key == .upArrow)
          }
          .onKeyPress(.escape) {
            guard self.popup != .none else { return .ignored }
            popupDismissed = true
            return .handled
          }
        attachButton
        Button(action: { model.sendComposer() }) {
          Image(systemName: "arrow.up.circle.fill")
            .font(.title2)
        }
        .buttonStyle(.plain)
        .disabled(
          !canSend || model.composerText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        )
        .help("Send message")
      }
      .padding(.horizontal, 10)
      .padding(.vertical, 7)
      .background(Color.lurkerControlBackground, in: .rect(cornerRadius: 8))
      .onDrop(of: [.image], isTargeted: nil, perform: handleDrop)
      .overlay {
        RoundedRectangle(cornerRadius: 8)
          .stroke(
            focused ? Color.accentColor : Color.lurkerSeparator,
            lineWidth: focused ? 1.5 : 0.5)
      }
      // Floated above the bar without affecting its layout. Anchored to the
      // bar's top-leading, then offset up by its own measured height so it
      // sits fully *above* the input. (An `.alignmentGuide(.top)` flip proved
      // unreliable — it rendered the panel directly over the bar, growing
      // downward off the window.)
      .overlay(alignment: .topLeading) {
        if popup != .none {
          // Clamp: the match list can shrink out from under the selection
          // (e.g. a member leaves mid-completion).
          ComposerPopupView(
            popup: popup,
            selection: min(popupSelection, max(0, popup.selectableCount - 1))
          ) { index in
            accept(index: index, in: popup)
          }
          .background(
            GeometryReader { proxy in
              Color.clear.preference(key: ComposerPopupHeightKey.self, value: proxy.size.height)
            }
          )
          .offset(y: -(popupHeight + 4))
          // Hide the pre-measurement frame so it never flashes over the bar.
          .opacity(popupHeight > 0 ? 1 : 0)
        }
      }
      .onPreferenceChange(ComposerPopupHeightKey.self) { popupHeight = $0 }
    }
    .padding(10)
    .background(.bar)
    .onChange(of: model.focusComposerRequest) { _, _ in focused = true }
    .onChange(of: model.composerText) { _, _ in
      popupSelection = 0
      popupDismissed = false
    }
    // Clicking away from the composer dismisses the popup (web parity: the web
    // client hides it on input blur).
    .onChange(of: focused) { _, isFocused in
      if !isFocused { popupDismissed = true }
    }
    #if os(macOS)
      // Autofocus on buffer switch is desktop-only: on iOS programmatic focus
      // raises the software keyboard over the timeline the user came to read.
      .onChange(of: buffer.id) { _, _ in focused = true }
    #endif
  }

  private var popup: ComposerPopup {
    guard !popupDismissed, canSend else { return .none }
    return ComposerPopup.resolve(
      text: model.composerText,
      buffer: buffer,
      members: model.members[buffer.id] ?? [],
      ownNick: model.selectedNetwork?.nick
    )
  }

  /// Tab/Enter acceptance of the highlighted nick/emoji row, clamped in case
  /// the match list shrank since the selection was made. The command popup is
  /// display-only, so those keys fall through (Enter submits).
  private func acceptSelection() -> Bool {
    let popup = self.popup
    let count = popup.selectableCount
    guard count > 0 else { return false }
    return accept(index: min(popupSelection, count - 1), in: popup)
  }

  @discardableResult
  private func accept(index: Int, in popup: ComposerPopup) -> Bool {
    switch popup {
    case .nick(let nicks):
      guard nicks.indices.contains(index) else { return false }
      model.composerText = NickCompletion.apply(nick: nicks[index])
      return true
    case .emoji(let matches):
      guard matches.indices.contains(index) else { return false }
      model.composerText = EmojiCompletion.apply(
        text: model.composerText, match: matches[index])
      return true
    case .command, .none:
      return false
    }
  }

  /// Bare ↑/↓ cycle the popup selection when one is open, otherwise browse
  /// per-buffer input history.
  private func handleArrow(up: Bool) -> KeyPress.Result {
    let count = popup.selectableCount
    if count > 0 {
      let current = min(popupSelection, count - 1)
      popupSelection = ((current + (up ? -1 : 1)) % count + count) % count
      return .handled
    }
    return model.navigateHistory(up: up) ? .handled : .ignored
  }

  private var canSend: Bool {
    model.connectionState == .connected && (buffer.kind != "channel" || buffer.joined)
  }

  /// Attach affordance: a `fileImporter`-backed button on macOS, a
  /// `PhotosPicker` on iOS. Both hand raw image data to
  /// `AppModel.attachImage`, which normalizes/uploads it and appends the
  /// resulting URL to the composer text.
  @ViewBuilder
  private var attachButton: some View {
    #if os(macOS)
      Button {
        importingImage = true
      } label: {
        attachIcon
      }
      .buttonStyle(.plain)
      .disabled(!canSend || model.isUploading)
      .help("Attach image")
      .fileImporter(isPresented: $importingImage, allowedContentTypes: [.image]) { result in
        guard case .success(let url) = result else { return }
        loadFile(at: url)
      }
    #else
      PhotosPicker(selection: $photoItem, matching: .images) {
        attachIcon
      }
      .disabled(!canSend || model.isUploading)
      .onChange(of: photoItem) { _, newValue in
        guard let newValue else { return }
        Task {
          if let data = try? await newValue.loadTransferable(type: Data.self) {
            await model.attachImage(data, sourceType: nil)
          }
          photoItem = nil
        }
      }
    #endif
  }

  @ViewBuilder
  private var attachIcon: some View {
    if model.isUploading {
      ProgressView()
        #if os(macOS)
          .controlSize(.small)
        #endif
    } else {
      Image(systemName: "paperclip")
        .font(.title2)
    }
  }

  #if os(macOS)
    private func loadFile(at url: URL) {
      let accessing = url.startAccessingSecurityScopedResource()
      defer { if accessing { url.stopAccessingSecurityScopedResource() } }
      guard let data = try? Data(contentsOf: url) else { return }
      let type = UTType(filenameExtension: url.pathExtension)
      Task {
        await model.attachImage(data, sourceType: type)
      }
    }
  #endif

  /// Drop handler for image files dragged onto the composer bar.
  private func handleDrop(_ providers: [NSItemProvider]) -> Bool {
    guard canSend else { return false }
    guard
      let provider = providers.first(where: {
        $0.hasItemConformingToTypeIdentifier(UTType.image.identifier)
      })
    else {
      return false
    }
    provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, _ in
      guard let data else { return }
      Task { await model.attachImage(data, sourceType: nil) }
    }
    return true
  }

  private var placeholder: String {
    if !canSend {
      return model.connectionState == .connected
        ? "This conversation is read-only" : "Waiting for connection…"
    }
    if buffer.kind == "status" {
      return "Commands only, e.g. /list, /nick, /msg NickServ …"
    }
    return "\(buffer.name)"
  }
}

private func isPresence(_ message: Message) -> Bool {
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
    if segment.bold == true { value.font = Theme.Fonts.message.bold() }
    if segment.italic == true { value.font = Theme.Fonts.message.italic() }
    if segment.underline == true { value.underlineStyle = .single }
    if segment.strike == true { value.strikethroughStyle = .single }
    if let foreground = segment.fg { value.foregroundColor = mircColor(foreground) }
    result.append(value)
  }
  if let detector = TimelineFormatters.linkDetector {
    for match in detector.matches(
      in: plainText, range: NSRange(plainText.startIndex..., in: plainText))
    {
      guard let url = match.url,
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

private func mircColor(_ value: Int) -> Color {
  let palette: [Color] = [
    .white, .black, .blue, .green, .red, .brown, .purple, .orange,
    .yellow, .green, .teal, .cyan, .blue, .pink, .gray, .secondary,
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
private func displayDay(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else { return dayKey(raw) }
  return date.formatted(.dateTime.weekday(.wide).month(.wide).day().year())
}

@MainActor
private func displayTime(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else {
    return String(raw.dropFirst(11).prefix(5))
  }
  return date.formatted(date: .omitted, time: .shortened)
}

@MainActor
private func parseTimestamp(_ raw: String) -> Date? {
  if let date = try? Date(raw, strategy: .iso8601) {
    return date
  }
  return TimelineFormatters.iso8601.date(from: raw)
}

@MainActor
private enum TimelineFormatters {
  static let linkDetector = try? NSDataDetector(
    types: NSTextCheckingResult.CheckingType.link.rawValue)
  static let iso8601 = ISO8601DateFormatter()
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
