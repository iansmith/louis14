package filters

import "image"

// FETile tiles its input across the filter region. Mirrors Blink's
// platform/graphics/filters/fe_tile.{h,cc} and SVG Filter Effects 1
// §feTile.
//
// The tile *source* — the rectangle whose pixels are repeated — is the
// input's filter primitive subregion (FETile::GetSourceRect in Blink):
//   - When the input is a "source" effect (SourceGraphic, SourceAlpha,
//     FillPaint, StrokePaint, BackgroundImage, BackgroundAlpha) it has
//     no explicit primitive subregion of its own; the source rect
//     falls back to the filter region.
//   - Otherwise the source rect is the input's
//     FilterPrimitiveSubregion(), exposed via the subregionAware
//     interface check below.
//
// The destination is the filter region (technically the tile's own
// primitive subregion, but louis14 wraps subregion clipping at the
// builder layer via subregionClippedEffect so feTile fills the entire
// region and the outer wrapper trims after).
//
// Floor-mod indexing maps each output pixel back into the source rect:
//
//	src_x = src.Min.X + ((x - src.Min.X) mod src.Dx)
//	src_y = src.Min.Y + ((y - src.Min.Y) mod src.Dy)
//
// This aligns the tile origin with the source's own origin, matching
// Skia's TilePaintFilter behaviour with `src_rect == dst_rect` anchors.
type FETile struct {
	baseEffect
}

// NewFETile constructs an FETile fed by the given input.
func NewFETile(in FilterEffect, space InterpolationSpace) *FETile {
	return &FETile{baseEffect: baseEffect{inputs: []FilterEffect{in}, space: space}}
}

// subregionAware is implemented by effects that advertise a filter
// primitive subregion (the SVG x/y/width/height on the primitive element).
// subregionClippedEffect implements it; source-input effects do not.
// Mirrors the role of FilterEffect::FilterPrimitiveSubregion() in Blink
// (filter_effect.h) — louis14 doesn't put it on the universal interface
// because most effects don't need to expose it.
type subregionAware interface {
	FilterPrimitiveSubregion() image.Rectangle
}

// FilterPrimitiveSubregion is the accessor subregionClippedEffect uses
// to advertise the wrapped primitive's declared subregion. Returning a
// non-empty rect lets downstream effects (notably FETile) anchor their
// behaviour to the declared rect rather than to image data.
func (e *subregionClippedEffect) FilterPrimitiveSubregion() image.Rectangle {
	return e.subregion
}

// ApplyEffect tiles the input across the filter region.
func (e *FETile) ApplyEffect(inputs []*image.RGBA, region image.Rectangle) *image.RGBA {
	if len(inputs) == 0 || inputs[0] == nil {
		return newRGBA(region)
	}
	in := inputs[0]

	// Resolve the tile source rect. Default = full filter region (the
	// "source-input" branch in Blink's FETile::GetSourceRect).
	srcRect := region
	if len(e.inputs) > 0 && e.inputs[0] != nil {
		if sa, ok := e.inputs[0].(subregionAware); ok {
			if sub := sa.FilterPrimitiveSubregion(); !sub.Empty() {
				srcRect = sub
			}
		}
	}

	// Clip the source rect to the region (the input buffer only has
	// data for pixels inside the filter region; anything outside is
	// transparent/undefined). Empty intersection → transparent output.
	srcClipped := srcRect.Intersect(region)
	if srcClipped.Empty() {
		return newRGBA(region)
	}

	out := newRGBA(region)
	srcDx := srcClipped.Dx()
	srcDy := srcClipped.Dy()
	if srcDx <= 0 || srcDy <= 0 {
		return out
	}

	// Per-pixel floor-mod indexing into the source rect. Coordinates
	// are in buffer-local space (region.Min → buffer origin 0,0).
	srcX0 := srcClipped.Min.X - region.Min.X
	srcY0 := srcClipped.Min.Y - region.Min.Y
	regionDx := region.Dx()
	regionDy := region.Dy()
	for y := 0; y < regionDy; y++ {
		// sy in buffer-local coordinates of the source rect.
		dy := y - srcY0
		sy := dy % srcDy
		if sy < 0 {
			sy += srcDy
		}
		sy += srcY0
		soffRow := sy * in.Stride
		doffRow := y * out.Stride
		for x := 0; x < regionDx; x++ {
			dx := x - srcX0
			sx := dx % srcDx
			if sx < 0 {
				sx += srcDx
			}
			sx += srcX0
			soff := soffRow + sx*4
			doff := doffRow + x*4
			out.Pix[doff] = in.Pix[soff]
			out.Pix[doff+1] = in.Pix[soff+1]
			out.Pix[doff+2] = in.Pix[soff+2]
			out.Pix[doff+3] = in.Pix[soff+3]
		}
	}
	return out
}
