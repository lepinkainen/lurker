import SwiftUI

// MARK: - ChannelSwitcher

struct ChannelSwitcher: View {

  // MARK: Internal

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
      .font(.title2)
      .padding(14)

      Divider()

      List(results, selection: $selection) { result in
        HStack {
          if let icon = switcherIcon(for: result.buffer.kind) {
            Image(systemName: icon)
              .foregroundStyle(.secondary)
              .frame(width: 16)
          } else {
            Color.clear.frame(width: 16)
          }
          VStack(alignment: .leading, spacing: 1) {
            Text(result.buffer.kind == "status" ? "Status" : result.buffer.name)
              .foregroundStyle(
                result.buffer.kind == "channel" && !result.buffer.joined
                  ? AnyShapeStyle(.secondary)
                  : AnyShapeStyle(.primary)
              )
            Text(result.network.name)
              .font(.footnote)
              .foregroundStyle(.secondary)
          }
          Spacer()
          if result.buffer.mentions > 0 {
            Text(result.buffer.mentions.formatted())
              .font(.caption.weight(.bold))
              .foregroundStyle(.orange)
          } else if result.buffer.unread > 0 {
            Text(result.buffer.unread.formatted())
              .font(.caption)
              .foregroundStyle(.secondary)
          }
        }
        .tag(result.buffer.id)
        .contentShape(.rect)
        #if os(macOS)
        .onTapGesture(count: 2) {
          open(result.buffer.id)
        }
        #else
        .onTapGesture {
          open(result.buffer.id)
        }
        #endif
      }
      .listStyle(.inset)
      .overlay {
        if results.isEmpty {
          ContentUnavailableView.search(text: query)
        }
      }
    }
    #if os(macOS)
    .frame(width: 520, height: 430)
    #endif
    .onAppear {
      selection = results.first?.buffer.id
      focused = true
    }
    .onChange(of: query) { _, _ in
      selection = results.first?.buffer.id
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model
  @Environment(\.dismiss) private var dismiss
  @State private var query = ""
  @State private var selection: UUID?
  @FocusState private var focused: Bool

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

private func switcherIcon(for kind: String) -> String? {
  switch kind {
  case "status": "server.rack"
  case "query": "person"
  default: nil
  }
}

// MARK: - SwitcherResult

private struct SwitcherResult: Identifiable {
  let buffer: Buffer
  let network: Network

  var id: UUID {
    buffer.id
  }
}

#if DEBUG
#Preview {
  ChannelSwitcher()
    .environment(AppModel.preview())
    .tint(.mint)
}
#endif
