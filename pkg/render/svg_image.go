package render

import (
	"image"

	"louis14/pkg/geometry"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

// svgImageForContainer draws an SVG image laid out in a w×h container,
// through louis14's own layout and paint tree. Mirrors SVGImage::
// DrawForContainer → PaintRecordForCurrentFrame (core/svg/graphics/
// svg_image.cc:283-296, :451-495 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the image's isolated SVG document (IsolatedSVGDocumentHost, built in
// SVGImage::DataChanged :879-930) has its root sized to the container
// (LayoutSVGRoot::SetContainerSize) and its view resized to the rounded
// container size, then is laid out and painted. Blink records the paint and
// replays it into the destination; louis14 paints into a w×h raster at the
// destination size, so drawing it needs no resampling.
//
// The document has no URL (NullUrl) and runs no script: only data: URL
// resources load inside it (a nil image fetcher serves just those), and
// Document.Scripts is never executed. Returns nil when the data does not
// parse or w×h is empty. Each drawing is kept for the renderer's paint
// pass.
func (r *Renderer) svgImageForContainer(svg *images.SVGImage, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return nil
	}
	key := svgImageForContainerKey{svg, w, h}
	if img, ok := r.svgImagesForContainer[key]; ok {
		return img
	}
	img := r.drawSVGImageForContainer(svg, w, h)
	if r.svgImagesForContainer == nil {
		r.svgImagesForContainer = make(map[svgImageForContainerKey]image.Image)
	}
	r.svgImagesForContainer[key] = img
	return img
}

// svgImageForContainerKey identifies one SVG image drawn at one container
// size; Renderer.svgImagesForContainer keeps each such drawing for the
// paint pass (read-only), as Blink reuses an SVGImageForContainer's paint
// record while the container size is unchanged.
type svgImageForContainerKey struct {
	svg  *images.SVGImage
	w, h int
}

func (r *Renderer) drawSVGImageForContainer(svg *images.SVGImage, w, h int) image.Image {
	doc, err := html.ParseSVGDocument(string(svg.Data()))
	if err != nil {
		return nil
	}
	engine := layout.NewLayoutEngine(float64(w), float64(h))
	engine.SetSVGRootContainerSize(geometry.NewPhysicalSize(w, h))
	engine.SetFontConfig(r.fonts)
	boxes := engine.Layout(doc)

	target := image.NewRGBA(image.Rect(0, 0, w, h))
	// NewRendererForImage points the shared text layout at the image's draw
	// context; give it back to this renderer's afterwards.
	defer text.SetTextLayout(r.dc.TextLayout())
	ir := NewRendererForImage(target)
	ir.SetFonts(r.fonts)
	ir.Render(boxes)
	return target
}
