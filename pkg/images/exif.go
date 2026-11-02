package images

import (
	"bytes"
	"encoding/binary"

	"louis14/pkg/graphics"
)

// readExifOrientation returns the orientation recorded in an image's EXIF
// metadata, or the default when there is none. It is the part of
// ImageDecoder::ApplyExifMetadata (platform/image-decoders/
// image_decoder.cc:1008-1018 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// louis14 needs: SkExif::Parse's fOrigin (EXIF tag 0x0112 in IFD0), read
// from the APP1 "Exif\0\0" segment JPEGImageDecoder hands it
// (jpeg_image_decoder.cc:469, getExifMetadata). Only JPEG is read; other
// formats keep the default orientation.
func readExifOrientation(data []byte) graphics.ImageOrientation {
	if exif := jpegExifPayload(data); exif != nil {
		if o, ok := exifOrigin(exif); ok {
			return graphics.NewImageOrientation(o)
		}
	}
	return graphics.NewImageOrientation(graphics.ImageOrientationDefault)
}

// jpegExifPayload returns the TIFF data of a JPEG's first APP1 Exif
// segment, scanning marker segments up to the start of scan.
func jpegExifPayload(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return nil
		}
		marker := data[i+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 || marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			return nil
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		end := i + 2 + segLen
		if segLen < 2 || end > len(data) {
			return nil
		}
		if payload := data[i+4 : end]; marker == 0xE1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			return payload[6:]
		}
		i = end
	}
	return nil
}

// exifOrigin reads the Orientation (0x0112) SHORT from IFD0 of TIFF data and
// reports it when it is a valid origin (1..8), as SkExif::Parse does.
func exifOrigin(tiff []byte) (graphics.ImageOrientationEnum, bool) {
	if len(tiff) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0, false
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd+2 > len(tiff) {
		return 0, false
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	for e := 0; e < count; e++ {
		entry := ifd + 2 + 12*e
		if entry+12 > len(tiff) {
			return 0, false
		}
		const tagOrientation, typeShort = 0x0112, 3
		if order.Uint16(tiff[entry:entry+2]) != tagOrientation {
			continue
		}
		if order.Uint16(tiff[entry+2:entry+4]) != typeShort || order.Uint32(tiff[entry+4:entry+8]) < 1 {
			return 0, false
		}
		o := graphics.ImageOrientationEnum(order.Uint16(tiff[entry+8 : entry+10]))
		return o, o >= graphics.OriginTopLeft && o <= graphics.OriginLeftBottom
	}
	return 0, false
}
