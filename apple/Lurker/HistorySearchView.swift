import SwiftUI

// MARK: - HistorySearchView

/// Full-text history search. macOS presents it as a sheet shaped like the
/// channel switcher; iOS pushes it (compact) or wraps it in a sheet (iPad).
/// Scope defaults to the active buffer; "All Channels" searches every network.
/// Selecting a hit expands a context window beneath it (macOS) or pushes one
/// (iOS). Opening the hit in its channel is deliberately not here yet.
struct HistorySearchView: View {

  // MARK: Internal

  var body: some View {
    #if os(macOS)
    macBody
      .frame(width: 640, height: 560)
    #else
    iosBody
    #endif
  }

  // MARK: Private

  private enum Scope: Hashable {
    case buffer
    case all
  }

  @Environment(AppModel.self) private var model
  @Environment(\.dismiss) private var dismiss
  @State private var query = ""
  @State private var scope = Scope.buffer
  @State private var results = [Message]()
  @State private var exhausted = false
  @State private var searching = false
  @State private var expandedID: UUID?
  @State private var context = [Message]()
  @State private var selection: UUID?
  @FocusState private var focused: Bool

  private var terms: [String] {
    query.split(whereSeparator: \.isWhitespace).map(String.init)
  }

  private var scopeBufferID: UUID? {
    scope == .buffer ? model.selectedBufferID : nil
  }

  private var scopeTitle: String {
    model.selectedBuffer.map { $0.kind == "status" ? "Status" : $0.name } ?? "This Channel"
  }

  private var scopePicker: some View {
    Picker("Scope", selection: $scope) {
      Text(scopeTitle).tag(Scope.buffer)
      Text("All Channels").tag(Scope.all)
    }
    .pickerStyle(.segmented)
    .labelsHidden()
    .disabled(model.selectedBufferID == nil)
  }

  private var matchCount: String {
    if searching, results.isEmpty {
      return "Searching…"
    }
    if query.isEmpty {
      return ""
    }
    return "\(results.count)\(exhausted ? "" : "+") matches"
  }

  #if os(macOS)
  private var macBody: some View {
    VStack(spacing: 0) {
      HStack(spacing: 9) {
        Image(systemName: "magnifyingglass")
          .foregroundStyle(.secondary)
        TextField("Search messages", text: $query)
          .textFieldStyle(.plain)
          .focused($focused)
          .onSubmit(toggleSelection)
      }
      .font(.title2)
      .padding(14)

      HStack {
        scopePicker
          .fixedSize()
        Spacer()
        Text(matchCount)
          .font(.footnote)
          .foregroundStyle(.secondary)
      }
      .padding(.horizontal, 14)
      .padding(.bottom, 10)

      Divider()

      List(results, selection: $selection) { message in
        row(message)
          .tag(message.id)
          .contentShape(.rect)
          .onTapGesture { toggle(message.id) }
          .onAppear { loadMoreIfLast(message) }
      }
      .listStyle(.inset)
      .overlay { emptyState }

      HStack {
        Text("↑↓ select · ↩ context · esc close")
        Spacer()
        Text("⇧⌘F")
      }
      .font(.caption)
      .foregroundStyle(.secondary)
      .padding(.horizontal, 14)
      .padding(.vertical, 8)
    }
    .onAppear { focused = true }
    .onExitCommand {
      if expandedID != nil {
        expandedID = nil
      } else {
        dismiss()
      }
    }
    .task(id: SearchKey(query: query, bufferID: scopeBufferID)) { await search() }
    .task(id: expandedID) { await loadContext() }
  }
  #else
  private var iosBody: some View {
    List {
      Section {
        scopePicker
          .listRowBackground(Color.clear)
          .listRowInsets(EdgeInsets())
      }
      Section(matchCount) {
        ForEach(results) { message in
          NavigationLink {
            SearchContextView(hit: message, terms: terms)
          } label: {
            row(message)
          }
          .onAppear { loadMoreIfLast(message) }
        }
      }
    }
    .listStyle(.insetGrouped)
    .overlay { emptyState }
    .navigationTitle("Search")
    .navigationBarTitleDisplayMode(.inline)
    .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search messages")
    .task(id: SearchKey(query: query, bufferID: scopeBufferID)) { await search() }
  }
  #endif

  @ViewBuilder
  private var emptyState: some View {
    if !query.isEmpty, !searching, results.isEmpty {
      ContentUnavailableView.search(text: query)
    }
  }

