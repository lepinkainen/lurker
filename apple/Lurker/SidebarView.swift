import SwiftUI

struct SidebarView: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 0) {
        sidebarContent
      }
      .padding(.vertical, 6)
    }
    .navigationTitle("Lurker")
    .safeAreaInset(edge: .bottom) {
      VStack(spacing: 6) {
        Divider()
        HStack {
          Label(model.connectionState.label, systemImage: model.connectionState.symbol)
          Spacer()
          #if os(macOS)
            // `SettingsLink` needs a `Settings` scene, which the Previews host lacks;
            // in previews it joins the window key-view loop and crashes on refresh.
            if !ProcessInfo.isPreviewOrUITest {
              SettingsLink {
                Image(systemName: "gearshape")
              }
              .buttonStyle(.plain)
              .help("Settings")
            }
          #else
            // iOS has no `Settings` scene; open settings as an in-app sheet.
            Button {
              model.showingSettings = true
            } label: {
              Image(systemName: "gearshape")
            }
            .buttonStyle(.plain)
          #endif
        }
        .font(.body)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
      }
      .background(.bar)
    }
  }

  @ViewBuilder private var sidebarContent: some View {
    if !model.pinnedBuffers.isEmpty {
      SidebarSectionHeader("Pinned")
      ForEach(model.pinnedBuffers) { buffer in
        SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
          model.selectBuffer(buffer.id)
        } content: {
          BufferRow(buffer: buffer, network: model.networks[buffer.networkID])
        }
        .bufferMenu(buffer, model: model)
      }
    }

    ForEach(model.orderedNetworks.filter { !$0.disabled }) { network in
      let groups = model.sidebarBuffers(for: network.id)
      let statusBufferID = groups.status.first?.id
      NetworkHeaderRow(
        network: network,
        isSelected: statusBufferID != nil && model.selectedBufferID == statusBufferID,
        unread: networkUnread(network.id),
        mentions: networkMentions(network.id)
      ) {
        guard let statusBufferID else { return }
        model.selectBuffer(statusBufferID)
      } statusAction: {
        guard let statusBufferID else { return }
        model.selectBuffer(statusBufferID)
      }
      // Status buffer is represented by the network header row above, so the
      // per-network rows list only channels and queries (no duplicate "Status").
      ForEach(groups.channels + groups.queries) { buffer in
        SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
          model.selectBuffer(buffer.id)
        } content: {
          BufferRow(buffer: buffer, network: network)
            .padding(.leading, 14)
        }
        .bufferMenu(buffer, model: model)
      }
    }

    let disabled = model.orderedNetworks.filter(\.disabled)
    if !disabled.isEmpty {
      SidebarSectionHeader("Disabled")
      ForEach(disabled) { network in
        Label(network.name, systemImage: "pause.circle")
          .foregroundStyle(.secondary)
          .font(.subheadline)
          .padding(.horizontal, 12)
          .padding(.vertical, 4)
      }
    }
  }

  private func networkUnread(_ id: UUID) -> Int {
    model.buffers.values.filter { $0.networkID == id }.reduce(0) { $0 + $1.unread }
  }

  private func networkMentions(_ id: UUID) -> Int {
    model.buffers.values.filter { $0.networkID == id }.reduce(0) { $0 + $1.mentions }
  }
}

/// A section label matching the muted, uppercase style `List` sections use by default.
private struct SidebarSectionHeader: View {
  let title: String

  init(_ title: String) {
    self.title = title
  }

  var body: some View {
    Text(title)
      .font(.caption.weight(.semibold))
      .foregroundStyle(.secondary)
      .padding(.horizontal, 12)
      .padding(.top, 10)
      .padding(.bottom, 2)
  }
}

/// Wraps sidebar row content in a tappable, highlightable container shared by
/// buffer rows and the network header row, so selection styling stays consistent
/// across the custom `ScrollView`/`LazyVStack` sidebar (used instead of a
/// `.sidebar` `List`, whose outline view crashed the SwiftUI Previews host).
private struct SidebarRow<Content: View>: View {
  let isSelected: Bool
  let action: () -> Void
  @ViewBuilder let content: Content

  var body: some View {
    Button(action: action) {
      content
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .contentShape(.rect)
    }
    .buttonStyle(.plain)
    .background(
      RoundedRectangle(cornerRadius: 6)
        .fill(Color.accentColor.opacity(isSelected ? 0.18 : 0))
    )
    .padding(.horizontal, 4)
  }
}

private struct NetworkHeaderRow: View {
  let network: Network
  let isSelected: Bool
  let unread: Int
  let mentions: Int
  let action: () -> Void
  let statusAction: () -> Void

  var body: some View {
    HStack(spacing: 4) {
      Button(action: action) {
        HStack(spacing: 7) {
          Circle()
            .fill(statusColor)
            .frame(width: 7, height: 7)
          Text(network.name)
            .font(.headline)
            .lineLimit(1)
          Spacer()
          if mentions > 0 {
            CountBadge(count: mentions, mention: true)
          } else if unread > 0 {
            CountBadge(count: unread, mention: false)
          }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .contentShape(.rect)
      }
      .buttonStyle(.plain)

      Menu {
        Button("Network Status", action: statusAction)
        // TODO: reconnect / network settings once backend ClientCommand exists
      } label: {
        Image(systemName: "ellipsis")
          .font(.caption.weight(.semibold))
          .foregroundStyle(.secondary)
          .frame(width: 16, height: 16)
      }
      .menuIndicator(.hidden)
      .fixedSize()
      #if os(macOS)
        .menuStyle(.borderlessButton)
      #endif
    }
    .padding(.trailing, 6)
    .background(
      RoundedRectangle(cornerRadius: 6)
        .fill(Color.accentColor.opacity(isSelected ? 0.18 : 0))
    )
    .padding(.horizontal, 4)
    .padding(.top, 4)
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
          .font(.body)
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

#Preview("Multiple networks") {
  SidebarView()
    .environment(AppModel.previewSidebar())
    .tint(.mint)
    .frame(width: 260, height: 640)
}

#Preview("Fixture") {
  SidebarView()
    .environment(AppModel.preview())
    .tint(.mint)
    .frame(width: 260, height: 600)
}
