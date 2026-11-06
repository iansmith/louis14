package render_test

import (
	"image/color"
	"testing"
)

// LOU-747 (filter-effects worklist B8): a url() filter resolves to the
// <filter> element with that id in the document, laid out or not.
// FilterEffectBuilder::BuildReferenceFilter takes the element from
// SVGResource::Target() and builds the graph from the DOM
// (filter_effect_builder.cc:514-521; LocalSVGResource, svg_resource.cc:196 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a <filter> inside a
// display:none <svg> applies.
//
// With no layout object, its color-interpolation-filters comes from its
// presentation attribute, else auto, which runs in sRGB
// (ColorInterpolationForElement, svg_filter_builder.cc:136-170 @ a9f50e52):
// the hidden inverting matrix inverts green (0,128,0) to (255,127,255) as
// invert(1) does, and to (255,229,255) in linearRGB.

// invertMatrix inverts each colour channel; invertedGreenLinear is green
// (0,128,0) through it run in linearRGB.
const invertMatrix = `<feColorMatrix type="matrix" values="-1 0 0 0 1 0 -1 0 0 1 0 0 -1 0 1 0 0 0 1 0"/>`

var invertedGreenLinear = color.RGBA{255, 229, 255, 255}

// hiddenSVGInvertFilter declares #inv, an inverting feColorMatrix, inside a
// display:none <svg>; filterAttrs go on the <filter>.
func hiddenSVGInvertFilter(filterAttrs string) string {
	return `<svg style="display:none"><defs><filter id="inv" ` + filterAttrs + `>
` + invertMatrix + `
</filter></defs></svg>`
}

func TestReferenceFilter_InsideDisplayNoneSVG(t *testing.T) {
	for _, tc := range []struct {
		name, filterAttrs, backdrop, effect string
		want                                color.RGBA
	}{
		// backdrop-filter-reference-filter.
		{"backdrop-filter", "", "green", "backdrop-filter:url(#inv)", invertedGreen},
		{"filter", "", "transparent", "background:green;filter:url(#inv)", invertedGreen},
		{"linearRGB attribute", `color-interpolation-filters="linearRGB"`, "green", "backdrop-filter:url(#inv)", invertedGreenLinear},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, backdropPage(tc.backdrop, tc.effect, hiddenSVGInvertFilter(tc.filterAttrs)), 120, 120)
			wantPixelWithinOne(t, img, 50, 50, tc.want, "green through the hidden <svg>'s filter")
		})
	}
}

// A url() filter resolves to the first <filter> with the id in tree order,
// laid out or not (TreeScope id lookup; LocalSVGResource, svg_resource.cc:196
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a hidden #inv before a
// laid-out #inv that floods red wins, from CSS and from SVG content alike.
func TestReferenceFilter_FirstInTreeOrderWins(t *testing.T) {
	const laidOut = `<svg width="100" height="100" style="position:absolute;left:0;top:100px">
<filter id="inv"><feFlood flood-color="red"/></filter>
<rect width="100" height="100" fill="green" filter="url(#inv)"/></svg>`
	img := renderToImage(t, backdropPage("green", "backdrop-filter:url(#inv)", hiddenSVGInvertFilter("")+laidOut), 120, 220)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "CSS backdrop-filter: the hidden <filter>, first in tree order")
	wantPixelWithinOne(t, img, 50, 150, invertedGreen, "SVG content filter: the hidden <filter>, first in tree order")
}

// Ids resolve per document (TreeScope::getElementById): a page inside an
// iframe resolves #inv to its own hidden <filter>, not to the outer page's
// laid-out one that floods red.
func TestReferenceFilter_PerDocument(t *testing.T) {
	const outer = `<svg width="0" height="0"><filter id="inv"><feFlood flood-color="red"/></filter></svg>`
	frame := `<iframe style="position:absolute;left:0;top:0;width:100px;height:100px;border:0" srcdoc='<style>body{margin:0}</style>` +
		hiddenSVGInvertFilter("") + `<div style="width:100px;height:100px;background:green;filter:url(#inv)"></div>'></iframe>`
	img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+outer+frame, 120, 120)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "the iframe document's own hidden <filter>")
}

// One id map per document, every kind in it (TreeScope::getElementById), and
// the referencing property checks the kind: the first element with the id
// wins, and a reference to an element of the wrong kind resolves to no
// resource.
func TestReferenceFilter_MixedKindIDs(t *testing.T) {
	const gradient = `<svg width="100" height="100" style="position:absolute;left:0;top:0">
<linearGradient id="a"><stop stop-color="blue"/></linearGradient>
<rect width="100" height="100" fill="url(#a)"/></svg>`
	filtered := `<div style="position:absolute;left:0;top:100px;width:100px;height:100px;background:green;filter:url(#a)"></div>`
	hidden := `<svg style="display:none"><filter id="a">` + invertMatrix + `</filter></svg>`
	blue := color.RGBA{0, 0, 255, 255}
	t.Run("gradient first", func(t *testing.T) {
		img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+gradient+hidden+filtered, 120, 220)
		wantPixel(t, img, 50, 50, blue, "fill:url(#a) gets the gradient")
		wantPixel(t, img, 50, 150, canvasGreen, "filter:url(#a) names a gradient: no filter")
	})
	t.Run("hidden filter first", func(t *testing.T) {
		img := renderToImage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+hidden+gradient+filtered, 120, 220)
		wantPixel(t, img, 50, 50, canvasWhite, "fill:url(#a) names a filter: no paint")
		wantPixelWithinOne(t, img, 50, 150, invertedGreen, "filter:url(#a) gets the hidden filter")
	})
}

// color-interpolation-filters: auto runs in sRGB, as does an invalid value on
// a filter with no layout object (its attribute is ignored and auto applies)
// (ResolveInterpolationSpace / ColorInterpolationForElement,
// svg_filter_builder.cc:136-162 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// A <filter> whose own display is none has no layout object
// (SVGElement::LayoutObjectIsNeeded, svg_element.cc:873-875), even inside a
// laid-out <svg>; one under <defs> has one (LayoutSVGHiddenContainer,
// svg_defs_element.cc:31-33), and so the linearRGB initial value.
func TestReferenceFilter_InterpolationSpace(t *testing.T) {
	for _, tc := range []struct {
		name, svg string
		want      color.RGBA
	}{
		{"laid out, auto", `<svg width="0" height="0"><filter id="inv" color-interpolation-filters="auto">` + invertMatrix + `</filter></svg>`, invertedGreen},
		{"hidden, invalid attribute", hiddenSVGInvertFilter(`color-interpolation-filters="bogus"`), invertedGreen},
		{"own display none", `<svg width="0" height="0"><filter id="inv" style="display:none">` + invertMatrix + `</filter></svg>`, invertedGreen},
		{"under defs", `<svg width="0" height="0"><defs><filter id="inv">` + invertMatrix + `</filter></defs></svg>`, invertedGreenLinear},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderToImage(t, backdropPage("transparent", "background:green;filter:url(#inv)", tc.svg), 120, 120)
			wantPixelWithinOne(t, img, 50, 50, tc.want, "green through the inverting filter")
		})
	}
}
