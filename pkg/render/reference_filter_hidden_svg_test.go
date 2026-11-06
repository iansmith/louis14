package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/render"
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
// (ColorInterpolationForElement and ResolveInterpolationSpace,
// svg_filter_builder.cc:136-155 and 157-162 @ a9f50e52):
// the hidden inverting matrix inverts green (0,128,0) to (255,127,255) as
// invert(1) does, and to (255,229,255) in linearRGB.

// invertMatrix inverts each colour channel; invertedGreenLinear is green
// (0,128,0) through it run in linearRGB.
const invertMatrixValues = "-1 0 0 0 1 0 -1 0 0 1 0 0 -1 0 1 0 0 0 1 0"

const invertMatrix = `<feColorMatrix type="matrix" values="` + invertMatrixValues + `"/>`

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
			img := render.RenderPage(t, backdropPage(tc.backdrop, tc.effect, hiddenSVGInvertFilter(tc.filterAttrs)), 120, 120)
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
	img := render.RenderPage(t, backdropPage("green", "backdrop-filter:url(#inv)", hiddenSVGInvertFilter("")+laidOut), 120, 220)
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
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+outer+frame, 120, 120)
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
		img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+gradient+hidden+filtered, 120, 220)
		wantPixel(t, img, 50, 50, blue, "fill:url(#a) gets the gradient")
		wantPixel(t, img, 50, 150, canvasGreen, "filter:url(#a) names a gradient: no filter")
	})
	t.Run("hidden filter first", func(t *testing.T) {
		img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+hidden+gradient+filtered, 120, 220)
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
			img := render.RenderPage(t, backdropPage("transparent", "background:green;filter:url(#inv)", tc.svg), 120, 120)
			wantPixelWithinOne(t, img, 50, 50, tc.want, "green through the inverting filter")
		})
	}
}

// findTag returns the first element named tag under n, in tree order.
func findTag(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.TagName == tag {
		return n
	}
	for _, c := range n.Children {
		if f := findTag(c, tag); f != nil {
			return f
		}
	}
	return nil
}

// A script-built resource element resolves as a parsed one does. louis14's
// createElementNS records no namespace (its node reports XHTML), so an
// element inside an <svg> is SVG by its <svg> ancestor and its tag, as the
// SVG build decides.
func TestReferenceResolve_ScriptBuiltGradient(t *testing.T) {
	doc := render.ParsePage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect width="100" height="100" fill="url(#g)"/></svg>`)
	newElt := func(tag string, attrs map[string]string) *html.Node {
		return &html.Node{Type: html.ElementNode, TagName: tag, Attributes: attrs}
	}
	grad := newElt("lineargradient", map[string]string{"id": "g"})
	grad.AddChild(newElt("stop", map[string]string{"stop-color": "blue"}))
	svgElt := findTag(doc.Root, "svg")
	svgElt.InsertBefore(grad, svgElt.Children[0])
	img, _ := render.RenderDocument(t, doc, 120, 120)
	wantPixel(t, img, 50, 50, color.RGBA{0, 0, 255, 255}, "fill:url(#g), a script-built gradient")
}

// getElementById does not look into <template> content (TreeScope's id map
// holds the tree's elements only): an inert #inv in a template does not
// shadow the real one after it.
func TestReferenceFilter_TemplateContentIsInert(t *testing.T) {
	const inert = `<template><svg><filter id="inv"><feFlood flood-color="red"/></filter></svg></template>`
	img := render.RenderPage(t, backdropPage("transparent", "background:green;filter:url(#inv)", inert+hiddenSVGInvertFilter("")), 120, 120)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "the real hidden <filter>, not the template's")
}

// An iframe document whose root is a 3D rendering-context root paints its
// layers through the 3D path; its url() references still resolve in its
// own document, not the outer page's.
func TestReferenceFilter_PerDocumentUnder3DRoot(t *testing.T) {
	const outer = `<svg width="0" height="0"><filter id="inv"><feFlood flood-color="red"/></filter></svg>`
	frame := `<iframe style="position:absolute;left:0;top:0;width:100px;height:100px;border:0" srcdoc='<html style="transform-style:preserve-3d;transform:translateZ(0)"><style>body{margin:0}</style>` +
		hiddenSVGInvertFilter("") + `<div style="width:100px;height:100px;background:green;filter:url(#inv)"></div></html>'></iframe>`
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>`+outer+frame, 120, 120)
	wantPixelWithinOne(t, img, 50, 50, invertedGreen, "the iframe document's own hidden <filter>")
}

// A <filter> under an element with no layout object (<metadata>, SVGElement::
// LayoutObjectIsNeeded false) or under a shape (whose layout object takes no
// children) has none itself: it runs in sRGB, auto.
func TestReferenceFilter_NoLayoutObjectUnderNonContainer(t *testing.T) {
	for _, tc := range []struct{ name, svg string }{
		{"metadata", `<svg width="0" height="0"><metadata><filter id="inv">` + invertMatrix + `</filter></metadata></svg>`},
		{"rect", `<svg width="0" height="0"><rect><filter id="inv">` + invertMatrix + `</filter></rect></svg>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := render.RenderPage(t, backdropPage("transparent", "background:green;filter:url(#inv)", tc.svg), 120, 120)
			wantPixelWithinOne(t, img, 50, 50, invertedGreen, "green through the filter, in sRGB")
		})
	}
}

// A primitive of a filter with no layout object takes its own
// color-interpolation-filters attribute, else the filter's
// (ColorInterpolationForElement, svg_filter_builder.cc:136-155, 189-192 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a linearRGB matrix in a hidden
// filter runs in linearRGB.
func TestReferenceFilter_PrimitiveInterpolationSpace(t *testing.T) {
	svgFilter := `<svg style="display:none"><filter id="inv"><feColorMatrix color-interpolation-filters="linearRGB" type="matrix" values="` + invertMatrixValues + `"/></filter></svg>`
	img := render.RenderPage(t, backdropPage("transparent", "background:green;filter:url(#inv)", svgFilter), 120, 120)
	wantPixelWithinOne(t, img, 50, 50, invertedGreenLinear, "the primitive's own linearRGB")
}

// An SVG shape's filter in an external document has no layout object, as on
// the CSS filter path: its color-interpolation-filters attribute applies,
// else auto, sRGB (ColorInterpolationForElement and
// ResolveInterpolationSpace, svg_filter_builder.cc:136-155 and 157-162 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b — "used by external SVG files"). The inverting matrix turns the green rect into (255,127,255)
// either way (svg-external-filter-resource).
func TestReferenceFilter_ExternalOnSVGShape(t *testing.T) {
	for _, attrs := range []string{`color-interpolation-filters="sRGB"`, ""} {
		external := `<svg xmlns="http://www.w3.org/2000/svg"><filter id="inv" ` + attrs + `>` + invertMatrix + `</filter></svg>`
		img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect width="100" height="100" fill="green" filter="url(ext.svg#inv)"/></svg>`, 120, 120,
			render.WithExternalSVGFetcher(func(string) ([]byte, error) { return []byte(external), nil }))
		wantPixelWithinOne(t, img, 50, 50, invertedGreen, "green through the external inverting filter, in sRGB ("+attrs+")")
	}
}
