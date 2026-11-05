package render_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

// TestFilterOutputClip_OwnClipsClipTheOutput — a filtered element's own
// clip-path and CSS clip are its filter's output clip: UpdateClipPathClip and
// UpdateCssClip run before UpdateFilter, whose output_clip is the clip they
// leave current (core/paint/paint_property_tree_builder.cc:4079-4082, 2485 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The drop shadow cast past the
// clip is clipped away.
func TestFilterOutputClip_OwnClipsClipTheOutput(t *testing.T) {
	for _, c := range []string{"clip-path:inset(0)", "position:absolute;clip:rect(0px,100px,100px,0px)"} {
		t.Run(c, func(t *testing.T) {
			page := `<!DOCTYPE html><html><body style="margin:0">` +
				`<div style="` + c + `;width:100px;height:100px;background:blue;filter:drop-shadow(150px 0 0 green)"></div>` +
				`</body></html>`
			img := renderToImage(t, page, 400, 200)
			sampleColorClose(t, img, 50, 50, 0, 0, 255, 0, "content")
			sampleColorClose(t, img, 200, 50, 255, 255, 255, 0, "shadow outside the clip")
		})
	}
}

// TestFilterOutputClip_ExpanderKeepsInput — for a filter that moves pixels,
// descendants paint under PixelMovingFilterClipExpander, a clip that never
// clips paint (paint_property_tree_builder.cc:2512-2532;
// platform/graphics/compositing/paint_chunks_to_cc_layer.cc:414-415 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an absolute child outside the
// element's CSS clip still feeds the filter, and its shadow lands inside the
// clip.
func TestFilterOutputClip_ExpanderKeepsInput(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:absolute;clip:rect(0px,100px,100px,0px);width:100px;height:100px;filter:drop-shadow(-125px 0 0 green)">` +
		`<div style="position:absolute;left:150px;top:0;width:50px;height:100px;background:blue"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 400, 200)
	// The child (150..200) casts its shadow at 25..75, inside the clip.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 0, "shadow of the child outside the clip")
	sampleColorClose(t, img, 175, 50, 255, 255, 255, 0, "the child itself is clipped")
}

// TestFilterOutputClip_ReplacedElement — an image's own CSS clip clips its
// filter output too: the drop shadow of the image's clipped-away right half
// lands inside the clip.
func TestFilterOutputClip_ReplacedElement(t *testing.T) {
	// A 7x7 red PNG: unique to this test, as the image cache is keyed by URL.
	src := image.NewRGBA(image.Rect(0, 0, 7, 7))
	for i := 0; i < len(src.Pix); i += 4 {
		copy(src.Pix[i:], []uint8{255, 0, 0, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	page := `<!DOCTYPE html><html><body style="margin:0">` +
		`<img style="position:absolute;left:100px;top:0;width:100px;height:100px;clip:rect(0px,50px,100px,-100px);` +
		`filter:drop-shadow(-100px 0 0 green)" src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `">` +
		`</body></html>`
	img := renderToImage(t, page, 300, 200)
	// The clip shows page x 0..150. The image's right half (150..200) is
	// clipped away but feeds the filter: its shadow lands at 50..100.
	sampleColorClose(t, img, 75, 50, 0, 128, 0, 0, "shadow of the clipped-away half")
	sampleColorClose(t, img, 125, 50, 255, 0, 0, 0, "the image's visible half")
	sampleColorClose(t, img, 175, 50, 255, 255, 255, 0, "the image's right half is clipped")
}

// TestFilterOutputClip_RootCanvasBackgroundIsFiltered — the canvas background
// is painted in the root's LocalBorderBoxProperties with its PreTransform
// (ViewPainter::PaintBoxDecorationBackground, view_painter.cc:183-188 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so it is under the root's filter
// (WPT compositing/root-element-filter, which Chrome passes).
func TestFilterOutputClip_RootCanvasBackgroundIsFiltered(t *testing.T) {
	const page = `<!DOCTYPE html><html style="background:yellow;filter:grayscale(1)">` +
		`<body style="margin:0"><div style="width:50px;height:50px;background:rgb(255,0,0)"></div></body></html>`
	img := renderToImage(t, page, 200, 200)
	// grayscale(1) of yellow: (0.2126 + 0.7152) * 255.
	// Inside the root's box: the buffer covers the root's paint extent only
	// (the canvas beyond it is LOU-704's sizing).
	sampleColorClose(t, img, 150, 25, 237, 237, 237, 2, "canvas background, filtered")
	sampleColorClose(t, img, 25, 25, 54, 54, 54, 2, "content, filtered")
}

// TestFilterOutputClip_ClipPathWithDescendants — with a filter that moves
// pixels, clip-path clips the output only: a descendant outside the
// clip-path still feeds the filter, and its shadow lands inside the clip-path
// on empty canvas.
func TestFilterOutputClip_ClipPathWithDescendants(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:relative;width:100px;height:100px;clip-path:inset(0);filter:drop-shadow(-125px 0 0 green)">` +
		`<div style="position:absolute;left:150px;top:0;width:50px;height:100px;background:blue"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 400, 200)
	// The child (150..200) casts its shadow at 25..75.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 0, "shadow of the child outside the clip-path")
	sampleColorClose(t, img, 175, 50, 255, 255, 255, 0, "child outside the clip-path")
}

// TestReplacedElementCSSClip_Transformed — an image's CSS clip clips it under
// a transform too.
func TestReplacedElementCSSClip_Transformed(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 9, 9))
	for i := 0; i < len(src.Pix); i += 4 {
		copy(src.Pix[i:], []uint8{255, 0, 0, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	page := `<!DOCTYPE html><html><body style="margin:0">` +
		`<img style="position:absolute;left:0;top:0;width:50px;height:50px;clip:rect(0px,25px,50px,0px);` +
		`transform:scale(2);transform-origin:0 0" src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `">` +
		`</body></html>`
	img := renderToImage(t, page, 200, 200)
	sampleColorClose(t, img, 25, 50, 255, 0, 0, 0, "inside the clip")
	sampleColorClose(t, img, 75, 50, 255, 255, 255, 0, "outside the clip")
}

// TestFilterOutputClip_NonPixelMovingFilter — a filter that does not move
// pixels is clipped by the element's own clip like its content; a positioned
// child outside the clip is clipped once.
func TestFilterOutputClip_NonPixelMovingFilter(t *testing.T) {
	const page = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="position:absolute;clip:rect(0px,100px,100px,0px);width:100px;height:100px;background:rgb(255,0,0);filter:grayscale(1)">` +
		`<div style="position:absolute;left:50px;top:50px;width:100px;height:100px;background:rgb(0,0,255)"></div>` +
		`</div></body></html>`
	img := renderToImage(t, page, 300, 300)
	sampleColorClose(t, img, 25, 25, 54, 54, 54, 2, "filtered content")
	sampleColorClose(t, img, 75, 75, 18, 18, 18, 2, "positioned child inside the clip")
	sampleColorClose(t, img, 125, 125, 255, 255, 255, 0, "positioned child outside the clip")
}
