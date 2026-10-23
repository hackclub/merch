// Package imgprep turns uploaded artwork (often 9 MB print-resolution PNGs)
// into web-sized JPEGs for the catalog.
package imgprep

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// MaxSide is the longest edge, in pixels, of a prepared image: sharp on a
// retina product page without shipping print files to phones.
const MaxSide = 1600

var ErrNotImage = errors.New("not a PNG, JPEG or WebP image")

// Web decodes a PNG, JPEG or WebP, scales it down to fit MaxSide (never up),
// flattens transparency onto white and re-encodes it as a JPEG.
func Web(data []byte) ([]byte, error) { return WebInset(data, 0) }

// WebInset is Web after cutting inset pixels off every edge: print art's
// bleed, so the picture shows the finished, trimmed piece.
func WebInset(data []byte, inset int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotImage
	}
	b := src.Bounds().Inset(inset)
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > MaxSide {
		w, h = w*MaxSide/long, h*MaxSide/long
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
