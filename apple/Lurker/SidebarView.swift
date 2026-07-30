import SwiftUI
import UniformTypeIdentifiers

/// Transient sidebar drag state. Network drags and channel drags are distinct
/// species — a channel can never drop onto a network header and vice versa.
struct SidebarDrag: Equatable {
  enum Kind: Equatable {
    case network(UUID)
    case channel(networkID: UUID, bufferID: UUID)
    case pinned(UUID)

    /// The dragged row's own id, regardless of species — used for the
    /// `NSItemProvider` payload and as the drop delegate's `draggedID`.
    var id: UUID {
      switch self {
      case .network(let id): id
      case .channel(_, let bufferID): bufferID
      case .pinned(let id): id
      }
    }
  }

  /// Rows insert-before; the end targets make the last position reachable.
  enum Target: Equatable {
    case row(UUID)
    case pinnedRow(UUID)
    case endOfNetworks
    case endOfChannels(UUID)
    case endOfPinned
  }

  var kind: Kind
  var over: Target?
}

/// A buffer can intentionally appear more than once in the sidebar (for
/// example, pinned channels also remain under their network). Lazy stacks
/// require each rendered occurrence to have a distinct identity, so namespace
/// the stable buffer id by where the row is rendered.
struct SidebarBufferOccurrence: Identifiable {
  enum Placement: Hashable {
    case pinned
    case network(UUID)
  }

  struct ID: Hashable {
    let placement: Placement
    let bufferID: UUID
  }

  let buffer: Buffer
  let placement: Placement

  var id: ID {
    ID(placement: placement, bufferID: buffer.id)
  }
}

/// Pure ordering transformation shared by all sidebar drag species.
///
/// Row targets use insert-before semantics. A nil target represents the end
/// drop zone. Keeping this independent of SwiftUI makes the directional
/// index adjustment explicit and unit-testable.
enum SidebarOrdering {
  static func moving(_ ids: [UUID], from fromID: UUID, before targetID: UUID?) -> [UUID]? {
    guard let fromIndex = ids.firstIndex(of: fromID) else { return nil }
    if let targetID {
      guard targetID != fromID, ids.contains(targetID) else { return nil }
    }

    var reordered = ids
    reordered.remove(at: fromIndex)
    if let targetID, let targetIndex = reordered.firstIndex(of: targetID) {
      reordered.insert(fromID, at: targetIndex)
    } else {
      reordered.append(fromID)
    }
    return reordered
  }
}

