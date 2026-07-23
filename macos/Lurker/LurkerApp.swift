import SwiftUI

@main
struct LurkerApp: App {
  @State private var model: AppModel

  init() {
    if ProcessInfo.processInfo.arguments.contains("-ui-testing") {
      _model = State(initialValue: AppModel(transport: FixtureTransport()))
    } else {
      _model = State(initialValue: AppModel())
    }
  }

  var body: some Scene {
    Window("Lurker", id: "main") {
      RootView()
        .environment(model)
        .tint(.mint)
        .frame(minWidth: 760, minHeight: 520)
        .onAppear { model.start() }
    }
    .defaultSize(width: 1180, height: 760)
    .commands {
      LurkerCommands(model: model)
    }

    Settings {
      SettingsView()
        .environment(model)
        .frame(width: 500, height: 330)
    }
  }
}

private struct LurkerCommands: Commands {
  let model: AppModel

  var body: some Commands {
    SidebarCommands()
    CommandMenu("Navigate") {
      Button("Open Channel Switcher…") {
        model.showingChannelSwitcher = true
      }
      .keyboardShortcut("k")

      Button("Previous Buffer") {
        model.nextBuffer(direction: -1)
      }
      .keyboardShortcut(.upArrow, modifiers: .option)

      Button("Next Buffer") {
        model.nextBuffer()
      }
      .keyboardShortcut(.downArrow, modifiers: .option)

      Button("Previous Unread Buffer") {
        model.nextBuffer(unreadOnly: true, direction: -1)
      }
      .keyboardShortcut(.upArrow, modifiers: [.option, .shift])

      Button("Next Unread Buffer") {
        model.nextBuffer(unreadOnly: true)
      }
      .keyboardShortcut(.downArrow, modifiers: [.option, .shift])

      Button("Next Mention") {
        model.nextBuffer(mentionsOnly: true)
      }
      .keyboardShortcut("m", modifiers: .option)

      Button("Network Status") {
        model.focusStatusBuffer()
      }
      .keyboardShortcut("s", modifiers: .option)
    }
    CommandMenu("Conversation") {
      Button("Focus Message Field") {
        model.focusComposer()
      }
      .keyboardShortcut("l")

      Button(model.inspectorVisible ? "Hide Members" : "Show Members") {
        model.setInspectorVisible(!model.inspectorVisible)
      }
      .keyboardShortcut("i", modifiers: [.command, .option])
    }
  }
}
