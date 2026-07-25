import SwiftUI

struct RootView: View {
  @Environment(AppModel.self) private var model
  @Environment(\.scenePhase) private var scenePhase

  var body: some View {
    @Bindable var model = model
    NavigationSplitView(columnVisibility: $model.columnVisibility) {
      SidebarView()
        .navigationSplitViewColumnWidth(min: 190, ideal: 230, max: 320)
    } detail: {
      ConversationView()
    }
    .navigationSplitViewStyle(.balanced)
    .inspector(isPresented: $model.inspectorVisible) {
      MembersInspector()
        .inspectorColumnWidth(min: 170, ideal: 210, max: 300)
    }
    .toolbar {
      ToolbarItem(placement: .navigation) {
        ConnectionStatusButton()
      }
      ToolbarItemGroup(placement: .primaryAction) {
        Button {
          model.showingChannelSwitcher = true
        } label: {
          Label("Switch Channel", systemImage: "magnifyingglass")
        }
        .help("Switch channel (⌘K)")

        Button {
          model.setInspectorVisible(!model.inspectorVisible)
        } label: {
          Label("Members", systemImage: "person.2")
        }
        .help("Show or hide channel members")
        .disabled(model.selectedBuffer?.kind != "channel")
      }
    }
    .sheet(isPresented: $model.showingConnectionEditor) {
      ConnectionEditor(required: model.configuredURL == nil)
        .environment(model)
    }
    .sheet(isPresented: $model.showingChannelSwitcher) {
      ChannelSwitcher()
        .environment(model)
    }
    .onChange(of: scenePhase) { _, phase in
      model.setApplicationActive(phase == .active)
    }
  }
}

private struct ConnectionStatusButton: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    Button {
      model.showingConnectionEditor = true
    } label: {
      Label(model.connectionState.label, systemImage: model.connectionState.symbol)
        .symbolEffect(.rotate, isActive: isAnimating)
    }
    .help(connectionHelp)
  }

  private var isAnimating: Bool {
    switch model.connectionState {
    case .connecting, .reconnecting: true
    default: false
    }
  }

  private var connectionHelp: String {
    if case .offline(let error) = model.connectionState {
      return error
    }
    return model.serviceIdentity.map { "Lurker \($0.version) • \($0.hash)" }
      ?? model.connectionState.label
  }
}

#Preview {
  RootView()
    .environment(AppModel.preview())
    .tint(.mint)
    .frame(minWidth: 1180, minHeight: 760)
}
