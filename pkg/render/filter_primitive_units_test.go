package render_test

import "testing"

// Under primitiveUnits="objectBoundingBox" a length scales by the float
// reference box (Filter::ApplyHorizontalScale, filter.cc:65-75 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not its pixel-snapped
// enclosing rect: a shape spanning x 0.25..20.75 (width 20.5) offset by
// dx="2" moves 41px, to 41.25..61.75. The snapped width 21 would move it to
// 42.25..62.75. (effect-reference-obb-dimensions covers the integral box.)
func TestFEOffset_ObjectBoundingBoxUnitsUseFloatReferenceBox(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="40">
<filter id="o" primitiveUnits="objectBoundingBox" x="0" y="0" width="4" height="1" color-interpolation-filters="sRGB"><feOffset dx="2" dy="0"/></filter>
<rect x="0.25" y="0" width="20.5" height="20" fill="green" filter="url(#o)"/></svg>`, 100, 40)
	if c := img.RGBAAt(41, 10); c == canvasWhite {
		t.Errorf("pixel (41,10) = %v, want the shape's left edge (it starts at x 41.25)", c)
	}
	wantPixel(t, img, 62, 10, canvasWhite, "past the shape's right edge at x 61.75")
}

// An SVG shape's filter reference box is its fill box, not its stroke box:
// SVGResources::ReferenceBoxForEffects defaults to GeometryBox::kFillBox,
// the ObjectBoundingBox (svg_resources.h:65-68, svg_resources.cc:53-75; the
// filter path calls it with the default at :375 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 100px-wide rect with a 20px
// stroke spans x 10..130; dx="0.1" moves it 10px (fill box), to 20..140, not
// 12px (stroke box), to 22..142.
func TestFEOffset_ObjectBoundingBoxUnitsUseFillBox(t *testing.T) {
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="160" height="80">
<filter id="o" primitiveUnits="objectBoundingBox" x="-1" y="-1" width="3" height="3" color-interpolation-filters="sRGB"><feOffset dx="0.1" dy="0"/></filter>
<rect x="20" y="20" width="100" height="40" fill="green" stroke="green" stroke-width="20" filter="url(#o)"/></svg>`, 160, 80)
	wantPixel(t, img, 20, 40, canvasGreen, "the stroke's moved left edge at x 20")
	wantPixel(t, img, 140, 40, canvasWhite, "past the moved right edge at x 140")
}
