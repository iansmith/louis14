// Package graphics holds ports of Blink's platform/graphics primitives that
// are not tied to a paint backend.
package graphics

import "louis14/pkg/geometry"

// ImageOrientationEnum is the EXIF orientation of an image's pixel data:
// where row 0 and column 0 of the stored pixels belong. Mirrors Blink's
// ImageOrientationEnum (platform/graphics/image_orientation_enum.h:14-29 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); values are the EXIF tag values.
type ImageOrientationEnum int8

const (
	OriginTopLeft     ImageOrientationEnum = 1 // default
	OriginTopRight    ImageOrientationEnum = 2 // mirror along y-axis
	OriginBottomRight ImageOrientationEnum = 3 // 180 degree rotation
	OriginBottomLeft  ImageOrientationEnum = 4 // mirror along the x-axis
	OriginLeftTop     ImageOrientationEnum = 5 // mirror along x-axis + 270 degree CW rotation
	OriginRightTop    ImageOrientationEnum = 6 // 90 degree CW rotation
	OriginRightBottom ImageOrientationEnum = 7 // mirror along x-axis + 90 degree CW rotation
	OriginLeftBottom  ImageOrientationEnum = 8 // 270 degree CW rotation

	ImageOrientationDefault = OriginTopLeft
)

// RespectImageOrientationEnum says whether a draw or size query applies the
// image's orientation. Mirrors Blink's RespectImageOrientationEnum
// (platform/graphics/image_orientation.h:43-46 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type RespectImageOrientationEnum uint8

const (
	DoNotRespectImageOrientation RespectImageOrientationEnum = 0
	RespectImageOrientation      RespectImageOrientationEnum = 1
)

// ImageOrientation wraps an ImageOrientationEnum. Mirrors Blink's
// ImageOrientation (platform/graphics/image_orientation.h:48-78 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The zero value is the default
// orientation.
type ImageOrientation struct {
	orientation ImageOrientationEnum
}

// NewImageOrientation mirrors the ImageOrientation(ImageOrientationEnum)
// constructor.
func NewImageOrientation(o ImageOrientationEnum) ImageOrientation {
	return ImageOrientation{orientation: o}
}

// Orientation mirrors ImageOrientation::Orientation.
func (o ImageOrientation) Orientation() ImageOrientationEnum {
	if o.orientation == 0 {
		return ImageOrientationDefault
	}
	return o.orientation
}

// UsesWidthAsHeight reports whether the orientation transposes the image:
// values 5 through 8. Mirrors ImageOrientation::UsesWidthAsHeight
// (image_orientation.h:56-59 @ the same SHA).
func (o ImageOrientation) UsesWidthAsHeight() bool {
	return o.Orientation() >= OriginLeftTop
}

// TransformFromDefault returns the transform that draws an image according
// to the orientation into a box of drawnWidth×drawnHeight (the oriented
// size) whose origin is at (0, 0). Mirrors
// ImageOrientation::TransformFromDefault (image_orientation.cc:35-60 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (o ImageOrientation) TransformFromDefault(drawnWidth, drawnHeight float64) geometry.AffineTransform {
	w, h := drawnWidth, drawnHeight
	switch o.Orientation() {
	case OriginTopRight:
		return geometry.NewAffineTransform(-1, 0, 0, 1, w, 0)
	case OriginBottomRight:
		return geometry.NewAffineTransform(-1, 0, 0, -1, w, h)
	case OriginBottomLeft:
		return geometry.NewAffineTransform(1, 0, 0, -1, 0, h)
	case OriginLeftTop:
		return geometry.NewAffineTransform(0, 1, 1, 0, 0, 0)
	case OriginRightTop:
		return geometry.NewAffineTransform(0, 1, -1, 0, w, 0)
	case OriginRightBottom:
		return geometry.NewAffineTransform(0, -1, -1, 0, w, h)
	case OriginLeftBottom:
		return geometry.NewAffineTransform(0, -1, 1, 0, 0, h)
	}
	return geometry.Identity()
}
