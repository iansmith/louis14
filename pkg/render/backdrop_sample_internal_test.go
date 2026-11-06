package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"louis14/pkg/geometry"
	"louis14/pkg/graphics/filters"
)

var (
	sampleLime = color.RGBA{0, 255, 0, 255}
	sampleRed  = color.RGBA{255, 0, 0, 255}
)

// sampleTarget returns a renderer over a red target with lime where inside
// reports a pixel centre inside the backdrop box.
func sampleTarget(w, h int, inside func(x, y float64) bool) *Renderer {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := sampleRed
			if inside(float64(x)+0.5, float64(y)+0.5) {
				c = sampleLime
			}
			img.SetRGBA(x, y, c)
		}
	}
	return NewRendererForImage(img)
}

// The edge mode works in local space, on the box itself: a layer pixel
// in the corner of a rotated box's device AABB (a Complex graph's layer is
// device space) reads the box's edge, not the red outside it.
func TestSampleBackdrop_ClampsToTheRotatedBox(t *testing.T) {
	r := sampleTarget(60, 60, func(x, y float64) bool { return math.Abs(x-30)+math.Abs(y-30) <= 10*math.Sqrt2 })
	ctm := geometry.Transform{}
	ctm.Translate(30, 30)
	ctm.Rotate(45)
	ctm.Translate(-30, -30)
	m := filters.Mapping{ParamToLayer: ctm}
	out := r.sampleBackdrop(image.Rect(16, 16, 44, 44), image.Rect(20, 20, 40, 40), m, ctm)
	if got := out.RGBAAt(0, 0); got.G < 100 {
		t.Errorf("AABB corner = %v, want the box's lime edge, not the red outside", got)
	}
}

// Bilinear taps are clamped to the pixels whose centres lie inside the
// box's device bounds: a box translated by a quarter pixel reads none of
// the red pixel past its edge.
func TestSampleBackdrop_TapsStayInsideTheBox(t *testing.T) {
	r := sampleTarget(40, 30, func(x, y float64) bool { return x >= 10 && x < 20 })
	ctm := geometry.Transform{}
	ctm.Translate(0.25, 0)
	out := r.sampleBackdrop(image.Rect(10, 10, 20, 20), image.Rect(10, 10, 20, 20), filters.Mapping{}, ctm)
	if got := out.RGBAAt(9, 5); !colorWithin(got, sampleLime, 1) {
		t.Errorf("edge column = %v, want lime: red bled in from past the edge", got)
	}
}

func colorWithin(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) <= tol && int(y)-int(x) <= tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// Backdrop filters read their input cropped to the layer bounds with a
// mirror edge mode (paint_layer.cc:2612-2614 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; Skia's SkTileMode::kMirror,
// which reflects about the edge so the edge pixel repeats once): a pad
// pixel reads the box pixel at its mirrored position, not the edge pixel.
func TestSampleBackdrop_MirrorsAtTheEdges(t *testing.T) {
	// Pixel x in [10, 20) has red channel 10*x; outside the box, black.
	img := image.NewRGBA(image.Rect(0, 0, 30, 5))
	for x := 10; x < 20; x++ {
		for y := range 5 {
			img.SetRGBA(x, y, color.RGBA{uint8(10 * x), 0, 0, 255})
		}
	}
	r := NewRendererForImage(img)
	for _, tc := range []struct {
		name   string
		scaleY float64
	}{
		{"integer translation", 1},
		{"bilinear", 1.0000001}, // not an integer translation: the general path
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctm geometry.Transform
			ctm.Scale(1, tc.scaleY)
			out := r.sampleBackdrop(image.Rect(4, 0, 26, 1), image.Rect(10, 0, 20, 5), filters.Mapping{}, ctm)
			// Layer x 9 → 10 (the edge, repeated), 8 → 11, 4 → 15; x 20 → 19,
			// 25 → 14.
			for _, p := range []struct{ x, want int }{{9, 10}, {8, 11}, {4, 15}, {20, 19}, {25, 14}} {
				if got := out.RGBAAt(p.x-4, 0).R; int(got) != 10*p.want {
					t.Errorf("layer x %d reads red %d, want %d (box x %d)", p.x, got, 10*p.want, p.want)
				}
			}
		})
	}
}
