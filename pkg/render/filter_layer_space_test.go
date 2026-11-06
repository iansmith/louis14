package render_test

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// LOU-665 (filter-effects worklist B13 + C4).
//
// B13: a filter is evaluated in the element's local transform space — the
// filter node's local_transform_space is the element's own transform
// (paint_property_tree_builder.cc:2453 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) — and its layer is drawn into
// device space through whatever part of the CTM the filter graph cannot
// handle (skif::Mapping::decomposeCTM, src/core/SkImageFilterTypes.cpp:259-298
// @ skia d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). A no-op filter must
// therefore paint the pixels the unfiltered transformed element paints, under
// any transform. Edge pixels may differ by resampling, so the check samples
// interior points.
//
// C4: the element's effect node (opacity, mix-blend-mode) is the filter node's
// parent (UpdateEffect before UpdateFilter, paint_property_tree_builder.cc:
// 4080-4082 @ a9f50e52), so opacity and blending apply to the filter's output:
// `filter:F; E` paints as `E` on a wrapper around a `filter:F` child.

const filterTestW, filterTestH = 800, 600

func renderFilterTestPage(t *testing.T, html string) *image.RGBA {
	t.Helper()
	return render.RenderPage(t, html, filterTestW, filterTestH)
}

func TestFilterLayerSpace_NoOpFilterMatchesUnfiltered(t *testing.T) {
	const box = `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;left:200px;top:200px;width:200px;height:200px;background:blue;
transform:%s;%%s}</style><div></div>`
	cases := []struct {
		name   string
		page   string
		filter string
		// Points inside the transformed box; at least one lies outside
		// the untransformed border box.
		points []image.Point
	}{
		{
			// Centre (300,300); the rotated top corner is at (300,159),
			// 41px above the untransformed box.
			name: "rotate", page: fmt.Sprintf(box, "rotate(45deg)"),
			filter: "filter:drop-shadow(0 0 0 transparent)",
			points: []image.Point{{300, 175}, {300, 300}, {175, 300}, {425, 300}},
		},
		{
			name: "skew", page: fmt.Sprintf(box, "skewX(30deg)"),
			filter: "filter:drop-shadow(0 0 0 transparent)",
			points: []image.Point{{170, 215}, {300, 300}, {430, 385}},
		},
		{
			// A color-only graph (kComplex) evaluates in device space.
			name: "rotate-color-filter", page: fmt.Sprintf(box, "rotate(45deg)"),
			filter: "filter:invert(0)",
			points: []image.Point{{300, 175}, {300, 300}},
		},
		{
			// Regression guard for the scale/translate device-space path.
			name: "scale-translate", page: fmt.Sprintf(box, "translate(30px,20px) scale(1.5)"),
			filter: "filter:drop-shadow(0 0 0 transparent)",
			points: []image.Point{{190, 190}, {330, 320}, {470, 450}},
		},
		{
			// css-transforms/3dtransform-and-filter-no-perspective-001. The
			// projected quad is (278.5,115.6) (553.8,15.4) (587.9,109.2)
			// (312.6,209.4) per the WPT test.
			name: "rotateZ-rotateX",
			page: `<!DOCTYPE html><style>body{margin:0}
div{height:500px;width:750px;overflow:hidden;position:relative}
span{background:blue;display:inline-block;top:0;left:200px;position:absolute;
width:293px;height:174px;transform-origin:0px -400px 0px;margin-top:300px;
transform:rotateZ(-20deg) rotateX(55deg);%s}</style><div><span></span></div>`,
			filter: "filter:drop-shadow(0px 60px 40px rgba(0,0,0,0))",
			points: []image.Point{{433, 112}, {300, 120}, {540, 50}},
		},
	}
	blue := color.RGBA{0, 0, 255, 255}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unfiltered := renderFilterTestPage(t, fmt.Sprintf(c.page, ""))
			filtered := renderFilterTestPage(t, fmt.Sprintf(c.page, c.filter))
			for _, p := range c.points {
				if got := unfiltered.RGBAAt(p.X, p.Y); got != blue {
					t.Fatalf("unfiltered at %v = %v, want blue (test geometry)", p, got)
				}
				wantPixel(t, filtered, p.X, p.Y, blue, "filtered must match unfiltered")
			}
		})
	}
}

// TestFilterEffectNesting_EffectAppliesToFilterOutput compares an element
// carrying both an effect and a filter with the effect on a wrapper around
// a filtered child, over the whole page.
func TestFilterEffectNesting_EffectAppliesToFilterOutput(t *testing.T) {
	const page = `<!DOCTYPE html><style>html{background:%s}body{margin:0}
div{width:100px;height:100px}.f{background:red;filter:%s}.e{%s}</style>%s`
	cases := []struct{ name, bg, filter, effect string }{
		// filter-effects/filter-with-mix-blend-mode.
		{"blend-grayscale", "green", "grayscale()", "mix-blend-mode:screen"},
		{"blend-drop-shadow", "green", "drop-shadow(20px 20px 0 blue)", "mix-blend-mode:screen"},
		{"opacity-drop-shadow", "white", "drop-shadow(20px 20px 0 black)", "opacity:0.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderFilterTestPage(t, fmt.Sprintf(page, c.bg, c.filter, c.effect, `<div class="f e"></div>`))
			want := renderFilterTestPage(t, fmt.Sprintf(page, c.bg, c.filter, c.effect, `<div class=e><div class=f></div></div>`))
			if c.name == "opacity-drop-shadow" {
				// Sanity check on the reference: where the opaque red covers
				// the shadow, only red at 50% shows over white.
				sampleColorClose(t, want, 50, 50, 255, 128, 128, 2, "reference overlap")
				sampleColorClose(t, want, 110, 110, 128, 128, 128, 2, "reference shadow-only")
			}
			wantImagesWithin(t, got, want, 1, "the nested reference")
		})
	}
}

func colorWithin(a, b color.RGBA, tol int) bool {
	return abs(int(a.R)-int(b.R)) <= tol && abs(int(a.G)-int(b.G)) <= tol &&
		abs(int(a.B)-int(b.B)) <= tol && abs(int(a.A)-int(b.A)) <= tol
}

// The filter's parameters are in the element's local space: a drop shadow's
// offset turns with the element's rotation (Skia maps it through the layer
// matrix and the layer is drawn through the remainder rotation).
func TestFilterLayerSpace_ShadowOffsetIsLocal(t *testing.T) {
	const page = `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;left:200px;top:200px;width:100px;height:100px;background:blue;
transform:rotate(90deg);filter:drop-shadow(20px 0 0 black)}</style><div></div>`
	img := renderFilterTestPage(t, page)
	wantPixel(t, img, 250, 310, color.RGBA{0, 0, 0, 255}, "local +x offset lands below the box after rotate(90deg)")
	wantPixel(t, img, 310, 250, color.RGBA{255, 255, 255, 255}, "no shadow to the right of the box")
}

// wantImagesWithin reports the pixels of got that differ from want by more
// than tol on any channel: how many, and the first.
func wantImagesWithin(t *testing.T, got, want *image.RGBA, tol int, why string) {
	t.Helper()
	diff, first := 0, image.Point{}
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !colorWithin(got.RGBAAt(x, y), want.RGBAAt(x, y), tol) {
				if diff == 0 {
					first = image.Pt(x, y)
				}
				diff++
			}
		}
	}
	if diff > 0 {
		t.Errorf("%d pixels differ from %s; first at %v: got %v, want %v",
			diff, why, first, got.RGBAAt(first.X, first.Y), want.RGBAAt(first.X, first.Y))
	}
}
