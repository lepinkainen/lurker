#if os(macOS)

import AppKit
import SwiftUI

// MARK: - Preview attachments

/// Attachment whose view is the shared SwiftUI `PreviewCard` (OpenGraph
/// card or inline image) hosted in an NSHostingView.
final class PreviewTextAttachment: NSTextAttachment {

  // MARK: Lifecycle

  init(preview: Preview, model: AppModel) {
    self.preview = preview
    self.model = model
    super.init(data: nil, ofType: nil)
  }

  @available(*, unavailable)
  required init?(coder _: NSCoder) {
    fatalError("not decodable")
  }

  // MARK: Internal

  let preview: Preview
  let model: AppModel

  override func viewProvider(
    for parentView: NSView?,
    location: NSTextLocation,
    textContainer: NSTextContainer?,
  ) -> NSTextAttachmentViewProvider? {
    let provider = PreviewAttachmentViewProvider(
      textAttachment: self,
      parentView: parentView,
      textLayoutManager: textContainer?.textLayoutManager,
      location: location,
    )
    provider.tracksTextAttachmentViewBounds = true
    return provider
  }

}

/// An inline image growing from its placeholder to the loaded bitmap is the
/// one post-insertion size change; this relay invalidates layout so TextKit
/// re-queries attachmentBounds. Guarded against invalidation loops. A
/// MainActor class so the hosted SwiftUI view can hold it across the
/// Sendable boundary into NSHostingView.
@MainActor
final class PreviewAttachmentResizeRelay {

  // MARK: Internal

  weak var layoutManager: NSTextLayoutManager?
  weak var hostView: NSView?
  var location: NSTextLocation?

  func fire() {
    guard let hostView else { return }
    let size = hostView.fittingSize
    guard size != lastSize, lastSize != .zero else {
      lastSize = size
      return
    }
    lastSize = size
    // Check pinning before invalidating: the growth would otherwise push
    // the viewport past the near-bottom threshold and kill auto-follow.
    var ancestor = hostView.superview
    while ancestor != nil, !(ancestor is TimelineNSTextView) { ancestor = ancestor?.superview }
    let coordinator = (ancestor as? TimelineNSTextView)?.coordinator
    let pinned = coordinator?.viewportPinnedToBottom ?? false
    if let location {
      layoutManager?.invalidateLayout(for: NSTextRange(location: location))
    }
    if pinned {
      coordinator?.repinToBottom()
    }
  }

  // MARK: Private

  private var lastSize = CGSize.zero

}

final class PreviewAttachmentViewProvider: NSTextAttachmentViewProvider {

  // MARK: Lifecycle

  override init(
    textAttachment: NSTextAttachment,
    parentView: NSView?,
    textLayoutManager: NSTextLayoutManager?,
    location: NSTextLocation,
  ) {
    layoutManager = textLayoutManager
    super.init(
      textAttachment: textAttachment,
      parentView: parentView,
      textLayoutManager: textLayoutManager,
      location: location,
    )
  }

  // MARK: Internal

  // NSTextAttachmentViewProvider's overrides are declared nonisolated, but
  // TextKit view hosting always calls them on the main thread — hence the
  // assumeIsolated + unsafe self smuggling (assumeIsolated traps off-main,
  // so a wrong assumption fails loudly, not racily).

  override func loadView() {
    nonisolated(unsafe) let unsafeSelf = self
    MainActor.assumeIsolated {
      guard let attachment = unsafeSelf.textAttachment as? PreviewTextAttachment else { return }
      let relay = PreviewAttachmentResizeRelay()
      relay.layoutManager = unsafeSelf.layoutManager
      relay.location = unsafeSelf.location
      let host = NSHostingView(
        rootView: PreviewAttachmentRoot(
          preview: attachment.preview,
          model: attachment.model,
          relay: relay,
        )
      )
      relay.hostView = host
      host.sizingOptions = [.intrinsicContentSize]
      unsafeSelf.view = host
    }
  }

  override func attachmentBounds(
    for _: [NSAttributedString.Key: Any],
    location _: NSTextLocation,
    textContainer _: NSTextContainer?,
    proposedLineFragment: CGRect,
    position _: CGPoint,
  ) -> CGRect {
    nonisolated(unsafe) let unsafeSelf = self
    return MainActor.assumeIsolated {
      unsafeSelf.view?.layoutSubtreeIfNeeded()
      var size = unsafeSelf.view?.fittingSize ?? .zero
      let available = proposedLineFragment.width - RowMetrics.contentLeft - RowMetrics.inset
      if available > 50 {
        size.width = min(size.width, available)
      }
      return CGRect(origin: .zero, size: size)
    }
  }

  // MARK: Private

  private weak var layoutManager: NSTextLayoutManager?

}

/// Hosted SwiftUI root for a preview attachment: the shared PreviewCard
/// plus a geometry probe that tells the relay when the content resized.
private struct PreviewAttachmentRoot: View {
  let preview: Preview
  let model: AppModel
  let relay: PreviewAttachmentResizeRelay

  var body: some View {
    PreviewCard(preview: preview)
      .environment(model)
      .onGeometryChange(for: CGSize.self, of: \.size) { _ in
        MainActor.assumeIsolated { relay.fire() }
      }
  }
}

/// NSAttributedString mirror of `attributedBody` (ConversationView.swift):
/// mIRC segment formatting plus NSDataDetector link runs. SwiftUI-scoped
/// attributes do not bridge to AppKit, so the builder is duplicated rather
/// than converted; keep the two in sync.
@MainActor
func nsMessageBody(_ message: Message, baseColor: NSColor = .labelColor) -> NSAttributedString {
  let result = NSMutableAttributedString()
  let segments =
    message.segments?.isEmpty == false ? message.segments! : [MircSegment(text: message.content)]
  let baseFont = TimelineNSFonts.message
  for segment in segments {
    var attributes: [NSAttributedString.Key: Any] = [
      .font: baseFont,
      .foregroundColor: baseColor,
    ]
    var traits: NSFontDescriptor.SymbolicTraits = []
    if segment.bold == true {
      traits.insert(.bold)
    }
    if segment.italic == true {
      traits.insert(.italic)
    }
    if
      !traits.isEmpty,
      let styled = NSFont(
        descriptor: baseFont.fontDescriptor.withSymbolicTraits(traits),
        size: baseFont.pointSize,
      )
    {
      attributes[.font] = styled
    }
    if segment.underline == true {
      attributes[.underlineStyle] = NSUnderlineStyle.single.rawValue
    }
    if segment.strike == true {
      attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue
    }
    if let foreground = segment.fg {
      attributes[.foregroundColor] = NSColor(mircColor(foreground))
    }
    result.append(NSAttributedString(string: segment.text, attributes: attributes))
  }
  // Detector ranges are UTF-16 offsets into the joined segment text, which
  // is exactly `result.string` — they apply directly.
  if let detector = TimelineFormatters.linkDetector {
    let plainText = result.string
    for match in detector.matches(
      in: plainText,
      range: NSRange(plainText.startIndex..., in: plainText),
    ) {
      guard let url = match.url else { continue }
      result.addAttributes(
        [
          .link: url,
          .foregroundColor: NSColor.linkColor,
          .underlineStyle: NSUnderlineStyle.single.rawValue,
        ],
        range: match.range,
      )
    }
  }
  return result
}

#endif
