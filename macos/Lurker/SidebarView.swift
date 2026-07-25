import SwiftUI

struct SidebarView: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    List(selection: selection) {
      if !model.pinnedBuffers.isEmpty {
        Section("Pinned") {
          ForEach(model.pinnedBuffers) { buffer in
            BufferRow(buffer: buffer, network: model.networks[buffer.networkID])
              .tag(buffer.id)
              .bufferMenu(buffer, model: model)
          }
        }
      }

      ForEach(model.orderedNetworks.filter { !$0.disabled }) { network in
        let groups = model.sidebarBuffers(for: network.id)
        Section {
          NetworkHeader(
            network: network,
            collapsed: model.collapsedNetworks.contains(network.id),
            unread: networkUnread(network.id),
            mentions: networkMentions(network.id)
          ) {
            model.toggleNetwork(network.id)
          }
          if !model.collapsedNetworks.contains(network.id) {
            ForEach(groups.all) { buffer in
              BufferRow(buffer: buffer, network: network)
                .tag(buffer.id)
                .bufferMenu(buffer, model: model)
            }
          }
        }
      }

      let disabled = model.orderedNetworks.filter(\.disabled)
      if !disabled.isEmpty {
        Section("Disabled") {
          ForEach(disabled) { network in
            Label(network.name, systemImage: "pause.circle")
              .foregroundStyle(.secondary)
          }
        }
      }
    }
    .listStyle(.sidebar)
    .navigationTitle("Lurker")
    .safeAreaInset(edge: .bottom) {
      VStack(spacing: 6) {
        Divider()
        HStack {
          Label(model.connectionState.label, systemImage: model.connectionState.symbol)
          Spacer()
          SettingsLink {
            Image(systemName: "gearshape")
          }
          .buttonStyle(.plain)
          .help("Settings")
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
      }
      .background(.bar)
    }
  }

  private var selection: Binding<UUID?> {
    Binding(
      get: { model.selectedBufferID },
      set: { if let value = $0 { model.selectBuffer(value) } }
    )
  }

  private func networkUnread(_ id: UUID) -> Int {
    model.buffers.values.filter { $0.networkID == id }.reduce(0) { $0 + $1.unread }
  }

  private func networkMentions(_ id: UUID) -> Int {
    model.buffers.values.filter { $0.networkID == id }.reduce(0) { $0 + $1.mentions }
  }
}

private struct NetworkHeader: View {
  let network: Network
  let collapsed: Bool
  let unread: Int
  let mentions: Int
  let toggle: () -> Void

  var body: some View {
    Button(action: toggle) {
      HStack(spacing: 7) {
        Image(systemName: collapsed ? "chevron.right" : "chevron.down")
          .font(.caption.weight(.semibold))
          .frame(width: 8)
        Circle()
          .fill(statusColor)
          .frame(width: 7, height: 7)
        Text(network.name)
          .font(.footnote.weight(.semibold))
          .lineLimit(1)
        Spacer()
        if mentions > 0 {
          CountBadge(count: mentions, mention: true)
        } else if unread > 0 {
          CountBadge(count: unread, mention: false)
        }
      }
      .contentShape(.rect)
    }
    .buttonStyle(.plain)
  }

  private var statusColor: Color {
    switch network.status {
    case "connected": .green
    case "connecting": .orange
    default: .secondary.opacity(0.6)
    }
  }
}

private struct BufferRow: View {
  let buffer: Buffer
  let network: Network?

  var body: some View {
    HStack(spacing: 7) {
      if let icon {
        Image(systemName: icon)
          .font(.footnote)
          .foregroundStyle(.secondary)
          .frame(width: 12)
      }
      Text(label)
        .lineLimit(1)
        .foregroundStyle(textStyle)
      Spacer(minLength: 4)
      if buffer.mentions > 0 {
        CountBadge(count: buffer.mentions, mention: true)
      } else if buffer.unread > 0 {
        CountBadge(count: buffer.unread, mention: false)
      }
    }
    .help(network.map { "\(buffer.name) — \($0.name)" } ?? buffer.name)
  }

  private var label: String {
    buffer.kind == "status" ? "Status" : buffer.name
  }

  private var icon: String? {
    switch buffer.kind {
    case "status": "server.rack"
    case "query": "person"
    default: nil
    }
  }

  private var textStyle: some ShapeStyle {
    if buffer.kind == "channel" {
      return buffer.joined ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary)
    }
    return buffer.unread > 0 ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary)
  }
}

private struct CountBadge: View {
  let count: Int
  let mention: Bool

  var body: some View {
    Text(count.formatted())
      .font(.caption.monospacedDigit().weight(.semibold))
      .padding(.horizontal, 5)
      .padding(.vertical, 1)
      .foregroundStyle(mention ? Color.white : Color.secondary)
      .background(mention ? Color.orange : Color.secondary.opacity(0.14), in: .capsule)
      .accessibilityLabel(mention ? "\(count) mentions" : "\(count) unread messages")
  }
}

extension View {
  fileprivate func bufferMenu(_ buffer: Buffer, model: AppModel) -> some View {
    contextMenu {
      if buffer.kind == "channel" {
        Button(buffer.pinned ? "Unpin" : "Pin") {
          model.updateBuffer(buffer.id, BufferSettingsPatch(pinned: !buffer.pinned))
        }
        Divider()
        Toggle(
          "Show Link Previews",
          isOn: Binding(
            get: { buffer.showEmbeds },
            set: { model.updateBuffer(buffer.id, BufferSettingsPatch(showEmbeds: $0)) }
          ))
        Toggle(
          "Show Presence Events",
          isOn: Binding(
            get: { buffer.showPresenceEvents },
            set: { model.updateBuffer(buffer.id, BufferSettingsPatch(showPresenceEvents: $0)) }
          ))
        Toggle(
          "Collapse Presence Events",
          isOn: Binding(
            get: { buffer.collapsePresenceEvents },
            set: { model.updateBuffer(buffer.id, BufferSettingsPatch(collapsePresenceEvents: $0)) }
          ))
      }
    }
  }
}

#Preview {
  SidebarView()
    .environment(AppModel.preview())
    .tint(.mint)
    .frame(width: 260, height: 600)
}
