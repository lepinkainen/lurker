import SwiftUI

struct ChannelSwitcher: View {
  @Environment(AppModel.self) private var model
  @Environment(\.dismiss) private var dismiss
  @State private var query = ""
  @State private var selection: UUID?
  @FocusState private var focused: Bool

  var body: some View {
    VStack(spacing: 0) {
      HStack(spacing: 9) {
        Image(systemName: "magnifyingglass")
          .foregroundStyle(.secondary)
        TextField("Jump to a channel or conversation", text: $query)
          .textFieldStyle(.plain)
          .focused($focused)
          .onSubmit(openSelection)
      }
      .font(.title3)
      .padding(14)

      Divider()

      List(results, selection: $selection) { result in
        HStack {
          Image(
            systemName: result.buffer.kind == "query"
              ? "person" : result.buffer.kind == "status" ? "server.rack" : "number"
          )
          .foregroundStyle(.secondary)
          .frame(width: 16)
          VStack(alignment: .leading, spacing: 1) {
            Text(result.buffer.kind == "status" ? "Status" : result.buffer.name)
            Text(result.network.name)
              .font(.caption)
              .foregroundStyle(.secondary)
          }
          Spacer()
          if result.buffer.mentions > 0 {
            Text(result.buffer.mentions.formatted())
              .font(.caption2.weight(.bold))
              .foregroundStyle(.orange)
          } else if result.buffer.unread > 0 {
            Text(result.buffer.unread.formatted())
              .font(.caption2)
              .foregroundStyle(.secondary)
          }
        }
        .tag(result.buffer.id)
        .contentShape(.rect)
        .onTapGesture(count: 2) {
          open(result.buffer.id)
        }
      }
      .listStyle(.inset)
      .overlay {
        if results.isEmpty {
          ContentUnavailableView.search(text: query)
        }
      }
    }
    .frame(width: 520, height: 430)
    .onAppear {
      selection = results.first?.buffer.id
      focused = true
    }
    .onChange(of: query) { _, _ in
      selection = results.first?.buffer.id
    }
  }

  private var results: [SwitcherResult] {
    let values = model.buffers.values.compactMap { buffer -> SwitcherResult? in
      guard let network = model.networks[buffer.networkID], !network.disabled else { return nil }
      return SwitcherResult(buffer: buffer, network: network)
    }
    let filtered =
      query.isEmpty
      ? values
      : values.filter {
        $0.buffer.name.localizedCaseInsensitiveContains(query)
          || $0.network.name.localizedCaseInsensitiveContains(query)
          || "\($0.buffer.name) \($0.network.name)".localizedCaseInsensitiveContains(query)
      }
    return filtered.sorted {
      if ($0.buffer.mentions > 0) != ($1.buffer.mentions > 0) {
        return $0.buffer.mentions > 0
      }
      if ($0.buffer.unread > 0) != ($1.buffer.unread > 0) {
        return $0.buffer.unread > 0
      }
      if $0.network.sortOrder != $1.network.sortOrder {
        return $0.network.sortOrder < $1.network.sortOrder
      }
      return $0.buffer.name.localizedCaseInsensitiveCompare($1.buffer.name) == .orderedAscending
    }
  }

  private func openSelection() {
    if let selection {
      open(selection)
    }
  }

  private func open(_ id: UUID) {
    model.selectBuffer(id)
    dismiss()
  }
}

private struct SwitcherResult: Identifiable {
  var id: UUID { buffer.id }
  let buffer: Buffer
  let network: Network
}
