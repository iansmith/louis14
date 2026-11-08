package render

import "louis14/pkg/layout/svg"

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
	if img == nil || !img.HasImage() {
		return
	}
	// A filter runs whatever the visibility (ScopedSVGPaintState::
	// ComputePaintBehavior), as for a shape or container; the content is
	// not drawn yet.
	trySVGFilterDispatch(pctx, svgFilteredElement{node: img, style: img.Style},
		svg.ElementTransform(img, img.Style, img.Viewport))
}
