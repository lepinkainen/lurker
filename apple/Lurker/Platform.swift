import ImageIO
import SwiftUI
import UniformTypeIdentifiers

#if os(macOS)
import AppKit
#else
import UIKit
#endif

// Cross-platform shims so the shared SwiftUI sources compile for both the macOS
// app and the iOS app. macOS draws on AppKit (`NSColor`, `NSPasteboard`); iOS on
// UIKit (`UIColor`, `UIPasteboard`). Keep all `#if os(...)` color/clipboard
// branching confined to this file so the views stay platform-agnostic.

#if os(macOS)
extension NSColor {
  /// Timeline surface. Dark mode uses the web client's `--bg-0` (#0b0c10):
  /// the system `textBackgroundColor` (#1e1e1e) washes out the nick palette.
  /// Light mode stays on the system color.
  static let lurkerTimelineBackground = NSColor(name: "lurkerTimelineBackground") { appearance in
    appearance.isDark ? NSColor(srgbRed: 0x0b / 255, green: 0x0c / 255, blue: 0x10 / 255, alpha: 1) : .textBackgroundColor
  }

  /// Primary message text. Dark mode uses the web client's `--fg-0` (#c0caf5).
  static let lurkerTimelineText = NSColor(name: "lurkerTimelineText") { appearance in
    appearance.isDark ? NSColor(srgbRed: 0xc0 / 255, green: 0xca / 255, blue: 0xf5 / 255, alpha: 1) : .labelColor
  }
}

extension NSAppearance {
  fileprivate var isDark: Bool {
    bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
  }
}
#endif

extension Color {
  /// Background behind the message timeline (the document/content surface).
  static var lurkerTimelineBackground: Color {
    #if os(macOS)
    Color(nsColor: .lurkerTimelineBackground)
    #else
    Color(uiColor: .systemBackground)
    #endif
  }

  /// Primary message/nick text on the timeline surface.
  static var lurkerTimelineText: Color {
    #if os(macOS)
    Color(nsColor: .lurkerTimelineText)
    #else
    .primary
    #endif
  }

  /// Hairline separator matching the platform's list/table separators.
  static var lurkerSeparator: Color {
    #if os(macOS)
    Color(nsColor: .separatorColor)
    #else
    Color(uiColor: .separator)
    #endif
  }

  /// Field/control background used by the composer input.
  static var lurkerControlBackground: Color {
    #if os(macOS)
    Color(nsColor: .controlBackgroundColor)
    #else
    Color(uiColor: .secondarySystemBackground)
    #endif
  }

  /// Link tint for detected URLs in message bodies.
  static var lurkerLink: Color {
    #if os(macOS)
    Color(nsColor: .linkColor)
    #else
    Color(uiColor: .link)
    #endif
  }
}

/// Cross-platform bitmap image type: `NSImage` on macOS, `UIImage` on iOS.
/// Lets shared code (e.g. the image cache) decode/store platform images
/// without `#if os(...)` branching at every call site.
#if os(macOS)
typealias PlatformImage = NSImage
#else
typealias PlatformImage = UIImage
#endif

extension Image {
  /// Wraps a decoded `PlatformImage` for display, mirroring `Image(nsImage:)`
  /// / `Image(uiImage:)` behind the single cross-platform name.
  init(platformImage: PlatformImage) {
    #if os(macOS)
    self.init(nsImage: platformImage)
    #else
    self.init(uiImage: platformImage)
    #endif
  }
}

// MARK: - Clipboard

/// Cross-platform clipboard write and macOS image paste support.
enum Clipboard {
  #if os(macOS)
  static func hasImage(from pasteboard: NSPasteboard = .general) -> Bool {
    (pasteboard.types ?? []).contains { UTType($0.rawValue)?.conforms(to: .image) == true }
  }

  /// Prefer the original web format over alternate TIFF representations so
  /// pasted PNGs keep transparency and GIFs keep animation.
  static func image(from pasteboard: NSPasteboard = .general) -> (data: Data, type: UTType)? {
    let preferred: [UTType] = [.gif, .png, .jpeg, .tiff]
    let types = preferred + (pasteboard.types ?? []).compactMap { UTType($0.rawValue) }
    for type in types where type.conforms(to: .image) {
      if let data = pasteboard.data(forType: NSPasteboard.PasteboardType(type.identifier)) {
        return (data, type)
      }
    }
    return nil
  }
  #endif

  static func copy(_ string: String) {
    #if os(macOS)
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(string, forType: .string)
    #else
    UIPasteboard.general.string = string
    #endif
  }
}

// MARK: - ImageEncoding

/// Composer attachment support: normalizes arbitrary picked/dropped image
/// data into something the backend accepts. The backend does not decode
/// HEIC, so anything that isn't already JPEG/PNG/GIF is transcoded to JPEG.
/// GIF passes through untouched (see `normalize`) to preserve animation.
/// Uses ImageIO so the same code path runs on both macOS and iOS.
enum ImageEncoding {

  // MARK: Internal

  /// Re-encodes arbitrary image data as JPEG, or nil if it can't be decoded
  /// as an image.
  static func toJPEG(_ data: Data, quality: CGFloat = 0.82) -> Data? {
    // Decode via a thumbnail request (at full resolution, no size cap) so
    // ImageIO bakes the source's EXIF orientation into the pixels. Decoding
    // with CGImageSourceCreateImageAtIndex instead would leave the pixel
    // buffer in the sensor's native orientation, so a portrait HEIC shot on
    // an iPhone held upright would re-encode as a sideways JPEG.
    let options: [CFString: Any] = [
      kCGImageSourceCreateThumbnailFromImageAlways: true,
      kCGImageSourceCreateThumbnailWithTransform: true,
    ]
    guard
      let source = CGImageSourceCreateWithData(data as CFData, nil),
      let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    else {
      return nil
    }
    let output = NSMutableData()
    guard
      let destination = CGImageDestinationCreateWithData(
        output,
        UTType.jpeg.identifier as CFString,
        1,
        nil,
      )
    else {
      return nil
    }
    let properties = [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary
    CGImageDestinationAddImage(destination, image, properties)
    guard CGImageDestinationFinalize(destination) else { return nil }
    return output as Data
  }

  /// Prepares image data for upload: JPEG/PNG/GIF pass through unchanged
  /// (GIF specifically to preserve animation, which re-encoding would
  /// flatten to a single frame); anything else decodable (e.g. HEIC) is
  /// transcoded to JPEG; undecodable data returns nil.
  static func normalize(
    _ data: Data,
    sourceUTType: UTType? = nil,
  ) -> (data: Data, filename: String, contentType: String)? {
    let detectedType = sourceUTType ?? detectUTType(data)
    if let detectedType, detectedType.conforms(to: .jpeg) {
      return (data, "image.jpg", "image/jpeg")
    }
    if let detectedType, detectedType.conforms(to: .png) {
      return (data, "image.png", "image/png")
    }
    if let detectedType, detectedType.conforms(to: .gif) {
      // GIFs pass through untouched: the backend stores them as-is
      // specifically to preserve animation, and toJPEG below would
      // flatten the first frame only, destroying it.
      return (data, "image.gif", "image/gif")
    }
    guard let jpeg = toJPEG(data) else { return nil }
    return (jpeg, "image.jpg", "image/jpeg")
  }

  // MARK: Private

  private static func detectUTType(_ data: Data) -> UTType? {
    guard
      let source = CGImageSourceCreateWithData(data as CFData, nil),
      let typeIdentifier = CGImageSourceGetType(source)
    else {
      return nil
    }
    return UTType(typeIdentifier as String)
  }

}
