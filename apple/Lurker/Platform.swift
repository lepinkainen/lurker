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

extension Color {
  /// Background behind the message timeline (the document/content surface).
  static var lurkerTimelineBackground: Color {
    #if os(macOS)
      Color(nsColor: .textBackgroundColor)
    #else
      Color(uiColor: .systemBackground)
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

/// Cross-platform clipboard write.
enum Clipboard {
  static func copy(_ string: String) {
    #if os(macOS)
      NSPasteboard.general.clearContents()
      NSPasteboard.general.setString(string, forType: .string)
    #else
      UIPasteboard.general.string = string
    #endif
  }
}

/// Composer attachment support: normalizes arbitrary picked/dropped image
/// data into something the backend accepts. The backend does not decode
/// HEIC, so anything that isn't already JPEG/PNG/GIF is transcoded to JPEG.
/// GIF passes through untouched (see `normalize`) to preserve animation.
/// Uses ImageIO so the same code path runs on both macOS and iOS.
enum ImageEncoding {
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
    guard let source = CGImageSourceCreateWithData(data as CFData, nil),
      let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    else {
      return nil
    }
    let output = NSMutableData()
    guard
      let destination = CGImageDestinationCreateWithData(
        output, UTType.jpeg.identifier as CFString, 1, nil)
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
    _ data: Data, sourceUTType: UTType? = nil
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

  private static func detectUTType(_ data: Data) -> UTType? {
    guard let source = CGImageSourceCreateWithData(data as CFData, nil),
      let typeIdentifier = CGImageSourceGetType(source)
    else {
      return nil
    }
    return UTType(typeIdentifier as String)
  }
}
