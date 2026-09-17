#if os(macOS)

import AppKit
import SwiftUI

// MARK: - Avatar / symbol bitmaps

/// Cached NSImage renderings for inline attachments. All images use
/// drawing-handler blocks, so dynamic colors resolve at draw time and adapt
/// to appearance changes without cache invalidation.
@MainActor
enum AvatarImages {

  // MARK: Internal

  static func identicon(nick: String, colorIndex: Int?, size: CGFloat = 14) -> NSImage {
    let key = "\(nick)#\(colorIndex ?? -1)" as NSString
    if let hit = identicons.object(forKey: key) {
      return hit
    }
    let rows = nickIdenticonRows(nick)
    let color = NSColor(nickPaletteColor(colorIndex))
    let image = NSImage(
      size: NSSize(width: size, height: size),
      flipped: true,
    ) { _ in
      NSBezierPath(
        roundedRect: NSRect(x: 0, y: 0, width: size, height: size),
        xRadius: 2,
        yRadius: 2,
      ).addClip()
      let cell = size / 5
      color.setFill()
      for (y, row) in rows.enumerated() {
        for (x, on) in row.enumerated() where on {
          NSRect(x: CGFloat(x) * cell, y: CGFloat(y) * cell, width: cell, height: cell).fill()
        }
      }
      return true
    }
    identicons.setObject(image, forKey: key)
    return image
  }

  /// Rounded-rect scaledToFill crop of a fetched avatar, mirroring
  /// NickAvatar's clipShape.
  static func rounded(_ source: NSImage, cacheKey: String, size: CGFloat = 14) -> NSImage {
    let key = cacheKey as NSString
    if let hit = avatars.object(forKey: key) {
      return hit
    }
    let image = NSImage(
      size: NSSize(width: size, height: size),
      flipped: false,
    ) { rect in
      NSBezierPath(roundedRect: rect, xRadius: 2, yRadius: 2).addClip()
      let sourceSize = source.size
      guard sourceSize.width > 0, sourceSize.height > 0 else { return true }
      let scale = max(size / sourceSize.width, size / sourceSize.height)
      let drawSize = NSSize(width: sourceSize.width * scale, height: sourceSize.height * scale)
      let origin = NSPoint(x: (size - drawSize.width) / 2, y: (size - drawSize.height) / 2)
      source.draw(
        in: NSRect(origin: origin, size: drawSize),
        from: .zero,
        operation: .sourceOver,
        fraction: 1,
      )
      return true
    }
    avatars.setObject(image, forKey: key)
    return image
  }

  /// SF Symbol tinted for attributed-string use (attachment images don't
  /// pick up .foregroundColor on macOS).
  static func symbol(_ name: String, pointSize: CGFloat, color: NSColor) -> NSImage {
    let key = "\(name)#\(pointSize)" as NSString
    if let hit = symbols.object(forKey: key) {
      return hit
    }
    let image = NSImage(
      size: NSSize(width: pointSize, height: pointSize),
      flipped: false,
    ) { rect in
      guard
        let base = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
          .withSymbolConfiguration(.init(pointSize: pointSize, weight: .regular))
      else { return true }
      base.draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1)
      color.set()
      rect.fill(using: .sourceAtop)
      return true
    }
    symbols.setObject(image, forKey: key)
    return image
  }

  // MARK: Private

  private static let identicons = NSCache<NSString, NSImage>()
  private static let avatars = NSCache<NSString, NSImage>()
  private static let symbols = NSCache<NSString, NSImage>()

}

#endif
