package render_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"louis14/pkg/render"
)

// LOU-654: Blink's per-call anti-aliasing choices (all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Fills, strokes and images
// anti-alias by default (GraphicsContextState, platform/graphics/
// graphics_context_state.cc:22-30); property-tree clips anti-alias
// (paint_chunks_to_cc_layer.cc:573-583); a border side's miter clip
// anti-aliases only for a soft miter (box_border_painter.cc:2426-2463);
// SVG shape-rendering: crispEdges turns it off (svg_shape_painter.cc:98-155).

// isGray reports whether a pixel of a black-on-white render is partially
// covered.
func isGray(img *image.RGBA, x, y int) bool { return grayPixel(img.RGBAAt(x, y)) }

// grayPixel reports a partly covered black-on-white pixel.
func grayPixel(c color.RGBA) bool {
	return c.R > 0 && c.R < 255 && c.R == c.G && c.G == c.B
}

// assertRingAA checks that a circular edge of radius r centred on (cx, cy)
// is anti-aliased: gray pixels lie on the ring (within 1.5px of it), and
// none lie away from it.
func assertRingAA(t *testing.T, img *image.RGBA, cx, cy, r float64, what string) {
	t.Helper()
	on, off := 0, 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !isGray(img, x, y) {
				continue
			}
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			if math.Abs(d-r) <= 1.5 {
				on++
			} else {
				off++
			}
		}
	}
	// A ring of radius r crosses at least ~2πr pixel edges; demand half.
	if on < int(math.Pi*r) {
		t.Errorf("%s: %d anti-aliased pixels on the r=%g edge, want ≥ %d", what, on, r, int(math.Pi*r))
	}
	if off != 0 {
		t.Errorf("%s: %d gray pixels away from the edge", what, off)
	}
}

func renderAA(t *testing.T, body string) *image.RGBA {
	t.Helper()
	return render.RenderPage(t, `<!DOCTYPE html><html><body style="margin:0;background:white">`+body+`</body></html>`, 120, 120)
}

// A rounded background fill (GraphicsContext::FillRoundedRect with
// FillFlags) anti-aliases its curved edge.
func TestAntialias_RoundedBackgroundFill(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:60px;height:60px;border-radius:30px;background:black"></div>`)
	assertRingAA(t, img, 40, 40, 30, "rounded background")
}

// A rotated box's edges anti-alias (a guard: the default fill flags).
func TestAntialias_RotatedBox(t *testing.T) {
	img := renderAA(t, `<div style="margin:30px;width:50px;height:50px;background:black;transform:rotate(30deg)"></div>`)
	if countPixels(img, grayPixel) == 0 {
		t.Fatal("rotated box has no anti-aliased edge pixels")
	}
}

// A rounded overflow clip is a property-tree ClipRRectOp with
// antialias=true (paint_chunks_to_cc_layer.cc:573-576).
func TestAntialias_RoundedOverflowClip(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:60px;height:60px;border-radius:30px;overflow:hidden">`+
		`<div style="width:60px;height:60px;background:black"></div></div>`)
	assertRingAA(t, img, 40, 40, 30, "rounded overflow clip")
}

// A path-based clip-path is a ClipPathOp with antialias=true
// (paint_chunks_to_cc_layer.cc:581-583).
func TestAntialias_ClipPathCircle(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:60px;height:60px;background:black;clip-path:circle(25px)"></div>`)
	assertRingAA(t, img, 40, 40, 25, "clip-path: circle()")
}

// Adjacent border sides of different colours meet at a soft miter, whose
// clip anti-aliases (ClipPolygon(…, first_miter == kSoftMiter),
// box_border_painter.cc:2426-2432): the diagonal blends both colours.
func TestAntialias_SoftMiterBlends(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:40px;height:40px;`+
		`border-top:20px solid rgb(255,0,0);border-right:20px solid rgb(0,0,255)"></div>`)
	blended := 0
	for y := 10; y < 30; y++ {
		for x := 50; x < 70; x++ {
			if c := img.RGBAAt(x, y); c.G == 0 && c.R > 0 && c.B > 0 {
				blended++
			}
		}
	}
	if blended == 0 {
		t.Fatal("soft miter between red and blue sides has no blended pixels")
	}
}

// Adjacent sides of one colour but miter-requiring styles meet at a hard
// miter, clipped without anti-aliasing (box_border_painter.cc:2426-2432),
// so the shared diagonal leaves no seam: every corner pixel is solid black.
func TestAntialias_HardMiterNoSeam(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:40px;height:40px;`+
		`border-top:20px solid black;border-right:20px dashed black"></div>`)
	for y := 10; y < 30; y++ {
		for x := 50; x < 70; x++ {
			if c := img.RGBAAt(x, y); c != (color.RGBA{0, 0, 0, 255}) {
				t.Fatalf("hard miter corner pixel (%d,%d) is %v; want solid black (no anti-aliased seam)", x, y, c)
			}
		}
	}
}

// A rounded inner border edge is cut by an anti-aliased clip-out
// (ClipOutContouredRect, box_border_painter.cc:1467-1469, or
// GraphicsContext::FillDRRect's drawDRRect). The 60px padding box with
// inner radius 30 makes the inner edge a circle of r=30 about (50,50); the
// outer edge is the r=40 border-box circle.
func TestAntialias_RoundedInnerBorderEdge(t *testing.T) {
	img := renderAA(t, `<div style="margin:10px;width:60px;height:60px;border:10px solid black;border-radius:40px"></div>`)
	inner := 0
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			if d := math.Hypot(float64(x)+0.5-50, float64(y)+0.5-50); math.Abs(d-30) <= 1.5 && isGray(img, x, y) {
				inner++
			}
		}
	}
	if inner < 94 {
		t.Fatalf("rounded inner border edge has %d anti-aliased pixels, want ≥ %d", inner, 94)
	}
}

// An image drawn under a rotation anti-aliases its edges
// (GraphicsContext::DrawImage copies FillFlags, graphics_context.cc:519).
// A guard: the rotated layer may also reach the canvas as a rotated buffer.
func TestAntialias_RotatedImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 7, 7))
	for i := range src.Pix {
		if i%4 == 3 {
			src.Pix[i] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	img := renderAA(t, `<img src="`+uri+`" style="margin:30px;width:49px;height:49px;transform:rotate(30deg)">`)
	if countPixels(img, grayPixel) == 0 {
		t.Fatal("rotated image has no anti-aliased edge pixels")
	}
}

// SVG shapes anti-alias unless shape-rendering is crispEdges or
// optimizeSpeed (SVGShapePainter::PaintShape, svg_shape_painter.cc:98-102).
func TestAntialias_SVGShapeRendering(t *testing.T) {
	const circle = `<circle cx="50" cy="50" r="30" fill="black"%s/>`
	assertRingAA(t, renderAA(t, `<svg width="100" height="100">`+fmt.Sprintf(circle, "")+`</svg>`), 50, 50, 30, "default SVG circle")
	for name, svg := range map[string]string{
		"crispEdges":           `<svg width="100" height="100">` + fmt.Sprintf(circle, ` shape-rendering="crispEdges"`) + `</svg>`,
		"optimizeSpeed":        `<svg width="100" height="100">` + fmt.Sprintf(circle, ` shape-rendering="optimizeSpeed"`) + `</svg>`,
		"inherited crispEdges": `<svg width="100" height="100" style="shape-rendering:crispEdges">` + fmt.Sprintf(circle, "") + `</svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			if n := countPixels(renderAA(t, svg), grayPixel); n != 0 {
				t.Errorf("%d anti-aliased pixels; want none", n)
			}
		})
	}
}
