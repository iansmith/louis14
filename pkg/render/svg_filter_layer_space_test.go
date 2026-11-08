package render_test

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"louis14/pkg/render"
)

// LOU-774 round 1: the layer an SVG filter runs in, and where its
// userSpaceOnUse geometry is anchored.

// Every SVG primitive clips to its primitive subregion (FilterEffect::
// clips_to_bounds_ defaults to true, filter_effect.cc:37; GetCropRect,
// filter_effect.cc:144-163 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so
// Skia wraps it in a crop filter (SkColorFilterImageFilter.cpp:92-93), and
// SkCropImageFilter keeps the default kScaleTranslate capability
// (SkImageFilter_Base.h:225-227 @ skia
// d19e557ac3175dc4f9e52d86dd1476c4dfe06a3e). A rotated flood therefore runs
// unrotated and is rotated when drawn: it fills the rotated square, not its
// axis-aligned bounds.
func TestSVGFilter_RotatedFloodFillsRotatedRegion(t *testing.T) {
	img := renderUserSpacePage(t, "", `<defs><filter id="fl" filterUnits="userSpaceOnUse" x="0" y="0" width="40" height="40"><feFlood flood-color="green"/></filter></defs>
<g transform="translate(60,10) rotate(45)"><rect width="40" height="40" fill="none" filter="url(#fl)"/></g>`)
	wantPixel(t, img, 60, 38, canvasGreen, "the centre of the rotated square")
	wantPixel(t, img, 35, 15, canvasWhite, "inside the square's axis-aligned bounds, outside the square")
}

// userSpaceOnUse light positions are in the filter's local space, whose
// origin on a box is its border-box origin (Filter::Resolve3dPoint leaves
// them as they are, filter.cc:77-88; the reference box is in the layer's
// local space, paint_property_tree_builder.cc:2414-2428 @ a9f50e52): a light
// at (30, 30) on a box at (100, 100) is above page (130, 130).
func TestHTMLFilter_UserSpaceLightAnchoredToBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}
#b{position:absolute;left:100px;top:100px;width:60px;height:60px;background:black;filter:url(#lit)}</style>
<svg width="0" height="0" style="position:absolute"><filter id="lit" color-interpolation-filters="sRGB">
<feDiffuseLighting lighting-color="white" surfaceScale="0"><fePointLight x="30" y="30" z="5"/></feDiffuseLighting></filter></svg>
<div id="b"></div>`, 200, 200)
	under, away := img.RGBAAt(130, 130), img.RGBAAt(108, 108)
	if under.R <= away.R {
		t.Errorf("lit %v under the light at page (130, 130), %v at (108, 108): want the light anchored to the box, brightest under it", under, away)
	}
}

// The filter region is clipped to the draw context's clip, grown by the
// filter's reach, before the buffer cap is checked (filterLayerClip, as for a
// CSS layer: get_layer_mapping_and_bounds, src/core/SkCanvas.cpp:570-600 @
// skia d19e557a): a 6000-wide userSpaceOnUse region on a 120px <svg> is
// filtered.
func TestSVGFilter_RegionClippedBeforeCap(t *testing.T) {
	img := renderUserSpacePage(t, "", `<defs><filter id="wide" filterUnits="userSpaceOnUse" x="-3000" y="0" width="6000" height="120"><feOffset dx="10"/></filter></defs>
<rect width="20" height="20" fill="green" filter="url(#wide)"/>`)
	wantPixel(t, img, 5, 10, canvasWhite, "the rect moved 10px right")
	wantPixel(t, img, 25, 10, canvasGreen, "the rect spans 10..30")
}

// Over the buffer cap, even after clipping, an element paints unfiltered on
// the SVG path as on the CSS layer path (prepareLayerFilter's fallback). The
// caps themselves differ (4000 and 8000 px: LOU-799).
func TestSVGFilter_OverCapPaintsUnfiltered(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}svg{display:block}</style>
<svg width="4200" height="40"><rect x="20" y="10" width="4100" height="20" fill="green" filter="blur(2px)"/></svg>`, 4200, 40)
	wantPixel(t, img, 20, 20, canvasGreen, "the rect's edge is sharp: no blur")
	wantPixel(t, img, 19, 20, canvasWhite, "nothing blurred outside the rect")
}

