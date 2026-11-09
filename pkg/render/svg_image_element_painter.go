package render

import (
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// paintSVGImageElement paints an SVG <image>. Mirrors Blink's
// SVGImagePainter::Paint (core/paint/svg_image_painter.cc:41-93 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an element with no image
// (LayoutImageResource::HasImage) paints nothing, its filter included; one
// with an image runs its filter, the paint state being set up before the
// content, and then paints its content (PaintForeground), which returns
// for an empty viewport (svg_image_painter.cc:95-98). Drawing the image
// itself is not ported yet (LOU-797 follow-up), so the content is empty and
// only the filter paints.
func paintSVGImageElement(pctx *svgPaintContext, img *svg.SVGImage) {
	// No image paints nothing (svg_image_painter.cc:41-44). With one, only
	// the filter paints until the image is drawn (LOU-797 follow-up): drop
	// the no-filter return then, as Blink paints the content inside the
	// paint state whether or not there is a filter (:72-86).
	if img == nil || !img.HasImage() || img.Style == nil || len(img.Style.GetFilter()) == 0 {
		return
	}
	// The filter is built inside the element's transform (as for a shape
	// or container: buildSVGElementFilter) and runs whatever the visibility
	// (ScopedSVGPaintState::ComputePaintBehavior); the content is not drawn
	// yet.
	guard := pushScopedSVGTransform(pctx, svg.ElementTransform(img, img.Style, img.Viewport), geometry.RectF{})
	defer guard.Close()
	trySVGFilterDispatch(pctx, img, img.Style, func(*svgPaintContext) {})
}
