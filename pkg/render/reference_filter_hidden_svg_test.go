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

// hiddenSVGInvertFilter declares #inv, an inverting feColorMatrix, inside a
// display:none <svg>; filterAttrs go on the <filter>.
func hiddenSVGInvertFilter(filterAttrs string) string {
	return `<svg style="display:none"><defs><filter id="inv" ` + filterAttrs + `>
<feColorMatrix type="matrix" values="-1 0 0 0 1 0 -1 0 0 1 0 0 -1 0 1 0 0 0 1 0"/>
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
		{"linearRGB attribute", `color-interpolation-filters="linearRGB"`, "green", "backdrop-filter:url(#inv)", color.RGBA{255, 229, 255, 255}},
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
