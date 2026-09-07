import Foundation
import SwiftUI
import UniformTypeIdentifiers

#if !os(macOS)
import PhotosUI
#endif

// MARK: - ComposerPopupHeightKey

private struct ComposerPopupHeightKey: PreferenceKey {
  static let defaultValue: CGFloat = 0

  static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
    value = max(value, nextValue())
  }
}

// MARK: - ComposerView

struct ComposerView: View {

  // MARK: Internal

  let buffer: Buffer

  var body: some View {
    @Bindable var model = model
    // Resolve once per render; the key handlers resolve once per event.
    let popup = popup
    VStack(alignment: .leading, spacing: 4) {
      if let error = model.composerError {
        Text(error)
          .font(.body)
          .foregroundStyle(.red)
          .transition(.move(edge: .bottom).combined(with: .opacity))
      }
      HStack(alignment: .bottom, spacing: 8) {
        Text(model.selectedNetwork?.nick ?? "you")
          .font(Theme.Fonts.nick.weight(.semibold))
          .foregroundStyle(.white)
        //          .padding(.bottom, 6)
        TextField(placeholder, text: $model.composerText, axis: .vertical)
          .textFieldStyle(.plain)
          .font(Theme.Fonts.message)
          .lineLimit(1...5)
          .focused($focused)
          .onSubmit { model.sendComposer() }
          .disabled(!canSend)
          .onKeyPress(keys: [.tab, .return]) { press in
            guard press.modifiers.isEmpty else { return .ignored }
            return acceptSelection() ? .handled : .ignored
          }
          .onKeyPress(keys: [.upArrow, .downArrow]) { press in
            // Modified arrows (⌥↑ etc.) belong to the menu-bar shortcuts.
            // Arrow keys always arrive with implicit flags set (.function,
            // .numericPad), so "bare" means no real chord modifiers — a plain
            // isEmpty check rejects every arrow press and kills history
            // browsing entirely.
            let chordModifiers: EventModifiers = [.command, .option, .control, .shift]
            guard press.modifiers.intersection(chordModifiers).isEmpty else {
              return .ignored
            }
            return handleArrow(up: press.key == .upArrow)
          }
          .onKeyPress(.escape) {
            guard self.popup != .none else { return .ignored }
            popupDismissed = true
            return .handled
          }
        attachButton
        Button(action: { model.sendComposer() }) {
          Image(systemName: "arrow.up.circle.fill")
            .font(.title2)
        }
        .buttonStyle(.plain)
        .disabled(
          !canSend || model.composerText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        )
        .help("Send message")
      }
      .padding(.horizontal, 10)
      .padding(.vertical, 7)
      .background(Color.lurkerControlBackground, in: .rect(cornerRadius: 8))
      .onDrop(of: [.image], isTargeted: nil, perform: handleDrop)
      .overlay {
        RoundedRectangle(cornerRadius: 8)
          .stroke(
            focused ? Color.accentColor : Color.lurkerSeparator,
            lineWidth: focused ? 1.5 : 0.5,
          )
      }
      // Floated above the bar without affecting its layout. Anchored to the
      // bar's top-leading, then offset up by its own measured height so it
      // sits fully *above* the input. (An `.alignmentGuide(.top)` flip proved
      // unreliable — it rendered the panel directly over the bar, growing
      // downward off the window.)
      .overlay(alignment: .topLeading) {
        if popup != .none {
          // Clamp: the match list can shrink out from under the selection
          // (e.g. a member leaves mid-completion).
          ComposerPopupView(
            popup: popup,
            selection: min(popupSelection, max(0, popup.selectableCount - 1)),
          ) { index in
            accept(index: index, in: popup)
          }
          .background(
            GeometryReader { proxy in
              Color.clear.preference(key: ComposerPopupHeightKey.self, value: proxy.size.height)
            }
          )
          .offset(y: -(popupHeight + 4))
          // Hide the pre-measurement frame so it never flashes over the bar.
          .opacity(popupHeight > 0 ? 1 : 0)
        }
      }
      .onPreferenceChange(ComposerPopupHeightKey.self) { popupHeight = $0 }
    }
    .padding(10)
    .background(.bar)
    .onChange(of: model.focusComposerRequest) { _, _ in focused = true }
    .onChange(of: model.composerText) { _, _ in
      popupSelection = 0
      popupDismissed = false
    }
    // Clicking away from the composer dismisses the popup (web parity: the web
    // client hides it on input blur).
    .onChange(of: focused) { _, isFocused in
      if !isFocused {
        popupDismissed = true
      }
    }
    #if os(macOS)
    // Autofocus on buffer switch is desktop-only: on iOS programmatic focus
    // raises the software keyboard over the timeline the user came to read.
    .onChange(of: buffer.id) { _, _ in focused = true }
    #endif
  }

  // MARK: Private

