import AppKit
import Foundation
import ImageIO
import Testing
import UniformTypeIdentifiers

@testable import Lurker

struct ImageEncodingTests {

  // MARK: Internal

  @Test
  func `gif passes through byte identical`() throws {
    let gifData = try #require(encodedImage(width: 1, height: 1, as: .gif))
    let result = try #require(ImageEncoding.normalize(gifData, sourceUTType: .gif))
    #expect(result.contentType == "image/gif")
    #expect(result.filename.hasSuffix(".gif"))
    #expect(result.data == gifData)
  }

  @Test
  func `jpeg passes through byte identical`() throws {
    let jpegData = try #require(encodedImage(width: 4, height: 4, as: .jpeg))
    let result = try #require(ImageEncoding.normalize(jpegData, sourceUTType: .jpeg))
    #expect(result.contentType == "image/jpeg")
    #expect(result.filename.hasSuffix(".jpg"))
    #expect(result.data == jpegData)
  }

  @Test
  func `png passes through byte identical`() throws {
    let pngData = try #require(encodedImage(width: 4, height: 4, as: .png))
    let result = try #require(ImageEncoding.normalize(pngData, sourceUTType: .png))
    #expect(result.contentType == "image/png")
    #expect(result.filename.hasSuffix(".png"))
    #expect(result.data == pngData)
  }

  /// A raw 4x2 (landscape) pixel buffer tagged with EXIF orientation 6
  /// (rotate 90° CW to display upright) mimics a portrait iPhone photo shot
  /// in the sensor's native landscape orientation. `toJPEG` must bake that
  /// rotation into the output pixels so the encoded JPEG is upright
  /// regardless of whether the eventual viewer honors EXIF orientation.
  @Test
  func `to JPEG bakes exif orientation into pixels`() throws {
    let sourceData = try #require(
      encodedImage(width: 4, height: 2, as: .jpeg, orientation: 6)
    )
    let sourceSize = try #require(pixelSize(of: sourceData))
    #expect(sourceSize.width == 4)
    #expect(sourceSize.height == 2)

    let outputData = try #require(ImageEncoding.toJPEG(sourceData))
    let outputSize = try #require(pixelSize(of: outputData))
    #expect(outputSize.width == 2)
    #expect(outputSize.height == 4)
  }

  @Test
  func `to JPEG returns non nil for plain image`() throws {
    let sourceData = try #require(encodedImage(width: 4, height: 4, as: .jpeg))
    let outputData = try #require(ImageEncoding.toJPEG(sourceData))
    let outputSize = try #require(pixelSize(of: outputData))
    #expect(outputSize.width == 4)
    #expect(outputSize.height == 4)
  }

  @Test
  func `clipboard with only text or no content has no image`() {
    let pasteboard = NSPasteboard.withUniqueName()
    defer { pasteboard.releaseGlobally() }
    #expect(Clipboard.image(from: pasteboard) == nil)
    #expect(!Clipboard.hasImage(from: pasteboard))
    pasteboard.setString("https://example.com/image.png", forType: .string)
    #expect(Clipboard.image(from: pasteboard) == nil)
    #expect(!Clipboard.hasImage(from: pasteboard))
  }

  @Test(arguments: [UTType.png, .jpeg, .gif, .tiff])
  func `clipboard image reaches upload normalization`(type: UTType) throws {
    let pasteboard = NSPasteboard.withUniqueName()
    defer { pasteboard.releaseGlobally() }
    let data = try #require(encodedImage(width: 4, height: 4, as: type))
    pasteboard.setData(data, forType: NSPasteboard.PasteboardType(type.identifier))
    #expect(Clipboard.hasImage(from: pasteboard))
    let image = try #require(Clipboard.image(from: pasteboard))
    #expect(image.data == data)
    #expect(image.type == type)
    let normalized = try #require(ImageEncoding.normalize(image.data, sourceUTType: image.type))
    #expect(normalized.contentType == (type == .tiff ? "image/jpeg" : type.preferredMIMEType))
    #expect(type == .tiff || normalized.data == data)
  }

  @Test
  func `clipboard prefers original gif and png to alternate representations`() throws {
    let pasteboard = NSPasteboard.withUniqueName()
    defer { pasteboard.releaseGlobally() }
    for type in [UTType.tiff, .png, .gif] {
      let data = try #require(encodedImage(width: 4, height: 4, as: type))
      pasteboard.setData(data, forType: NSPasteboard.PasteboardType(type.identifier))
      let image = try #require(Clipboard.image(from: pasteboard))
      #expect(image.type == type)
      #expect(image.data == data)
    }
  }

  @MainActor
  @Test
  func `field editor routes image paste and disables it while unavailable`() throws {
    let pasteboard = NSPasteboard.withUniqueName()
    defer { pasteboard.releaseGlobally() }
    let data = try #require(encodedImage(width: 4, height: 4, as: .png))
    pasteboard.setData(data, forType: .png)
    let editor = ComposerFieldEditor()
    editor.pasteboard = pasteboard
    editor.string = "existing draft"
    var uploads = 0
    editor.onPasteImage = { pasted, type in
      #expect(pasted == data)
      #expect(type == .png)
      uploads += 1
    }
    let paste = NSMenuItem(title: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")

    #expect(!editor.validateUserInterfaceItem(paste))
    editor.paste(nil)
    #expect(uploads == 0)

    editor.canPasteImage = true
    #expect(editor.validateUserInterfaceItem(paste))
    editor.paste(nil)
    #expect(uploads == 1)
    #expect(editor.string == "existing draft")

    editor.canPasteImage = false
    #expect(!editor.validateUserInterfaceItem(paste))
    editor.paste(nil)
    #expect(uploads == 1)
  }

  // MARK: Private

  /// A solid-color CGImage with no alpha channel (JPEG destinations reject
  /// alpha), sized `width` x `height`.
  private func solidCGImage(width: Int, height: Int) -> CGImage? {
    let colorSpace = CGColorSpaceCreateDeviceRGB()
    guard
      let context = CGContext(
        data: nil,
        width: width,
        height: height,
        bitsPerComponent: 8,
        bytesPerRow: 0,
        space: colorSpace,
        bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue,
      )
    else {
      return nil
    }
    context.setFillColor(CGColor(red: 1, green: 0, blue: 0, alpha: 1))
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    return context.makeImage()
  }

  /// Encodes a solid-color image as the given UTType, optionally tagging it
  /// with an EXIF/TIFF orientation value (without transforming pixels).
  private func encodedImage(
    width: Int,
    height: Int,
    as type: UTType,
    orientation: Int? = nil,
  ) -> Data? {
    guard let image = solidCGImage(width: width, height: height) else { return nil }
    let output = NSMutableData()
    guard
      let destination = CGImageDestinationCreateWithData(
        output,
        type.identifier as CFString,
        1,
        nil,
      )
    else {
      return nil
    }
    var properties = [CFString: Any]()
    if let orientation {
      properties[kCGImagePropertyOrientation] = orientation
    }
    CGImageDestinationAddImage(destination, image, properties as CFDictionary)
    guard CGImageDestinationFinalize(destination) else { return nil }
    return output as Data
  }

  private func pixelSize(of data: Data) -> (width: Int, height: Int)? {
    guard
      let source = CGImageSourceCreateWithData(data as CFData, nil),
      let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
      let width = properties[kCGImagePropertyPixelWidth] as? Int,
      let height = properties[kCGImagePropertyPixelHeight] as? Int
    else {
      return nil
    }
    return (width, height)
  }

}
