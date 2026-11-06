package render_test

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// LOU-730 (filter-effects worklist D3): a backdrop filter lives on an effect
// node in the element's local transform space (PropertyTreeManager::
// PopulateCcEffectNode, property_tree_manager.cc:1346-1360 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a rotated element inverts the
// backdrop under its rotated box, not under its untransformed border box.
func TestBackdropFilter_RotatedElementSamplesUnderItsRotatedBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;width:100px;height:100px;left:100px;top:100px}</style>
<div style="background:green"></div>
<div style="top:140px;backdrop-filter:invert(1);transform:rotate(30deg)"></div>`, 300, 300)
	// (120,195) is inside the rotated box and over the green box: green
	// inverted. (160,230) is inside the rotated box, over white: black.
	wantPixelWithinOne(t, img, 120, 195, invertedGreen, "green under the rotated box, inverted")
	wantPixelWithinOne(t, img, 160, 230, color.RGBA{0, 0, 0, 255}, "white under the rotated box, inverted")
}

// A scaled element's backdrop still paints when its layer-space size passes
// the 8000px buffer limit: the region is clamped to the device clip mapped
// into layer space, as a filter's is (filterLayerClip).
func TestBackdropFilter_ScaledElementPastBufferLimit(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;background:green}</style>
<div style="position:absolute;left:0;top:0;width:1000px;height:1000px;transform-origin:0 0;transform:scale(10);backdrop-filter:invert(1)"></div>`, 100, 100)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "green under the scaled box, inverted")
}

// invertedGreen is green (0,128,0) inverted.
var invertedGreen = color.RGBA{255, 127, 255, 255}

// backdropPage puts a 100×100 element styled effect over a box of the same
// size and background backdrop, at the page origin; extra (an <svg> of
// filters) goes first.
func backdropPage(backdrop, effect, extra string) string {
	return `<!DOCTYPE html><style>body{margin:0}
div{position:absolute;left:0;top:0;width:100px;height:100px}</style>
` + extra + `<div style="background:` + backdrop + `"></div>
<div style="` + effect + `"></div>`
}

// rotatedBlurPage puts a rotated backdrop-filtered box over a larger lime
// box of the same centre and rotation, on a red page: the box's backdrop is
// lime well past its anti-aliased edge, and red only beyond.
func rotatedBlurPage(filter string) string {
	return `<!DOCTYPE html><style>body{margin:0;background:red}
div{position:absolute;transform:rotate(45deg)}</style>
<div style="left:30px;top:30px;width:140px;height:140px;background:lime"></div>
<div style="left:50px;top:50px;width:100px;height:100px;backdrop-filter:` + filter + `"></div>`
}

// A rotated blur reads its backdrop bilinearly (ScaleTranslate layer, rotated
// LayerToDev) and duplicates only pixels inside the rotated box: the backdrop
// inside stays lime, with no red pulled in from outside the box.
func TestBackdropFilter_RotatedBlurReadsOnlyInsideTheBox(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   color.RGBA
	}{
		{"blur(4px)", color.RGBA{0, 255, 0, 255}},
		{"blur(4px) invert(1)", color.RGBA{255, 0, 255, 255}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			img := render.RenderPage(t, rotatedBlurPage(tc.filter), 200, 200)
			// The centre, and 4px inside the top corner (100, 29.3).
			for _, p := range [][2]int{{100, 100}, {100, 34}} {
				if got := img.RGBAAt(p[0], p[1]); !colorWithin(got, tc.want, 2) {
					t.Errorf("(%d,%d) = %v, want %v within 2: red from outside the box", p[0], p[1], got, tc.want)
				}
			}
		})
	}
}

// An element with both backdrop-filter and filter filters its backdrop with
// the backdrop ops followed by the filter ops (PaintLayer::
// UpdateBackdropFilters, paint_layer.cc:2601-2614 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), read from the real backdrop: a
// green backdrop inverted, then greyscaled, is grey.
func TestBackdropFilter_PlusFilterFiltersTheBackdrop(t *testing.T) {
	img := render.RenderPage(t, backdropPage("green", "backdrop-filter:invert(1);filter:grayscale(1)", ""), 120, 120)
	// (0,128,0) inverted is (255,127,255), greyscaled (163,163,163).
	wantPixelWithinOne(t, img, 50, 50, color.RGBA{163, 163, 163, 255}, "the inverted green backdrop greyscaled")
}