struct SidebarView: View {
  @Environment(AppModel.self) private var model
  @State private var pendingDelete: Buffer?
  @State private var drag: SidebarDrag?
  @State private var dragCleanupTask: Task<Void, Never>?

  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 0) {
        sidebarContent
      }
      .padding(.vertical, 6)
    }
    .navigationTitle("Lurker")
    .onChange(of: drag != nil) { _, active in
      // SwiftUI's onDrag has no cancellation callback: an Esc-cancelled or
      // dropped-nowhere drag would leave the lifted row ghosted forever.
      // While a drag is active, watch for the mouse button being released
      // without a performDrop having cleared the state.
      dragCleanupTask?.cancel()
      dragCleanupTask = nil
      guard active else { return }
      #if os(macOS)
        dragCleanupTask = Task { @MainActor in
          var idleTicks = 0
          while !Task.isCancelled, drag != nil {
            try? await Task.sleep(for: .milliseconds(250))
            if NSEvent.pressedMouseButtons == 0 {
              // Two consecutive idle ticks so a legitimate drop's
              // performDrop always wins the race.
              idleTicks += 1
              if idleTicks >= 2 {
                drag = nil
                break
              }
            } else {
              idleTicks = 0
            }
          }
        }
      #endif
    }
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
      ForEach(
        model.pinnedBuffers.map {
          SidebarBufferOccurrence(buffer: $0, placement: .pinned)
        }
      ) { occurrence in
        let buffer = occurrence.buffer
        SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
          model.selectBuffer(buffer.id)
        } content: {
          BufferRow(buffer: buffer, network: model.networks[buffer.networkID])
        }
        .bufferMenu(buffer, model: model) { pendingDelete = $0 }
        // A pinned channel also renders under its network, so pinned rows use
        // dedicated drag kind/targets to keep the two lists independent.
        .sidebarDraggable(
          kind: .pinned(buffer.id),
          target: .pinnedRow(buffer.id),
          accepts: isPinnedDrag,
          drag: $drag,
          commit: { fromID in commitPinnedDrop(from: fromID, toRow: buffer.id) }
        )
      }
      if isPinnedDragActive {
        EndDropZone(isActive: drag?.over == .endOfPinned)
          .onDrop(
            of: [.text],
            delegate: SidebarDropDelegate(
              target: .endOfPinned,
              accepts: isPinnedDrag,
              drag: $drag,
              commit: { fromID in commitPinnedDrop(from: fromID, toRow: nil) }
            ))
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
      // Drag reordering is desktop-only: on iOS onDrag's long-press would
      // fight the rows' context menus.
      .sidebarDraggable(
        kind: .network(network.id),
        target: .row(network.id),
        accepts: isNetworkDrag,
        drag: $drag,
        commit: { fromID in commitNetworkDrop(from: fromID, toRow: network.id) }
      )
      if !isCollapsed {
        // Status buffer is represented by the network header row above, so the
        // per-network rows list only channels and queries (no duplicate "Status").
        // Channels are draggable for manual reordering; queries stay alphabetical.
        ForEach(
          groups.channels.map {
            SidebarBufferOccurrence(buffer: $0, placement: .network(network.id))
          }
        ) { occurrence in
          let buffer = occurrence.buffer
          SidebarRow(isSelected: model.selectedBufferID == buffer.id) {
            model.selectBuffer(buffer.id)
          } content: {
            BufferRow(buffer: buffer, network: network)
              .padding(.leading, 14)
          }
          .bufferMenu(buffer, model: model) { pendingDelete = $0 }
          .sidebarDraggable(
            kind: .channel(networkID: network.id, bufferID: buffer.id),
            target: .row(buffer.id),
            indicatorInset: 14,
            accepts: { isChannelDrag($0, of: network.id) },
            drag: $drag,
            commit: { fromID in
              commitChannelDrop(networkID: network.id, from: fromID, toRow: buffer.id)
            }
          )
        }
        if isChannelDragActive(of: network.id) {
          EndDropZone(
            isActive: drag?.over == .endOfChannels(network.id),
            indent: 14
          )
          .onDrop(
            of: [.text],
            delegate: SidebarDropDelegate(
              target: .endOfChannels(network.id),
              accepts: { isChannelDrag($0, of: network.id) },
              drag: $drag,
              commit: { fromID in
                commitChannelDrop(networkID: network.id, from: fromID, toRow: nil)
              }
            ))
        }
        ForEach(
          groups.queries.map {
            SidebarBufferOccurrence(buffer: $0, placement: .network(network.id))
          }
        ) { occurrence in
          let buffer = occurrence.buffer
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
            ForEach(
              groups.archived.map {
                SidebarBufferOccurrence(buffer: $0, placement: .network(network.id))
              }
            ) { occurrence in
              let buffer = occurrence.buffer
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

    if isNetworkDragActive {
      EndDropZone(isActive: drag?.over == .endOfNetworks)
        .onDrop(
          of: [.text],
          delegate: SidebarDropDelegate(
            target: .endOfNetworks,
            accepts: isNetworkDrag,
            drag: $drag,
            commit: { fromID in commitNetworkDrop(from: fromID, toRow: nil) }
          ))
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

  private func isNetworkDrag(_ kind: SidebarDrag.Kind) -> Bool {
    if case .network = kind { return true }
    return false
  }

  private var isNetworkDragActive: Bool {
    guard let kind = drag?.kind else { return false }
    return isNetworkDrag(kind)
  }

  private func isPinnedDrag(_ kind: SidebarDrag.Kind) -> Bool {
    if case .pinned = kind { return true }
    return false
  }

  private var isPinnedDragActive: Bool {
    guard let kind = drag?.kind else { return false }
    return isPinnedDrag(kind)
  }

  private func isChannelDrag(_ kind: SidebarDrag.Kind, of networkID: UUID) -> Bool {
    if case .channel(let dragNetworkID, _) = kind { return dragNetworkID == networkID }
    return false
  }

  private func isChannelDragActive(of networkID: UUID) -> Bool {
    guard let kind = drag?.kind else { return false }
    return isChannelDrag(kind, of: networkID)
  }

  /// Insert the dragged network before `toRow`, or append when nil (end
  /// zone). Enabled networks only; the model appends disabled networks for
  /// the API's complete set.
  private func commitNetworkDrop(from fromID: UUID, toRow targetID: UUID?) {
    let ids = model.orderedNetworks.filter { !$0.disabled }.map(\.id)
    guard let reordered = SidebarOrdering.moving(ids, from: fromID, before: targetID) else {
      return
    }
    model.reorderNetworks(reordered)
  }

  /// Insert the dragged channel before `toRow`, or append when nil, within
  /// its network's visible channels group.
  private func commitChannelDrop(networkID: UUID, from fromID: UUID, toRow targetID: UUID?) {
    let ids = model.sidebarBuffers(for: networkID).channels.map(\.id)
    guard let reordered = SidebarOrdering.moving(ids, from: fromID, before: targetID) else {
      return
    }
    model.reorderChannels(networkID: networkID, orderedIDs: reordered)
  }

  /// Insert the dragged pinned buffer before `toRow`, or append when nil,
  /// within the pinned section.
  private func commitPinnedDrop(from fromID: UUID, toRow targetID: UUID?) {
    let ids = model.pinnedBuffers.map(\.id)
    guard let reordered = SidebarOrdering.moving(ids, from: fromID, before: targetID) else {
      return
    }
    model.reorderPinnedBuffers(reordered)
  }
}

extension View {
  /// The shared macOS sidebar row drag/drop cluster: dim the row while it's
  /// the one being dragged, show a drop indicator when a matching drag hovers
  /// this row's target, and wire up `onDrag`/`onDrop`. No-ops on non-macOS
  /// platforms — drag reordering is desktop-only there. `SidebarDropDelegate`
  /// already refuses to set `over` to a row's own target (`isSelfTarget`), so
  /// the indicator only needs to check `over == target` and `accepts`.
  fileprivate func sidebarDraggable(
    kind: SidebarDrag.Kind,
    target: SidebarDrag.Target,
    indicatorInset: CGFloat = 0,
    accepts: @escaping (SidebarDrag.Kind) -> Bool,
    drag: Binding<SidebarDrag?>,
    commit: @escaping (UUID) -> Void
  ) -> some View {
    #if os(macOS)
      self
        .opacity(drag.wrappedValue?.kind == kind ? 0.5 : 1)
        .overlay(alignment: .top) {
          if let dragKind = drag.wrappedValue?.kind, drag.wrappedValue?.over == target,
            accepts(dragKind)
          {
            DropIndicator()
              .padding(.leading, indicatorInset)
          }
        }
        .onDrag {
          drag.wrappedValue = SidebarDrag(kind: kind)
          return NSItemProvider(object: kind.id.uuidString as NSString)
        }
        .onDrop(
          of: [.text],
          delegate: SidebarDropDelegate(
            target: target,
            accepts: accepts,
            drag: drag,
            commit: commit
          ))
    #else
      self
    #endif
  }
}

/// Accent insertion line shown at the top edge of a drop target (web parity:
/// the web sidebar draws a 2px accent bar above the hovered section).
private struct DropIndicator: View {
  var body: some View {
    Rectangle()
      .fill(Color.accentColor)
      .frame(height: 2)
      .padding(.horizontal, 4)
  }
}

/// Shared drop delegate for sidebar reordering. `accepts` filters by drag
/// species (network vs channel-of-this-network); `commit` receives the
/// dragged id once the drop lands on `target`.
struct SidebarDropDelegate: DropDelegate {
  let target: SidebarDrag.Target
  let accepts: (SidebarDrag.Kind) -> Bool
  @Binding var drag: SidebarDrag?
  let commit: (UUID) -> Void

  private var draggedID: UUID? {
    drag?.kind.id
  }

  private var isSelfTarget: Bool {
    switch target {
    case .row(let id), .pinnedRow(let id): id == draggedID
    default: false
    }
  }

  func validateDrop(info _: DropInfo) -> Bool {
    guard let kind = drag?.kind else { return false }
    return accepts(kind)
  }

  func dropEntered(info _: DropInfo) {
    guard let kind = drag?.kind, accepts(kind), !isSelfTarget else { return }
    drag?.over = target
  }

  func dropExited(info _: DropInfo) {
    if drag?.over == target {
      drag?.over = nil
    }
  }

  func dropUpdated(info _: DropInfo) -> DropProposal? {
    DropProposal(operation: .move)
  }

  func performDrop(info _: DropInfo) -> Bool {
    performDrop()
  }

  /// Testable core of `performDrop(info:)`. `DropInfo` is intentionally
  /// unused because sidebar drops rely on the app-owned `drag` state.
  func performDrop() -> Bool {
    defer { drag = nil }
    guard let kind = drag?.kind, accepts(kind), let fromID = draggedID, !isSelfTarget
    else {
      return false
    }
    commit(fromID)
    return true
  }
}

/// A drop target after the last row of a group, visible only while a matching
/// drag is active — without it the final position would be unreachable.
private struct EndDropZone: View {
  let isActive: Bool
  var indent: CGFloat = 0

  var body: some View {
    ZStack(alignment: .top) {
      Color.clear
      if isActive {
        DropIndicator()
          .padding(.leading, indent)
      }
    }
    .frame(height: 12)
    .contentShape(.rect)
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

#if DEBUG
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
#endif
