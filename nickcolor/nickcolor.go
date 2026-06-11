// Package nickcolor computes deterministic per-nick colors shared by all
// clients. The server ships the palette index alongside every nick-bearing
// payload (messages, members, networks); clients map the index onto their
// medium's palette (web: oklch CSS hues, TUI: hex via Hex). The hash matches
// the web client's historical djb2 implementation so existing nick colors
// are stable across the migration.
package nickcolor

import (
	"math"
	"strings"
	"unicode/utf16"
)

// Count is the palette size: perceptually-spaced OKLCH hues at equal angular
// steps (360/Count degrees apart). Mirrored by web/src/nick-palette.ts.
const Count = 48

// Lightness/chroma for terminal rendering, matching the web defaults
// (--nick-l: 72%, --nick-c: 0.12) tuned for dark backgrounds.
const (
	paletteL = 0.72
	paletteC = 0.12
)

// Index hashes a nick to a palette index. djb2 over the UTF-16 code units of
// the lowercased nick, truncated to 32-bit signed — bit-for-bit the JS
// `((h << 5) + h + c) | 0` the web client used, so colors survive the move
// server-side.
func Index(nick string) int {
	var h int32 = 5381
	for _, u := range utf16.Encode([]rune(strings.ToLower(nick))) {
		h = h<<5 + h + int32(u)
	}
	// JS Math.abs on the float result; abs in 64-bit to dodge MinInt32.
	abs := int64(h)
	if abs < 0 {
		abs = -abs
	}
	return int(abs % Count)
}

// Hue returns the OKLCH hue in degrees for a palette index.
func Hue(idx int) float64 {
	return float64(idx) * (360.0 / Count)
}

// Hex returns the sRGB hex color ("#RRGGBB") for a palette index, converted
// from oklch(72% 0.12 hue). For clients that can't express OKLCH directly.
func Hex(idx int) string {
	return hexPalette[((idx%Count)+Count)%Count]
}

var hexPalette = func() [Count]string {
	var out [Count]string
	for i := range out {
		out[i] = oklchToHex(paletteL, paletteC, Hue(i))
	}
	return out
}()

// oklchToHex converts an OKLCH color to gamma-encoded sRGB hex, clamping
// out-of-gamut channels.
func oklchToHex(l, c, hDeg float64) string {
	hRad := hDeg * math.Pi / 180
	a := c * math.Cos(hRad)
	b := c * math.Sin(hRad)

	// OKLab -> LMS (cube roots), then cube.
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	lc, mc, sc := l_*l_*l_, m_*m_*m_, s_*s_*s_

	// LMS -> linear sRGB.
	r := 4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc
	g := -1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc
	bl := -0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc

	return "#" + hexByte(srgbEncode(r)) + hexByte(srgbEncode(g)) + hexByte(srgbEncode(bl))
}

func srgbEncode(v float64) int {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return int(math.Round(v * 255))
}

const hexDigits = "0123456789abcdef"

func hexByte(v int) string {
	return string([]byte{hexDigits[v>>4&0xf], hexDigits[v&0xf]})
}
