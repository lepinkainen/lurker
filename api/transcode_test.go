package api

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func buildJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func buildPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x * 5), G: uint8(y * 5), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// buildAnimatedGIF creates a minimal 2-frame animated GIF so tests can
// confirm optimizeImage passes GIFs through unchanged rather than
// flattening them to a single frame.
func buildAnimatedGIF(t *testing.T) []byte {
	t.Helper()
	const w, h = 8, 8
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}

	frame1 := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	frame2 := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := range h {
		for x := range w {
			frame1.SetColorIndex(x, y, 0)
			frame2.SetColorIndex(x, y, 1)
		}
	}

	g := &gif.GIF{
		Image: []*image.Paletted{frame1, frame2},
		Delay: []int{10, 10},
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// buildOversizedImage constructs an NRGBA image whose pixel count exceeds
// maxPixels, to exercise the decompression-bomb guard. It intentionally
// picks dimensions well above the cap while staying decodable in test time
// and memory (grayscale-ish flat fill compresses fast).
func buildOversizedImage(t *testing.T) []byte {
	t.Helper()
	const w, h = 8000, 7000 // 56,000,000 px > maxPixels (50,000,000)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Leave pixels zeroed (fully transparent black) — PNG compresses this
	// extremely well and DecodeConfig only needs the header, so encoding
	// cost stays reasonable despite the large nominal dimensions.
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode oversized png: %v", err)
	}
	return buf.Bytes()
}

func TestOptimizeImage(t *testing.T) {
	t.Run("jpeg above cap is downscaled to long edge 2048", func(t *testing.T) {
		input := buildJPEG(t, 4000, 3000)
		result, err := optimizeImage(input)
		if err != nil {
			t.Fatalf("optimizeImage: %v", err)
		}
		if result.Ext != ".jpg" || result.Mime != "image/jpeg" {
			t.Fatalf("ext/mime = %q/%q, want .jpg/image/jpeg", result.Ext, result.Mime)
		}
		longEdge := max(result.Height, result.Width)
		if longEdge != 2048 {
			t.Fatalf("long edge = %d, want 2048", longEdge)
		}
		if len(result.Data) >= len(input) {
			t.Fatalf("output bytes (%d) not smaller than input (%d)", len(result.Data), len(input))
		}
	})

	t.Run("jpeg below cap is not upscaled", func(t *testing.T) {
		input := buildJPEG(t, 800, 600)
		result, err := optimizeImage(input)
		if err != nil {
			t.Fatalf("optimizeImage: %v", err)
		}
		if result.Ext != ".jpg" || result.Mime != "image/jpeg" {
			t.Fatalf("ext/mime = %q/%q, want .jpg/image/jpeg", result.Ext, result.Mime)
		}
		if result.Width != 800 || result.Height != 600 {
			t.Fatalf("dims = %dx%d, want unchanged 800x600", result.Width, result.Height)
		}
		if len(result.Data) == 0 {
			t.Fatal("empty output")
		}
	})

	t.Run("png passes through byte-identical", func(t *testing.T) {
		input := buildPNG(t, 100, 80)
		result, err := optimizeImage(input)
		if err != nil {
			t.Fatalf("optimizeImage: %v", err)
		}
		if result.Ext != ".png" || result.Mime != "image/png" {
			t.Fatalf("ext/mime = %q/%q, want .png/image/png", result.Ext, result.Mime)
		}
		if result.Width != 100 || result.Height != 80 {
			t.Fatalf("dims = %dx%d, want 100x80", result.Width, result.Height)
		}
		if !bytes.Equal(result.Data, input) {
			t.Fatal("png output not byte-identical to input")
		}
	})

	t.Run("animated gif passes through byte-identical, preserving frames", func(t *testing.T) {
		input := buildAnimatedGIF(t)
		result, err := optimizeImage(input)
		if err != nil {
			t.Fatalf("optimizeImage: %v", err)
		}
		if result.Ext != ".gif" || result.Mime != "image/gif" {
			t.Fatalf("ext/mime = %q/%q, want .gif/image/gif", result.Ext, result.Mime)
		}
		if !bytes.Equal(result.Data, input) {
			t.Fatal("gif output not byte-identical to input")
		}
		decoded, err := gif.DecodeAll(bytes.NewReader(result.Data))
		if err != nil {
			t.Fatalf("decode output gif: %v", err)
		}
		if len(decoded.Image) != 2 {
			t.Fatalf("frame count = %d, want 2 (animation preserved)", len(decoded.Image))
		}
	})

	t.Run("oversized image is rejected by the decompression-bomb guard", func(t *testing.T) {
		input := buildOversizedImage(t)
		_, err := optimizeImage(input)
		if err == nil {
			t.Fatal("expected error for oversized image, got nil")
		}
	})

	t.Run("non-image bytes are rejected", func(t *testing.T) {
		_, err := optimizeImage([]byte("not an image"))
		if err == nil {
			t.Fatal("expected error for non-image bytes, got nil")
		}
	})
}

func TestClassifyMedia(t *testing.T) {
	cases := []struct {
		sniffed string
		want    mediaKind
	}{
		{"image/jpeg", mediaKindImage},
		{"image/png", mediaKindImage},
		{"image/gif", mediaKindImage},
		{"image/webp", mediaKindImage},
		{"video/mp4", mediaKindVideo},
		{"video/quicktime", mediaKindVideo},
		{"application/pdf", mediaKindUnknown},
		{"text/plain; charset=utf-8", mediaKindUnknown},
	}
	for _, c := range cases {
		if got := classifyMedia(c.sniffed); got != c.want {
			t.Errorf("classifyMedia(%q) = %v, want %v", c.sniffed, got, c.want)
		}
	}
}
