package api

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif" // register GIF decoder
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WebP decoder (decode-only)
)

// maxPixels caps the decoded image area to guard against decompression
// bombs: a small file that decodes into an enormous in-memory bitmap.
const maxPixels = 50 * 1000 * 1000 // 50 MP

// maxLongEdge is the longest allowed dimension (in pixels) after
// optimization for formats we re-encode. Images already smaller are never
// upscaled.
const maxLongEdge = 2048

// jpegQuality is the quality used when re-encoding jpeg/webp uploads.
const jpegQuality = 82

var errImageTooLarge = errors.New("image dimensions exceed maximum allowed pixels")
var errUnsupportedImageFormat = errors.New("unsupported image format")

// optimizedImage is the result of optimizeImage: the bytes to store, the
// file extension (with leading dot), the MIME type, and the resulting
// width/height. Bundled into a struct rather than returned positionally so
// the function stays under gocritic's result-count limit.
type optimizedImage struct {
	Data   []byte
	Ext    string
	Mime   string
	Width  int
	Height int
}

// optimizeImage inspects raw image bytes, rejects decompression-bomb sized
// inputs, and lightly optimizes the result before storage:
//
//   - PNG and GIF are passed through unchanged (GIF may be animated;
//     re-encoding would flatten it to a single frame).
//   - JPEG and WebP are decoded, downscaled (never upscaled) so their long
//     edge is at most maxLongEdge, and re-encoded as JPEG.
func optimizeImage(data []byte) (optimizedImage, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return optimizedImage{}, errUnsupportedImageFormat
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return optimizedImage{}, errUnsupportedImageFormat
	}
	if cfg.Width*cfg.Height > maxPixels {
		return optimizedImage{}, errImageTooLarge
	}

	switch format {
	case "png":
		return optimizedImage{Data: data, Ext: ".png", Mime: "image/png", Width: cfg.Width, Height: cfg.Height}, nil
	case "gif":
		return optimizedImage{Data: data, Ext: ".gif", Mime: "image/gif", Width: cfg.Width, Height: cfg.Height}, nil
	case "jpeg", "webp":
		img, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			return optimizedImage{}, decErr
		}
		resized := resizeToMaxEdge(img, maxLongEdge)
		var buf bytes.Buffer
		if encErr := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); encErr != nil {
			return optimizedImage{}, encErr
		}
		outBounds := resized.Bounds()
		return optimizedImage{
			Data:   buf.Bytes(),
			Ext:    ".jpg",
			Mime:   "image/jpeg",
			Width:  outBounds.Dx(),
			Height: outBounds.Dy(),
		}, nil
	default:
		return optimizedImage{}, errUnsupportedImageFormat
	}
}

// resizeToMaxEdge downscales img so its longest edge is at most maxEdge,
// preserving aspect ratio. It never upscales: if img is already within
// bounds it is returned unchanged.
func resizeToMaxEdge(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return img
	}
	longEdge := max(srcH, srcW)
	if longEdge <= maxEdge {
		return img
	}

	scale := float64(maxEdge) / float64(longEdge)
	dstW := int(float64(srcW)*scale + 0.5)
	dstH := int(float64(srcH)*scale + 0.5)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// mediaKind classifies an upload's sniffed content type for routing
// purposes.
type mediaKind int

const (
	mediaKindUnknown mediaKind = iota
	mediaKindImage
	mediaKindVideo
)

// classifyMedia maps an http.DetectContentType result to a mediaKind using
// an allow-list. Note that DetectContentType does not reliably recognize
// WebP; that's fine here because the real validation gate for images is
// optimizeImage's decode attempt, not this classifier. classifyMedia mainly
// exists to route video uploads to the (currently unimplemented) video path
// and obvious non-media to a 415 without paying for a decode attempt.
func classifyMedia(sniffed string) mediaKind {
	mt := sniffed
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = mt[:i]
	}
	mt = strings.TrimSpace(mt)

	switch mt {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return mediaKindImage
	case "video/mp4", "video/quicktime":
		// Reserved scaffold: video upload support is not implemented yet.
		return mediaKindVideo
	default:
		return mediaKindUnknown
	}
}
