package imgprep

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(w, h int, c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestWebShrinksLargeImagesToJPEG(t *testing.T) {
	out, err := Web(pngOf(3850, 5951, color.NRGBA{236, 55, 80, 255}))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output is not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dy() != MaxSide || b.Dx() != 3850*MaxSide/5951 {
		t.Fatalf("size = %v, want long side %d with aspect kept", b, MaxSide)
	}
}

func TestWebKeepsSmallImagesAndFlattensTransparencyOnWhite(t *testing.T) {
	out, err := Web(pngOf(40, 30, color.NRGBA{0, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	img, _ := jpeg.Decode(bytes.NewReader(out))
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("size = %v, want 40x30", b)
	}
	if r, g, bl, _ := img.At(20, 15).RGBA(); r>>8 < 250 || g>>8 < 250 || bl>>8 < 250 {
		t.Fatalf("transparent pixel = %d,%d,%d, want white", r>>8, g>>8, bl>>8)
	}
}

func TestWebRejectsNonImages(t *testing.T) {
	if _, err := Web([]byte("%PDF-1.4 not an image")); err == nil {
		t.Fatal("want error for non-image")
	}
}
