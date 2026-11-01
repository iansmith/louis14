package render_test

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
	"louis14/pkg/render"
)

func renderView(t *testing.T, src string) *image.RGBA {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	boxes := layout.NewLayoutEngine(200, 200).Layout(doc)
	target := image.NewRGBA(image.Rect(0, 0, 200, 200))
	render.NewRendererForImage(target).Render(boxes)
	return target
}

// TestViewBackgroundPaintsUnderInlineBodyContent pins the paint order of
// ViewPainter::PaintRootElementGroup (view_painter.cc:264 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the body background
// propagated to the view paints first, under everything, and the body
// itself skips it (LayoutBoxModelObject::BackgroundTransfersToView,
// layout_box_model_object.cc:1095-1125; BoxFragmentPainter::PaintBackground,
// box_fragment_painter.cc:1923). A display:inline body painted its
// canvas-filling background from its own (foreground) paint slot, over its
// block-in-inline child (css-backgrounds/background-color-body-propagation-003).
func TestViewBackgroundPaintsUnderInlineBodyContent(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0;display:inline;background:green">` +
		`<div style="width:50px;height:50px;background:blue"></div></body></html>`
	img := renderView(t, src)
	if got, want := rgbAt(img, 25, 25), (color.RGBA{0, 0, 255, 255}); got != want {
		t.Errorf("block child at (25,25) = %v, want %v (view background painted over it)", got, want)
	}
	if got, want := rgbAt(img, 150, 150), (color.RGBA{0, 128, 0, 255}); got != want {
		t.Errorf("canvas at (150,150) = %v, want %v", got, want)
	}
}

// TestViewBackgroundTranslucentRootPaintsOnce: the view paints the root
// background once, blended over the base white (PaintRootElementGroup's
// combined_background_color, view_painter.cc:416-431 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the root box paints none of
// its own (BackgroundTransfersToView).
func TestViewBackgroundTranslucentRootPaintsOnce(t *testing.T) {
	const src = `<!DOCTYPE html><html style="background:rgba(0,0,255,0.5)"><body style="margin:0"></body></html>`
	img := renderView(t, src)
	got := img.RGBAAt(100, 100)
	// White blended with 50% blue: (127|128, 127|128, 255).
	if got.B != 255 || got.R < 126 || got.R > 129 || got.G < 126 || got.G > 129 || got.A != 255 {
		t.Errorf("canvas at (100,100) = %v, want ~rgb(128,128,255) opaque", got)
	}
}

// TestViewBackgroundPaintsUnderNegativeZChildren: the view background
// paints before the root's stacking context, so a z-index:-1 child of a
// body with a perspective (the body's background propagates) paints over
// it (ViewPainter::PaintBoxDecorationBackground runs in the view's
// background phase, view_painter.cc:76 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The negative-z patch of
// css-transforms/perspective-split-by-zero-w-ref.html relies on this.
func TestViewBackgroundPaintsUnderNegativeZChildren(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0;perspective:500px;background:green">` +
		`<p style="margin:0;position:absolute;z-index:-1;left:0;top:0;width:50px;height:50px;background:blue"></p></body></html>`
	img := renderView(t, src)
	if got, want := rgbAt(img, 25, 25), (color.RGBA{0, 0, 255, 255}); got != want {
		t.Errorf("negative-z child at (25,25) = %v, want %v", got, want)
	}
}

// TestRootOverflowDoesNotClipRootBox: the document element is the viewport
// defining element when its overflow is not visible, and such a box does not
// respect CSS overflow — the viewport clips instead (LayoutBlock::
// RespectsCSSOverflow, layout_block.cc:166-170; Document::
// ViewportDefiningElement, document.cc:4147-4180 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Content past a short root's
// border box stays visible (css-transforms/transform-fixed-bg-007,
// css-overflow/overflow-body-propagation-007-ref).
func TestRootOverflowDoesNotClipRootBox(t *testing.T) {
	const src = `<!DOCTYPE html><html style="overflow:hidden"><body style="margin:0">` +
		`<div style="height:20px"></div>` +
		`<div style="position:relative;top:30px;width:50px;height:50px;background:blue"></div></body></html>`
	img := renderView(t, src)
	if got, want := rgbAt(img, 25, 90), (color.RGBA{0, 0, 255, 255}); got != want {
		t.Errorf("content below the root box at (25,90) = %v, want %v", got, want)
	}
}
