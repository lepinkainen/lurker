import SwiftUI

// Deterministic per-nick avatar + color, ported from web/src/nick.ts and
// web/src/nick-palette.ts. See ai-docs/nick-identicon.md for the full spec —
// this file and nick.ts are the two conforming implementations and must stay
// in exact parity (same xorshift32 bitmap, same OKLCH tint).

/// 48-entry hue ramp, `NICK_HUES[i] = i * 7.5` (0..352.5°). Mirrors the
/// generated web/src/nick-palette.ts.
let nickHues: [Double] = (0..<48).map { Double($0) * 7.5 }

/// Resolves a server-assigned palette index to the tint used for a nick's
/// avatar and label. `nil` (index not yet known) draws neutral gray, matching
/// nickColor()/nick.ts behavior.
func nickPaletteColor(_ index: Int?) -> Color {
  guard let index else { return oklchColor(l: 0.72, c: 0, h: 0) }
  let hue = nickHues[((index % nickHues.count) + nickHues.count) % nickHues.count]
  return oklchColor(l: 0.72, c: 0.12, h: hue)
}

/// xorshift32 identicon bitmap for `nick`, mirroring nickAvatar() in
/// web/src/nick.ts exactly (same seed, same per-cell round, same LSB test).
/// Uses the raw (non-lowercased) nick and UTF-16 code units to match JS
/// `charCodeAt`.
func nickIdenticonRows(_ nick: String) -> [[Bool]] {
  var r: UInt32 = 1
  for unit in nick.utf16 {
    r = r &+ UInt32(unit)
    r ^= r &<< 13
    r ^= r >> 17
    r ^= r &<< 5
  }

  let size = 5
  let half = size / 2  // 2
  var rows = Array(repeating: Array(repeating: false, count: size), count: size)
  for y in 0..<size {
    for x in 0...half {
      r ^= r &<< 13
      r ^= r >> 17
      r ^= r &<< 5
      if r & 1 == 1 {
        rows[y][x] = true
        rows[y][size - 1 - x] = true
      }
    }
  }
  return rows
}

/// oklch(L C H) → sRGB, H in degrees. Matches the CSS Color 4 formula used by
/// the web client's `oklch()` CSS values.
func oklchColor(l: Double, c: Double, h: Double) -> Color {
  let hRad = h * .pi / 180
  let a = c * cos(hRad)
  let b = c * sin(hRad)

  let lLms = l + 0.3963377774 * a + 0.2158037573 * b
  let mLms = l - 0.1055613458 * a - 0.0638541728 * b
  let sLms = l - 0.0894841775 * a - 1.2914855480 * b

  let ll = lLms * lLms * lLms
  let mm = mLms * mLms * mLms
  let ss = sLms * sLms * sLms

  let r = 4.0767416621 * ll - 3.3077115913 * mm + 0.2309699292 * ss
  let g = -1.2684380046 * ll + 2.6097574011 * mm - 0.3413193965 * ss
  let bl = -0.0041960863 * ll - 0.7034186147 * mm + 1.7076147010 * ss

  func gamma(_ value: Double) -> Double {
    let clamped = value <= 0.0031308 ? 12.92 * value : 1.055 * pow(value, 1 / 2.4) - 0.055
    return min(max(clamped, 0), 1)
  }

  return Color(.sRGB, red: gamma(r), green: gamma(g), blue: gamma(bl), opacity: 1)
}

/// Small identicon square drawn next to a nick, deterministic from the nick
/// text with a color derived from the server-assigned palette index.
struct NickAvatar: View {
  let nick: String
  let colorIndex: Int?
  /// IRCv3 bot-mode nicks show a robot glyph instead: the identicon exists to
  /// tell humans apart, which is not what matters about a bot.
  var isBot: Bool = false
  var size: CGFloat = 14

  var body: some View {
    if isBot {
      Text("🤖")
        .font(.system(size: size * 0.86))
        .frame(width: size, height: size)
        .accessibilityLabel("bot")
    } else {
      identicon
    }
  }

  @ViewBuilder private var identicon: some View {
    let rows = nickIdenticonRows(nick)
    let color = nickPaletteColor(colorIndex)
    Canvas { context, canvasSize in
      let cell = canvasSize.width / 5
      for (y, row) in rows.enumerated() {
        for (x, on) in row.enumerated() where on {
          let rect = CGRect(x: CGFloat(x) * cell, y: CGFloat(y) * cell, width: cell, height: cell)
          context.fill(Path(rect), with: .color(color))
        }
      }
    }
    .frame(width: size, height: size)
    .clipShape(RoundedRectangle(cornerRadius: 2))
  }
}
