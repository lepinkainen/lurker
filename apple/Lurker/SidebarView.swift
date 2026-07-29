import SwiftUI

struct SidebarView: View {
  @Environment(AppModel.self) private var model
  @State private var pendingDelete: Buffer?

  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 0) {
        sidebarContent
      }
      .padding(.vertical, 6)
    }
    .navigationTitle("Lurker")
    .alert(
      "Delete \(pendingDelete?.name ?? "buffer")?",
      isPresented: Binding(
        get: { pendingDelete != nil },
        set: { if !$0 { pendingDelete = nil } }
      ),
      presenting: pendingDelete
    ) { buffer in
      Button("Delete Forever", role: .destructive) {
        model.deleteBuffer(buffer.id)
      }
      Button("Cancel", role: .cancel) {}
    } message: { _ in
      Text("This permanently removes the buffer and all of its history. This cannot be undone.")
    }
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
        .bufferMenu(buffer, model: model) { pendingDelete = $0 }
      }
    }

    ForEach(model.orderedNetworks.filter { !$0.disabled }) { network in
      let groups = model.sidebarBuffers(for: network.id)
      let statusBuffer = groups.status.first
      let statusBufferID = statusBuffer?.id
      let isCollapsed = model.collapsedNetworks.contains(network.id)
      // Collapsed headers aggregate the whole network (web parity); expanded
      // headers keep showing only the status buffer's own counts.
      let headerCounts =
        isCollapsed
        ? model.networkAggregateCounts(network.id)
        : (unread: statusBuffer?.unread ?? 0, mentions: statusBuffer?.mentions ?? 0)
      NetworkHeaderRow(
        network: network,
        isSelected: statusBufferID != nil && model.selectedBufferID == statusBufferID,
        isCollapsed: isCollapsed,
        unread: headerCounts.unread,
        mentions: headerCounts.mentions
      ) {
        guard let statusBufferID else { return }
        model.selectBuffer(statusBufferID)
      } statusAction: {
        guard let statusBufferID else { return }
        model.selectBuffer(statusBufferID)
      } collapseAction: {
        model.toggleNetworkCollapsed(network.id)
      }
      if !isCollapsed {
        // Status buffer is represented by the network header row above, so the
        // per-network rows list only channels and queries (no duplicate "Status").
        ForEach(groups.channels + groups.queries) { buffer in
          SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
            model.selectBuffer(buffer.id)
          } content: {
            BufferRow(buffer: buffer, network: network)
              .padding(.leading, 14)
          }
          .bufferMenu(buffer, model: model) { pendingDelete = $0 }
        }
        if !groups.archived.isEmpty {
          ArchivesToggleRow(
            count: groups.archived.count,
            isOpen: model.archivesOpen.contains(network.id)
          ) {
            model.toggleArchives(network.id)
          }
          if model.archivesOpen.contains(network.id) {
            ForEach(groups.archived) { buffer in
              SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
                model.selectBuffer(buffer.id)
              } content: {
                BufferRow(buffer: buffer, network: network)
                  .padding(.leading, 22)
              }
              .bufferMenu(buffer, model: model) { pendingDelete = $0 }
            }
          }
        }
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

/// The per-network "Archives (n)" fold row — folded by default, IRCCloud-style.
private struct ArchivesToggleRow: View {
  let count: Int
  let isOpen: Bool
  let action: () -> Void

  var body: some View {
    Button(action: action) {
      HStack(spacing: 5) {
        Image(systemName: "chevron.right")
          .font(.caption2.weight(.semibold))
          .rotationEffect(.degrees(isOpen ? 90 : 0))
        Text("Archives")
        Text(count.formatted())
          .foregroundStyle(.tertiary)
        Spacer()
      }
      .font(.caption.weight(.semibold))
      .foregroundStyle(.secondary)
      .padding(.horizontal, 8)
      .padding(.leading, 14)
      .padding(.vertical, 4)
      .contentShape(.rect)
    }
    .buttonStyle(.plain)
    .padding(.horizontal, 4)
    .accessibilityLabel("Archives, \(count) buffers, \(isOpen ? "expanded" : "collapsed")")
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
  let isCollapsed: Bool
  let unread: Int
  let mentions: Int
  let action: () -> Void
  let statusAction: () -> Void
  let collapseAction: () -> Void

  var body: some View {
    HStack(spacing: 4) {
      Button(action: collapseAction) {
        Image(systemName: "chevron.right")
          .font(.caption.weight(.semibold))
          .foregroundStyle(.secondary)
          .rotationEffect(.degrees(isCollapsed ? 0 : 90))
          .frame(width: 16, height: 16)
          .contentShape(.rect)
      }
      .buttonStyle(.plain)
      .padding(.leading, 4)
      .accessibilityLabel(isCollapsed ? "Expand \(network.name)" : "Collapse \(network.name)")

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
        .padding(.trailing, 8)
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
    if buffer.archived {
      return AnyShapeStyle(.secondary)
    }
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
  fileprivate func bufferMenu(
    _ buffer: Buffer, model: AppModel, requestDelete: @escaping (Buffer) -> Void
  ) -> some View {
    contextMenu {
      if buffer.kind == "channel" {
        Button(buffer.pinned ? "Unpin" : "Pin") {
          model.updateBuffer(buffer.id, BufferSettingsPatch(pinned: !buffer.pinned))
        }
        Divider()
        // Channel archiving follows membership: Archive parts (the server
        // sets archived on self-part), Unarchive rejoins.
        if buffer.joined {
          Button("Archive") {
            model.command(ClientCommand(type: "part", bufferID: buffer.id))
          }
        } else {
          Button("Unarchive") {
            model.command(
              ClientCommand(type: "join", networkID: buffer.networkID, channel: buffer.name))
          }
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
      if buffer.kind == "query" {
        Button(buffer.archived ? "Unarchive" : "Archive") {
          model.setArchived(buffer.id, !buffer.archived)
        }
      }
      if buffer.archived {
        Divider()
        Button("Delete…", role: .destructive) {
          requestDelete(buffer)
        }
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