  @Environment(AppModel.self) private var model
  @FocusState private var focused: Bool
  @State private var popupSelection = 0
  /// The popup derives from the text, so Esc-dismissal needs an explicit flag;
  /// any text change re-arms it.
  @State private var popupDismissed = false
  /// Measured height of the autocomplete panel, used to float it above the bar.
  @State private var popupHeight: CGFloat = 0
  #if os(macOS)
  @State private var importingImage = false
  #else
  @State private var photoItem: PhotosPickerItem?
  #endif

  private var popup: ComposerPopup {
    guard !popupDismissed, canSend else { return .none }
    return ComposerPopup.resolve(
      text: model.composerText,
      buffer: buffer,
      members: model.members[buffer.id] ?? [],
      ownNick: model.selectedNetwork?.nick,
    )
  }

  private var canSend: Bool {
    model.connectionState == .connected && (buffer.kind != "channel" || buffer.joined)
  }

  /// Attach affordance: a `fileImporter`-backed button on macOS, a
  /// `PhotosPicker` on iOS. Both hand raw image data to
  /// `AppModel.attachImage`, which normalizes/uploads it and appends the
  /// resulting URL to the composer text.
  @ViewBuilder
  private var attachButton: some View {
    #if os(macOS)
    Button {
      importingImage = true
    } label: {
      attachIcon
    }
    .buttonStyle(.plain)
    .disabled(!canSend || model.isUploading)
    .help("Attach image")
    .fileImporter(isPresented: $importingImage, allowedContentTypes: [.image]) { result in
      guard case .success(let url) = result else { return }
      loadFile(at: url)
    }
    #else
    PhotosPicker(selection: $photoItem, matching: .images) {
      attachIcon
    }
    .disabled(!canSend || model.isUploading)
    .onChange(of: photoItem) { _, newValue in
      guard let newValue else { return }
      Task {
        if let data = try? await newValue.loadTransferable(type: Data.self) {
          await model.attachImage(data, sourceType: nil)
        }
        photoItem = nil
      }
    }
    #endif
  }

  @ViewBuilder
  private var attachIcon: some View {
    if model.isUploading {
      ProgressView()
        #if os(macOS)
        .controlSize(.small)
        #endif
    } else {
      Image(systemName: "paperclip")
        .font(.title2)
    }
  }

  private var placeholder: String {
    if !canSend {
      return model.connectionState == .connected
        ? "This conversation is read-only"
        : "Waiting for connection…"
    }
    if buffer.kind == "status" {
      return "Commands only, e.g. /list, /nick, /msg NickServ …"
    }
    return "\(buffer.name)"
  }

  /// Tab/Enter acceptance of the highlighted nick/emoji row, clamped in case
  /// the match list shrank since the selection was made. The command popup is
  /// display-only, so those keys fall through (Enter submits).
  private func acceptSelection() -> Bool {
    let popup = popup
    let count = popup.selectableCount
    guard count > 0 else { return false }
    return accept(index: min(popupSelection, count - 1), in: popup)
  }

  @discardableResult
  private func accept(index: Int, in popup: ComposerPopup) -> Bool {
    switch popup {
    case .nick(let nicks):
      guard nicks.indices.contains(index) else { return false }
      model.composerText = NickCompletion.apply(nick: nicks[index])
      return true

    case .emoji(let matches):
      guard matches.indices.contains(index) else { return false }
      model.composerText = EmojiCompletion.apply(
        text: model.composerText,
        match: matches[index],
      )
      return true

    case .command,
         .none:
      return false
    }
  }

  /// Bare ↑/↓ cycle the popup selection when one is open, otherwise browse
  /// per-buffer input history.
  private func handleArrow(up: Bool) -> KeyPress.Result {
    let count = popup.selectableCount
    if count > 0 {
      let current = min(popupSelection, count - 1)
      popupSelection = ((current + (up ? -1 : 1)) % count + count) % count
      return .handled
    }
    return model.navigateHistory(up: up) ? .handled : .ignored
  }

  #if os(macOS)
  private func loadFile(at url: URL) {
    let accessing = url.startAccessingSecurityScopedResource()
    defer {
      if accessing {
        url.stopAccessingSecurityScopedResource()
      }
    }
    guard let data = try? Data(contentsOf: url) else { return }
    let type = UTType(filenameExtension: url.pathExtension)
    Task {
      await model.attachImage(data, sourceType: type)
    }
  }
  #endif

  /// Drop handler for image files dragged onto the composer bar.
  private func handleDrop(_ providers: [NSItemProvider]) -> Bool {
    guard canSend else { return false }
    guard
      let provider = providers.first(where: {
        $0.hasItemConformingToTypeIdentifier(UTType.image.identifier)
      })
    else {
      return false
    }
    provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, _ in
      guard let data else { return }
      Task { await model.attachImage(data, sourceType: nil) }
    }
    return true
  }

}
