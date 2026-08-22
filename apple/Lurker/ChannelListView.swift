import SwiftUI

/// Result sheet for `/list`: the channels a network reported, joinable in
/// place. Presented whenever `AppModel.channelList` is non-nil.
struct ChannelListView: View {
  @Environment(AppModel.self) private var model
  let result: ChannelListEvent
  @State private var filter = ""

  var body: some View {
    VStack(spacing: 0) {
      header
      Divider()
      if !result.done {
        ProgressView("Listing channels…")
          .frame(maxWidth: .infinity, maxHeight: .infinity)
      } else if filtered.isEmpty {
        ContentUnavailableView {
          Label("No Channels", systemImage: "number")
        } description: {
          Text(
            filter.isEmpty ? "The network returned an empty list." : "No channels match the filter."
          )
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
      } else {
        list
      }
    }
    .frame(minWidth: 460, minHeight: 380)
  }

  private var header: some View {
    HStack {
      Text(title)
        .font(.headline)
      Spacer()
      TextField("Filter", text: $filter)
        .textFieldStyle(.roundedBorder)
        .frame(maxWidth: 200)
      Button("Done") { model.channelList = nil }
        .keyboardShortcut(.cancelAction)
    }
    .padding(12)
  }

  private var title: String {
    let network = model.networks[result.networkID]?.name ?? "Network"
    let count = result.entries?.count ?? 0
    return "\(network) — \(count) channel\(count == 1 ? "" : "s")"
  }

  private var filtered: [ChannelListEntry] {
    let entries = (result.entries ?? []).sorted { $0.count > $1.count }
    guard !filter.isEmpty else { return entries }
    return entries.filter {
      $0.name.localizedCaseInsensitiveContains(filter)
        || ($0.topic?.localizedCaseInsensitiveContains(filter) ?? false)
    }
  }

  private var list: some View {
    List(filtered) { entry in
      HStack(alignment: .firstTextBaseline, spacing: 10) {
        VStack(alignment: .leading, spacing: 2) {
          Text(entry.name)
            .font(Theme.Fonts.nick.weight(.semibold))
          if let topic = entry.topic, !topic.isEmpty {
            Text(topic)
              .font(.body)
              .foregroundStyle(.secondary)
              .lineLimit(2)
          }
        }
        Spacer()
        Text("\(entry.count)")
          .font(.callout.monospacedDigit())
          .foregroundStyle(.secondary)
        Button("Join") { join(entry) }
          .buttonStyle(.bordered)
          .controlSize(.small)
      }
      .padding(.vertical, 2)
    }
    .listStyle(.inset)
  }

  private func join(_ entry: ChannelListEntry) {
    model.command(
      ClientCommand(type: "join", networkID: result.networkID, channel: entry.name))
    model.channelList = nil
  }
}
