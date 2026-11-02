package images

import (
	"bytes"
	"encoding/binary"
	"testing"

	"louis14/pkg/graphics"
)

// exifJPEGHeader builds SOI + an APP1 Exif segment whose IFD0 holds one
// SHORT Orientation entry, in the given byte order.
func exifJPEGHeader(order binary.ByteOrder, orient uint16) []byte {
	var tiff bytes.Buffer
	if order == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	binary.Write(&tiff, order, uint16(42))
	binary.Write(&tiff, order, uint32(8))
	binary.Write(&tiff, order, uint16(2))
	binary.Write(&tiff, order, []uint16{0x010F, 2}) // Make (ASCII), skipped
	binary.Write(&tiff, order, uint32(4))
	tiff.WriteString("abc\x00")
	binary.Write(&tiff, order, []uint16{0x0112, 3})
	binary.Write(&tiff, order, uint32(1))
	binary.Write(&tiff, order, []uint16{orient, 0})
	binary.Write(&tiff, order, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8})
	out.Write([]byte{0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00}) // an APP0 before it
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write([]byte{0xFF, 0xDA, 0x00, 0x02})
	return out.Bytes()
}

// readExifOrientation: the SkExif::Parse fOrigin read that
// ImageDecoder::ApplyExifMetadata applies (image_decoder.cc:1008-1018 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestReadExifOrientation(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		for _, o := range []uint16{1, 3, 6, 8} {
			if got := readExifOrientation(exifJPEGHeader(order, o)).Orientation(); got != graphics.ImageOrientationEnum(o) {
				t.Errorf("%v orientation %d: got %d", order, o, got)
			}
		}
		if got := readExifOrientation(exifJPEGHeader(order, 9)).Orientation(); got != graphics.OriginTopLeft {
			t.Errorf("%v invalid orientation 9: got %d, want default", order, got)
		}
	}
	for name, data := range map[string][]byte{
		"png":       {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
		"no exif":   {0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02},
		"truncated": {0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x40, 'E', 'x'},
		"empty":     nil,
	} {
		if got := readExifOrientation(data).Orientation(); got != graphics.OriginTopLeft {
			t.Errorf("%s: got %d, want default", name, got)
		}
	}
}
