#if os(macOS)
import AppKit
import SwiftUI
import UniformTypeIdentifiers

/// A native field editor is needed because SwiftUI's TextField consumes Paste
/// before `onPasteCommand` can handle image content.
struct MacComposerTextField: NSViewRepresentable {
  final class Coordinator: NSObject, NSTextFieldDelegate {

    // MARK: Lifecycle

    init(_ parent: MacComposerTextField) {
      self.parent = parent
    }

    // MARK: Internal

    var parent: MacComposerTextField

    func controlTextDidChange(_ notification: Notification) {
      guard let field = notification.object as? NSTextField else { return }
      parent.text = field.stringValue
      field.invalidateIntrinsicContentSize()
    }

  }

  @Binding var text: String

  var focused: Binding<Bool>
  var placeholder: String
  var isEnabled: Bool
  var canPasteImage: Bool
  var onPasteImage: (Data, UTType) -> Void
  var onKey: (KeyEquivalent) -> Bool

  func makeCoordinator() -> Coordinator {
    Coordinator(self)
  }

  func makeNSView(context: Context) -> NSTextField {
    let field = NSTextField()
    field.cell = ComposerTextFieldCell(textCell: "")
    field.isEditable = true
    field.isSelectable = true
    field.isBordered = false
    field.drawsBackground = false
    field.focusRingType = .none
    field.font = .monospacedSystemFont(ofSize: NSFont.preferredFont(forTextStyle: .body).pointSize, weight: .regular)
    field.maximumNumberOfLines = 5
    field.lineBreakMode = .byWordWrapping
    field.cell?.wraps = true
    field.cell?.isScrollable = false
    field.delegate = context.coordinator
    field.setContentHuggingPriority(.defaultLow, for: .horizontal)
    return field
  }

  func updateNSView(_ field: NSTextField, context: Context) {
    context.coordinator.parent = self
    field.isEnabled = isEnabled
    field.placeholderString = placeholder
    if field.stringValue != text {
      field.stringValue = text
      if let editor = field.currentEditor() as? NSTextView {
        editor.string = text
        editor.setSelectedRange(NSRange(location: text.utf16.count, length: 0))
      }
      field.invalidateIntrinsicContentSize()
    }
    let editor = (field.cell as! ComposerTextFieldCell).editor
    editor.canPasteImage = canPasteImage
    editor.onPasteImage = onPasteImage
    editor.onKey = onKey
    // Defer focus changes until the field is attached to its window, outside
    // SwiftUI's update pass. Read the latest parent to avoid stale requests.
    let coordinator = context.coordinator
    editor.onFocusChange = { coordinator.parent.focused.wrappedValue = $0 }
    DispatchQueue.main.async { [weak field] in
      guard let field, let window = field.window else { return }
      if coordinator.parent.focused.wrappedValue, field.isEnabled {
        if field.currentEditor() == nil {
          window.makeFirstResponder(field)
        }
      } else if field.currentEditor() != nil {
        window.makeFirstResponder(nil)
      }
    }
  }

  func sizeThatFits(_ proposal: ProposedViewSize, nsView: NSTextField, context _: Context) -> CGSize? {
    guard let width = proposal.width, width > 0 else { return nil }
    let size = nsView.cell!.cellSize(forBounds: NSRect(x: 0, y: 0, width: width, height: .greatestFiniteMagnitude))
    return CGSize(width: width, height: size.height)
  }
}

private final class ComposerTextFieldCell: NSTextFieldCell {
  let editor = ComposerFieldEditor()

  override func fieldEditor(for _: NSView) -> NSTextView? {
    editor.isFieldEditor = true
    editor.isRichText = false
    editor.importsGraphics = false
    editor.allowsUndo = true
    return editor
  }
}

final class ComposerFieldEditor: NSTextView {
  var pasteboard = NSPasteboard.general
  var canPasteImage = false
  var onPasteImage: ((Data, UTType) -> Void)?
  var onKey: ((KeyEquivalent) -> Bool)?
  var onFocusChange: ((Bool) -> Void)?

  override func becomeFirstResponder() -> Bool {
    let accepted = super.becomeFirstResponder()
    if accepted {
      onFocusChange?(true)
    }
    return accepted
  }

  override func resignFirstResponder() -> Bool {
    let accepted = super.resignFirstResponder()
    if accepted {
      onFocusChange?(false)
    }
    return accepted
  }

  override func paste(_ sender: Any?) {
    if let image = Clipboard.image(from: pasteboard) {
      if canPasteImage {
        onPasteImage?(image.data, image.type)
      }
      return
    }
    super.paste(sender)
  }

  override func validateUserInterfaceItem(_ item: any NSValidatedUserInterfaceItem) -> Bool {
    if item.action == #selector(paste(_:)), Clipboard.hasImage(from: pasteboard) {
      return canPasteImage
    }
    return super.validateUserInterfaceItem(item)
  }

  override func keyDown(with event: NSEvent) {
    let modifiers = event.modifierFlags.intersection([.command, .option, .control, .shift])
    if
      modifiers.isEmpty, !hasMarkedText(), let character = event.characters?.first,
      onKey?(KeyEquivalent(character)) == true
    {
      return
    }
    super.keyDown(with: event)
  }
}
#endif