// A blurred backdrop mirrors at the box's edges (Blink's compositor crops
// the input to the bounds with a mirror edge mode): a 2px red strip along
// the box's left edge, over a lime backdrop, is not smeared across the
// blur's pad, so next to the edge lime still dominates.
func TestBackdropFilter_BlurMirrorsAtTheEdge(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;background:lime}</style>
<div style="position:absolute;left:0;top:0;width:2px;height:100px;background:red"></div>
<div style="position:absolute;left:0;top:0;width:100px;height:100px;backdrop-filter:blur(10px)"></div>`, 120, 120)
	if c := img.RGBAAt(1, 50); c.G <= c.R || c.R == 0 {
		t.Errorf("pixel (1,50) = %v: want lime dominating with the red edge blurred in (mirrored, not duplicated)", c)
	}
}

// The backdrop is the initial content of the layer the element composites
// in, which restores with the element's opacity (SkiaRenderer::
// PrepareCanvasForRPDQ, components/viz/service/display/skia_renderer.cc:
// 1736-1773 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): it is read from the
// page under the element even when the element paints into a buffer of its
// own, as for a filter inside opacity. The grey backdrop at half opacity over
// green (0,128,0) is (82,146,82).
func TestBackdropFilter_PlusFilterInsideOpacity(t *testing.T) {
	img := render.RenderPage(t, backdropPage("green", "backdrop-filter:invert(1);filter:grayscale(1);opacity:.5", ""), 120, 120)
	wantPixelWithinOne(t, img, 50, 50, color.RGBA{82, 146, 82, 255}, "the greyscaled backdrop at half opacity")
}

// A blended element's backdrop is the initial content of its layer too: the
// layer paint composites source-over, and the element's blend mode applies
// only to its content drawn into the layer (PrepareCanvasForRPDQ,
// skia_renderer.cc:1743-1751 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so
// the inverted green backdrop is not multiplied with the page.
func TestBackdropFilter_InsideMixBlendMode(t *testing.T) {
	img := render.RenderPage(t, backdropPage("green", "backdrop-filter:invert(1);mix-blend-mode:multiply", ""), 120, 120)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "the inverted backdrop")
}

// The filtered backdrop is converted back to sRGB (FilterEffectBuilder
// appends TransformInterpolationSpace, filter_effect_builder.cc:500-505 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an identity url() filter, which
// runs in linearRGB, leaves the grey backdrop grey.
func TestBackdropFilter_URLFilterEndsInSRGB(t *testing.T) {
	img := render.RenderPage(t, backdropPage("rgb(128,128,128)", "backdrop-filter:invert(0);filter:url(#f)",
		`<svg width="0" height="0" style="position:absolute"><filter id="f"><feColorMatrix type="saturate" values="1"/></filter></svg>`), 120, 120)
	wantPixelWithinOne(t, img, 50, 50, color.RGBA{128, 128, 128, 255}, "the grey backdrop through an identity url() filter")
}

// The backdrop is read padded by the whole chain's reach, mirrored at the
// box's edges: an offset appended from filter shifts mirrored backdrop pixels
// in at the box's left edge. A 2px red strip along that edge comes back only
// where the mirror lands on it, next to the edge: a clamped pad would repeat
// it across the offset, a transparent one show the page.
func TestBackdropFilter_OffsetReadsTheMirroredPad(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0;background:green}
div{position:absolute;left:0;top:0;height:100px}</style>
<svg width="0" height="0" style="position:absolute"><filter id="off" x="-50%" width="200%"><feOffset dx="20"/></filter></svg>
<div style="width:2px;background:red"></div>
<div style="width:100px;backdrop-filter:invert(1);filter:url(#off)"></div>`, 120, 120)
	wantPixelWithinOne(t, img, 10, 50, invertedGreen, "green mirrored in from x=10, inverted")
	wantPixelWithinOne(t, img, 19, 50, color.RGBA{0, 255, 255, 255}, "the red strip mirrored in from x=0, inverted")
}

