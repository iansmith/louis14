package render

// EXIF image orientation (LOU-611, css-images worklist D2).
//
// The decoder reads the EXIF origin (ImageDecoder::ApplyExifMetadata,
// image_decoder.cc:1008-1018 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b);
// with the initial `image-orientation: from-image` a LayoutImage's natural
// size is the oriented size (LayoutImageResource::GetNaturalDimensions) and
// BitmapImage::Draw applies ImageOrientation::TransformFromDefault.

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// jpegWithExifOrientation returns a data: URI for a w×h JPEG whose left half
// is `left` and right half `right`, tagged with EXIF orientation `orient`.
func jpegWithExifOrientation(t *testing.T, w, h int, left, right color.RGBA, orient uint16) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := left
			if x >= w/2 {
				c = right
			}
			img.SetRGBA(x, y, c)
		}
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	// APP1 "Exif\0\0" + big-endian TIFF header + IFD0 with one SHORT
	// Orientation (0x0112) entry.
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a")
	binary.Write(&tiff, binary.BigEndian, uint32(8))
	binary.Write(&tiff, binary.BigEndian, uint16(1))
	binary.Write(&tiff, binary.BigEndian, []uint16{0x0112, 3})
	binary.Write(&tiff, binary.BigEndian, uint32(1))
	binary.Write(&tiff, binary.BigEndian, []uint16{orient, 0})
	binary.Write(&tiff, binary.BigEndian, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	data := enc.Bytes()
	out.Write(data[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(data[2:])
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

// assertNear checks that the pixel at (x, y) is within 40 of c per channel
// (JPEG is lossy; the halves are flat colours, sampled away from edges).
func assertNear(t *testing.T, img *image.RGBA, x, y int, c color.RGBA, why string) {
	t.Helper()
	r, g, b := rgbAt(img, x, y)
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	if d(r, c.R) > 40 || d(g, c.G) > 40 || d(b, c.B) > 40 {
		t.Errorf("pixel (%d,%d) = rgb(%d,%d,%d), want ≈ rgb(%d,%d,%d): %s", x, y, r, g, b, c.R, c.G, c.B, why)
	}
}

var (
	orientRed  = color.RGBA{230, 20, 20, 255}
	orientBlue = color.RGBA{20, 20, 230, 255}
)

// Orientation 6 (kOriginRightTop, 90° clockwise): a 32×16 image whose left
// half is red is laid out 16×32 with red on top.
func TestImageOrientation_FromImageRotatesAndTransposes(t *testing.T) {
	uri := jpegWithExifOrientation(t, 32, 16, orientRed, orientBlue, 6)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block} div{width:10px;height:10px;background:rgb(0,255,0)}</style>
<img src="` + uri + `"><div></div>`
	img := RenderPage(t, src, 100, 100)
	assertNear(t, img, 8, 6, orientRed, "rotated left half on top")
	assertNear(t, img, 8, 26, orientBlue, "rotated right half at the bottom")
	wantRGB(t, img, 5, 34, 0, 255, 0, "sibling below the 32px-tall image")
	wantRGB(t, img, 20, 6, 255, 255, 255, "right of the 16px-wide image")
}

// image-orientation: none ignores the EXIF orientation.
func TestImageOrientation_NoneIgnoresExif(t *testing.T) {
	uri := jpegWithExifOrientation(t, 32, 16, orientBlue, orientRed, 6)
	src := `<!DOCTYPE html><style>body{margin:0} img{display:block; image-orientation:none} div{width:10px;height:10px;background:rgb(0,255,0)}</style>
<img src="` + uri + `"><div></div>`
	img := RenderPage(t, src, 100, 100)
	assertNear(t, img, 6, 8, orientBlue, "unrotated left half")
	assertNear(t, img, 26, 8, orientRed, "unrotated right half")
	wantRGB(t, img, 5, 18, 0, 255, 0, "sibling below the 16px-tall image")
}
