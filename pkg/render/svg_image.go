package render

import (
	"image"
	"image/color"
	"math"

	"louis14/pkg/geometry"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout"
	"louis14/pkg/text"
)

// svgImageForContainer draws an SVG image laid out in a container of size
// containerSize into a w×h raster (the pixel-snapped destination), through
// louis14's own layout and paint tree. Mirrors SVGImage::DrawForContainer →
// DrawInternal → PaintRecordForCurrentFrame (core/svg/graphics/
// svg_image.cc:283-296, :451-495, :510-545 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the image's isolated SVG
// document (IsolatedSVGDocumentHost, built in SVGImage::DataChanged
// :879-930) has its root sized to the container (LayoutSVGRoot::
// SetContainerSize, PhysicalSize::FromSizeFFloor(container_size)), is laid
// out and painted, and the paint is mapped onto the destination rect. Blink
// replays a paint record; louis14 paints straight into the destination
// raster under that mapping's scale.
//
// The document has no URL (NullUrl) and runs no script: only data: URL
// resources load inside it (a nil image fetcher serves just those), and
// Document.Scripts is never executed. Its frame view's base background is
// transparent (isolated_svg_document_host.cc:148). Returns nil when the
// data does not parse or a size is empty. Each drawing is kept for the
// renderer's paint pass.
func (r *Renderer) svgImageForContainer(svg *images.SVGImage, containerSize geometry.SizeF, w, h int) image.Image {
	if w <= 0 || h <= 0 || containerSize.IsEmpty() {
		return nil
	}
	key := svgImageForContainerKey{svg, containerSize, w, h}
	if img, ok := r.svgImagesForContainer[key]; ok {
		return img
	}
	img := r.drawSVGImageForContainer(svg, containerSize, w, h)
	if r.svgImagesForContainer == nil {
		r.svgImagesForContainer = make(map[svgImageForContainerKey]image.Image)
	}
	r.svgImagesForContainer[key] = img
	return img
}

// svgImageForContainerKey identifies one SVG image drawn for one container
// into one raster size; Renderer.svgImagesForContainer keeps each such
// drawing for the paint pass (read-only), as Blink reuses an
// SVGImageForContainer's paint record while nothing changes.
type svgImageForContainerKey struct {
	svg           *images.SVGImage
	containerSize geometry.SizeF
	w, h          int
}

func (r *Renderer) drawSVGImageForContainer(svg *images.SVGImage, containerSize geometry.SizeF, w, h int) image.Image {
	doc, err := html.ParseSVGDocument(string(svg.Data()))
	if err != nil {
		return nil
	}
	// The view is resized to the rounded container size and the root laid
	// out at the floored one (PaintRecordForCurrentFrame, :462-473); the
	// root painter scales the root onto its pixel-snapped box, which fills
	// the view (SVGRootPainter::TransformToPixelSnappedBorderBox).
	viewW, viewH := math.Round(containerSize.Width), math.Round(containerSize.Height)
	if viewW <= 0 || viewH <= 0 {
		return nil
	}
	engine := layout.NewLayoutEngine(viewW, viewH)
	engine.SetSVGRootContainerSize(geometry.PhysicalSizeFromF64Floor(containerSize.Width, containerSize.Height))
	engine.SetFontConfig(r.fonts)
	boxes := engine.Layout(doc)

	target := image.NewRGBA(image.Rect(0, 0, w, h))
	// NewRendererForImage points the shared text layout at the image's draw
	// context; give it back to this renderer's afterwards.
	defer text.SetTextLayout(r.dc.TextLayout())
	ir := NewRendererForImage(target)
	ir.SetFonts(r.fonts)
	// The view maps onto the destination raster: DrawForContainer's src
	// rect is the container compensated by the rounding residual, i.e. the
	// rounded view (:287-293), and DrawInternal maps src onto dst
	// (:517-518).
	ir.dc.Scale(float64(w)/viewW, float64(h)/viewH)
	ir.renderOnBase(boxes, color.RGBA{})
	return target
}