// LOU-769: an effect layer under a perspective ancestor paints under the
// draw context's whole matrix even when the draw context's origin is behind
// the camera. Here the perspective origin is 600px to the left of the
// rotated box, so its local (0, 0) projects behind the eye. A half-opaque
// box over grey with backdrop-filter: invert(1) shows the inverted backdrop
// exactly where the box is, and nowhere else.
func TestEffectLayer_PerspectiveOriginBehindCamera(t *testing.T) {
	page := func(effect string) string {
		return perspectiveBoxPage(900, 600, "200px", "rotateY(50deg)", "rgba(0,128,0,0.5)", effect)
	}
	plain := render.RenderPage(t, page(""), 900, 220)
	img := render.RenderPage(t, page("backdrop-filter:invert(1)"), 900, 220)
	box := pixelBounds(plain, func(c color.RGBA) bool { return c.G > c.R+20 })
	inverted := pixelBounds(img, func(c color.RGBA) bool { return c.R < 70 && c.B < 70 })
	if box.Empty() || inverted != box {
		t.Errorf("inverted backdrop covers %v, want the box's %v", inverted, box)
	}
}

// LOU-769: an effect layer under a perspective ancestor whose camera plane
// cuts the box paints as the box paints without it: the review's cases of a
// box straddling the camera with its centre behind, the camera line through
// its centre, and a corner on the near plane (perspective p, the box's
// centre at 50px and its far edge at 100px from the rotation axis).
func TestEffectLayer_PerspectiveCameraPlaneCutsBox(t *testing.T) {
	for _, c := range []struct{ name, perspective, rotate string }{
		{"centre behind", "40px", "rotateY(75deg)"},
		{"centre behind, mirrored", "40px", "rotateY(-75deg)"},
		{"camera line through centre", "43.3px", "rotateY(60deg)"},
		{"camera line through centre, mirrored", "43.3px", "rotateY(-60deg)"},
		{"near-plane corner", "86.6px", "rotateY(60deg)"},
		{"near-plane corner, mirrored", "86.6px", "rotateY(-60deg)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			page := func(effect string) string {
				return perspectiveBoxPage(400, 100, c.perspective, c.rotate, "green", effect)
			}
			wantImagesWithin(t, render.RenderPage(t, page("backdrop-filter:invert(0);opacity:0.999"), 420, 220),
				render.RenderPage(t, page(""), 420, 220), 2, "the box without its effect layer")
		})
	}
}

// perspectiveBoxPage is a 100px box with background box and style effect,
// in a 200px square rotated by rotate under perspective, at left over a
// grey backdrop width wide.
func perspectiveBoxPage(width, left int, perspective, rotate, box, effect string) string {
	return fmt.Sprintf(`<!DOCTYPE html><style>body{margin:0}</style>
<div style="position:absolute;left:0;top:0;width:%dpx;height:220px;background:rgb(200,200,200)"></div>
<div style="position:absolute;left:%dpx;top:0;width:200px;height:200px;perspective:%s">
<div style="width:200px;height:200px;transform:%s">
<div style="width:100px;height:100px;background:%s;%s"></div></div></div>`, width, left, perspective, rotate, box, effect)
}

// pixelBounds is the bounding rect of img's pixels matching f.
func pixelBounds(img *image.RGBA, f func(color.RGBA) bool) image.Rectangle {
	var bb image.Rectangle
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if f(img.RGBAAt(x, y)) {
				bb = bb.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return bb
}

// LOU-769: translateZ(50px) under perspective:100px is a homography with a
// constant w of 0.5, a 2× scale about the perspective origin. It is affine
// up to scale, so a backdrop filter runs in its 2× layer space and blurs as
// under scale(2).
func TestBackdropFilter_TranslateZUnderPerspectiveScales(t *testing.T) {
	page := func(perspective, transform string) string {
		return `<!DOCTYPE html><style>body{margin:0}</style>
<div style="position:absolute;left:0;top:0;width:200px;height:200px;background:white"></div>
<div style="position:absolute;left:96px;top:0;width:8px;height:200px;background:black"></div>
<div style="position:absolute;left:0;top:0;width:200px;height:200px;` + perspective + `">
<div style="position:absolute;left:50px;top:50px;width:100px;height:100px;transform:` + transform + `;backdrop-filter:blur(2px)"></div></div>`
	}
	wantTranslateZMatchesScale2(t, page, "the backdrop blurred under scale(2)")
}