  private func row(_ message: Message) -> some View {
    VStack(alignment: .leading, spacing: 6) {
      HStack(alignment: .top, spacing: 8) {
        NickAvatar(
          nick: message.sender,
          colorIndex: message.senderColor,
          isBot: model.isBot(message.sender),
          networkID: message.networkID,
          hasAvatar: model.hasAvatar(message.sender),
          size: 28,
        )
        VStack(alignment: .leading, spacing: 1) {
          HStack(alignment: .firstTextBaseline) {
            Text(message.sender)
              .font(Theme.Fonts.nick.weight(.semibold))
            if scope == .all {
              Text(bufferLabel(message))
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            Spacer()
            Text(shortTimestamp(message.ts))
              .font(Theme.Fonts.timestamp)
              .foregroundStyle(.secondary)
          }
          Text(highlighted(message.content, terms: terms))
            .font(Theme.Fonts.message)
            .lineLimit(expandedID == message.id ? nil : 2)
        }
      }
      if expandedID == message.id {
        ContextCard(hit: message, messages: context, terms: terms)
          .padding(.leading, 36)
      }
    }
    .padding(.vertical, 2)
  }

  private func bufferLabel(_ message: Message) -> String {
    let buffer = model.buffers[message.bufferID]?.name ?? "?"
    guard let network = model.networks[message.networkID]?.name else { return buffer }
    return "\(network) · \(buffer)"
  }

  private func toggleSelection() {
    guard let selection else { return }
    toggle(selection)
  }

  private func toggle(_ id: UUID) {
    selection = id
    expandedID = expandedID == id ? nil : id
  }

  private func loadMoreIfLast(_ message: Message) {
    guard message.id == results.last?.id, !exhausted, !searching else { return }
    Task { await search(before: message.id) }
  }

  private func search(before: UUID? = nil) async {
    guard let transport = model.transport else { return }
    if before == nil {
      // Debounce: a fresh keystroke replaces this task via .task(id:).
      try? await Task.sleep(for: .milliseconds(250))
      guard !Task.isCancelled else { return }
      expandedID = nil
    }
    guard !terms.isEmpty else {
      results = []
      exhausted = true
      return
    }
    searching = true
    defer { searching = false }
    do {
      let page = try await transport.searchMessages(query: query, bufferID: scopeBufferID, before: before)
      guard !Task.isCancelled else { return }
      results = before == nil ? page : results + page
      exhausted = page.count < 50
      if before == nil, selection == nil || !results.contains(where: { $0.id == selection }) {
        selection = results.first?.id
      }
    } catch is CancellationError {
      return
    } catch {
      if before == nil {
        results = []
      }
      exhausted = true
    }
  }

  private func loadContext() async {
    context = []
    guard
      let expandedID, let transport = model.transport,
      let hit = results.first(where: { $0.id == expandedID })
    else { return }
    context = (try? await transport.fetchContext(bufferID: hit.bufferID, around: hit.id)) ?? []
  }

}

// MARK: - SearchKey

private struct SearchKey: Hashable {
  let query: String
  let bufferID: UUID?
}

// MARK: - ContextCard

/// The ±N messages around a hit, hit row tinted. Shared by the macOS inline
/// expansion and the iOS pushed screen.
private struct ContextCard: View {

  let hit: Message
  let messages: [Message]
  let terms: [String]

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      if messages.isEmpty {
        ProgressView()
          .controlSize(.small)
          .frame(maxWidth: .infinity)
          .padding(8)
      } else {
        ForEach(messages) { message in
          HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(displayTime(message.ts))
              .font(Theme.Fonts.timestamp)
              .foregroundStyle(.tertiary)
            Text(message.sender)
              .font(Theme.Fonts.nick)
              .foregroundStyle(nickPaletteColor(message.senderColor))
              .lineLimit(1)
            Text(message.id == hit.id ? highlighted(message.content, terms: terms) : AttributedString(message.content))
              .font(Theme.Fonts.message)
              .foregroundStyle(message.id == hit.id ? .primary : .secondary)
          }
          .padding(.vertical, 2)
          .padding(.horizontal, 8)
          .frame(maxWidth: .infinity, alignment: .leading)
          .background(message.id == hit.id ? Color.yellow.opacity(0.18) : .clear)
        }
      }
    }
    .padding(.vertical, 4)
    .background(Color.lurkerControlBackground, in: RoundedRectangle(cornerRadius: 8))
  }

}

#if os(iOS)
// MARK: - SearchContextView

private struct SearchContextView: View {
  let hit: Message
  let terms: [String]

  var body: some View {
    ScrollView {
      ContextCard(hit: hit, messages: messages, terms: terms)
        .padding()
    }
    .navigationTitle(model.buffers[hit.bufferID]?.name ?? "Context")
    .navigationBarTitleDisplayMode(.inline)
    .task {
      guard let transport = model.transport else { return }
      messages = (try? await transport.fetchContext(bufferID: hit.bufferID, around: hit.id)) ?? []
    }
  }

  @Environment(AppModel.self) private var model
  @State private var messages = [Message]()
}
#endif

// MARK: - Helpers

/// Marks every case-insensitive occurrence of each search term. Terms are
/// whitespace-split words, matching how the server tokenizes the query.
private func highlighted(_ text: String, terms: [String]) -> AttributedString {
  var result = AttributedString(text)
  for term in terms where !term.isEmpty {
    var searchRange = text.startIndex..<text.endIndex
    while let found = text.range(of: term, options: [.caseInsensitive, .diacriticInsensitive], range: searchRange) {
      if let range = Range(found, in: result) {
        result[range].backgroundColor = Color.yellow.opacity(0.35)
        result[range].font = Theme.Fonts.message.weight(.semibold)
      }
      searchRange = found.upperBound..<text.endIndex
    }
  }
  return result
}

/// "14:41" for today, "Mon 22:17" within the week, "28 Sep" beyond.
@MainActor
private func shortTimestamp(_ raw: String) -> String {
  guard let date = parseTimestamp(raw) else { return displayTime(raw) }
  let calendar = Calendar.current
  if calendar.isDateInToday(date) {
    return date.formatted(date: .omitted, time: .shortened)
  }
  if let weekAgo = calendar.date(byAdding: .day, value: -6, to: .now), date > weekAgo {
    return date.formatted(.dateTime.weekday(.abbreviated).hour().minute())
  }
  return date.formatted(.dateTime.day().month(.abbreviated))
}

#if DEBUG
#Preview {
  HistorySearchView()
    .environment(AppModel.preview())
    .tint(.mint)
}
#endif
