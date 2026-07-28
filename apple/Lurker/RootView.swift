import SwiftUI

struct RootView: View {
  @Environment(AppModel.self) private var model
  @Environment(\.scenePhase) private var scenePhase
  #if os(iOS)
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
  #endif

  var body: some View {
    @Bindable var model = model
    layout
      .sheet(isPresented: $model.showingConnectionEditor) {
        ConnectionEditor(required: model.configuredURL == nil)
          .environment(model)
      }
      .sheet(isPresented: $model.showingChannelSwitcher) {
        ChannelSwitcher()
          .environment(model)
      }
      .sheet(
        isPresented: Binding(
          get: { model.channelList != nil },
          set: { if !$0 { model.channelList = nil } }
        )
      ) {
        if let result = model.channelList {
          ChannelListView(result: result)
            .environment(model)
        }
      }
      #if os(iOS)
        .sheet(isPresented: $model.showingSettings) {
          NavigationStack {
            SettingsView()
            .environment(model)
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
              ToolbarItem(placement: .confirmationAction) {
                Button("Done") { model.showingSettings = false }
              }
            }
          }
        }
      #endif
      .onChange(of: scenePhase) { _, phase in
        model.setApplicationActive(phase == .active)
      }
  }

  // iPhone (compact width) gets a NavigationStack that pushes the conversation:
  // NavigationSplitView only auto-pushes its detail column when the sidebar is a
  // `List(selection:)`, which the custom sidebar is not.
  @ViewBuilder private var layout: some View {
    #if os(iOS)
      if horizontalSizeClass == .compact {
        CompactRootView()
      } else {
        splitView
      }
    #else
      splitView
    #endif
  }

  // macOS renders the toolbar attached to the split view itself in the unified
  // window toolbar; iOS drops toolbars attached outside a navigation container,
  // so on iPad the buttons live inside the columns' navigation bars instead.
  private var splitView: some View {
    @Bindable var model = model
    return NavigationSplitView(columnVisibility: $model.columnVisibility) {
      SidebarView()
        .navigationSplitViewColumnWidth(min: 190, ideal: 230, max: 320)
        #if os(iOS)
          .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
              ConnectionStatusButton()
            }
          }
        #endif
    } detail: {
      ConversationView()
        #if os(iOS)
          .toolbar {
            ToolbarItemGroup(placement: .topBarTrailing) {
              ChannelSwitcherButton()
              MembersToggleButton()
            }
          }
        #endif
    }
    .navigationSplitViewStyle(.balanced)
    .inspector(isPresented: model.membersPresented) {
      MembersInspector()
        .inspectorColumnWidth(min: 170, ideal: 210, max: 300)
    }
    #if os(macOS)
      .toolbar {
        ToolbarItem(placement: .navigation) {
          ConnectionStatusButton()
        }
        ToolbarItemGroup(placement: .primaryAction) {
          ChannelSwitcherButton()
          MembersToggleButton()
        }
      }
    #endif
  }
}

#if os(iOS)
  private struct CompactRootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
      @Bindable var model = model
      NavigationStack {
        SidebarView()
          .toolbar {
            ToolbarItem(placement: .topBarLeading) {
              ConnectionStatusButton()
            }
            ToolbarItem(placement: .topBarTrailing) {
              ChannelSwitcherButton()
            }
          }
          .navigationDestination(isPresented: $model.compactConversationVisible) {
            ConversationView()
              .navigationTitle(model.selectedBuffer?.name ?? "")
              .navigationBarTitleDisplayMode(.inline)
              .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                  MembersToggleButton()
                }
              }
          }
      }
      .sheet(isPresented: model.membersPresented) {
        MembersInspector()
          .environment(model)
          .presentationDetents([.medium, .large])
      }
    }
  }
#endif

extension AppModel {
  /// Presents the members inspector; writes route through `setInspectorVisible`
  /// so interactive dismissal (sheet swipe, inspector close) is persisted.
  fileprivate var membersPresented: Binding<Bool> {
    Binding(
      get: { self.inspectorVisible },
      set: { self.setInspectorVisible($0) }
    )
  }
}

private struct ChannelSwitcherButton: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    Button {
      model.showingChannelSwitcher = true
    } label: {
      Label("Switch Channel", systemImage: "magnifyingglass")
    }
    .help("Switch channel (⌘K)")
  }
}

private struct MembersToggleButton: View {
  @Environment(AppModel.self) private var model

  var body: some View {
    Button {
      model.setInspectorVisible(!model.inspectorVisible)
    } label: {
      Label("Members", systemImage: "person.2")
    }
    .help("Show or hide channel members")
    .disabled(model.selectedBuffer?.kind != "channel")
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
    // Desktop window sizing only: a fixed frame wider than the device breaks
    // the iOS preview canvas (content lays out past the screen edges).
    #if os(macOS)
      .frame(minWidth: 1180, minHeight: 760)
    #endif
}