func TestHTMLFilter_OverCapPaintsUnfiltered(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}
#b{margin:10px 20px;width:8100px;height:20px;background:green;filter:blur(2px)}</style><div id="b"></div>`, 8200, 40)
	wantPixel(t, img, 20, 20, canvasGreen, "the box's edge is sharp: no blur")
	wantPixel(t, img, 19, 20, canvasWhite, "nothing blurred outside the box")
}

// A <g> whose object bounding box is empty, under an objectBoundingBox
// filter region, has an empty filter region and is not rendered: Blink's
// filter then has no primitives (FilterEffectBuilder::BuildReferenceFilter,
// filter_effect_builder.cc:545-550 @ a9f50e52). This hides a group of only
// <text> until text bounding boxes exist (LOU-795).
func TestSVGFilter_EmptyBoxGroupNotRendered(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}svg{display:block}</style>
<svg width="120" height="60"><defs><filter id="f"><feOffset dx="1"/></filter></defs>
<g filter="url(#f)"><rect width="0" height="0"/><text x="10" y="40" font-size="30">Hi</text></g></svg>`, 120, 60)
	if n := countPixels(img, func(c color.RGBA) bool { return c != canvasWhite }); n != 0 {
		t.Errorf("%d pixels painted, want none", n)
	}
}

// A CSS filter: url() on a box maps its primitives through the box's
// transform as an SVG element's do: feOffset dx=10 moves 10px under a
// translate, 20px under scale(2), and along device y under rotate(90deg).
func TestHTMLFilter_URLUnderTransforms(t *testing.T) {
	page := func(transform string) *image.RGBA {
		return render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}
#b{position:absolute;left:20px;top:20px;width:20px;height:20px;background:green;transform-origin:0 0;transform:`+transform+`;filter:url(#off)}</style>
<svg width="0" height="0" style="position:absolute"><filter id="off" filterUnits="userSpaceOnUse" x="-100" y="-100" width="300" height="300"><feOffset dx="10"/></filter></svg>
<div id="b"></div>`, 120, 120)
	}
	img := page("translate(30px,0)")
	wantPixel(t, img, 55, 30, canvasWhite, "translate: the box moved 10px right of 50")
	wantPixel(t, img, 75, 30, canvasGreen, "translate: the box spans 60..80")
	img = page("scale(2)")
	wantPixel(t, img, 30, 30, canvasWhite, "scale(2): the box moved 20px right of 20")
	wantPixel(t, img, 75, 30, canvasGreen, "scale(2): the box spans 40..80")
	img = page("rotate(90deg)")
	wantPixel(t, img, 10, 25, canvasWhite, "rotate(90deg): the box moved 10px down from 20")
	wantPixel(t, img, 10, 45, canvasGreen, "rotate(90deg): the box spans y 30..50")
}

// The graph is built once per paint: an feImage on a scaled box is fetched
// once, not once to read the graph's capability and again in its layer. The
// fetch fails, so no image cache answers the second build.
func TestHTMLFilter_FEImageFetchedOnce(t *testing.T) {
	fetches := 0
	render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}
#b{width:20px;height:20px;background:green;transform-origin:0 0;transform:scale(2);filter:url(#im)}</style>
<svg width="0" height="0" style="position:absolute"><filter id="im"><feImage href="missing.png"/></filter></svg>
<div id="b"></div>`, 60, 60, render.WithImageFetcher(func(string) ([]byte, error) {
		fetches++
		return nil, errors.New("not found")
	}))
	if fetches != 1 {
		t.Errorf("feImage fetched %d times, want 1", fetches)
	}
}
