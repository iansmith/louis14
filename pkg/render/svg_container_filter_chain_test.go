package render_test

import (
	"image/color"
	"strings"
	"testing"

	"louis14/pkg/render"
)

// LOU-785 (filter-effects worklist C2): an SVG container's CSS filter
// functions apply, as Blink builds the container's filter operations for
// shorthands and url() alike (SVGElementResourceClient::UpdateFilterData →
// FilterEffectBuilder::BuildFilterOperations, svg_resources.cc:371-422 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Each page is the end state of
// an svg-mutation-group-* reftest: a 50×100 green rect and its green drop
// shadow cover the red 100×100 square.
func TestSVGContainer_ShorthandFilter(t *testing.T) {
	for _, c := range []struct{ name, group, rect string }{
		{"position", `filter="drop-shadow(-50px 0 green)"`, `x="50" width="50" height="100"`},
		{"size", `filter="drop-shadow(50px 0 green)"`, `width="50" height="100"`},
		{"transform", `transform="translate(50, 0)" filter="drop-shadow(-50px 0 green)"`, `width="50" height="100"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg><rect width="100" height="100" fill="red"/>
<g `+c.group+`><rect `+c.rect+` fill="green"/></g></svg>`, 120, 120)
			for _, x := range []int{25, 75} {
				sampleColorClose(t, img, x, 50, 0, 128, 0, 2, "the rect and its shadow")
			}
		})
	}
}

// A container's url() filter region is resolved in the container's own
// transformed frame, where its children paint: under translate(50, 0) the
// group's rect at user x 0..50 is at device x 50..100, inside the region.
func TestSVGContainer_URLFilterUnderGroupTransform(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg><filter id="f" color-interpolation-filters="sRGB"><feOffset dx="0" dy="0"/></filter>
<rect x="50" width="50" height="100" fill="red"/>
<g transform="translate(50, 0)" filter="url(#f)"><rect width="50" height="100" fill="green"/></g></svg>`, 120, 120)
	sampleColorClose(t, img, 75, 50, 0, 128, 0, 2, "the translated group's rect")
}

// A url() stage of a container's chain resolves userSpaceOnUse coordinates
// in the container's user space and percentages against its <svg>'s
// viewport, as a lone url() and a shape's chain do (SVGViewportResolver,
// svg_resources.cc:375-381 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the
// flood's (0, 0, 10, 10) subregion is at the <svg>'s origin, not at the
// group's reference box.
func TestSVGContainer_ChainURLUserSpaceOnUseAnchorsAtUserOrigin(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green" x="0" y="0" width="10" height="10"/></filter>
<g style="filter:url(#f) blur(0px)"><rect x="50" y="50" width="20" height="20" fill="red"/></g></svg>`, 100, 100)
	wantPixel(t, img, 5, 5, canvasGreen, "the flood at the user-space origin")
	wantPixel(t, img, 55, 55, canvasWhite, "not at the group's reference box")
}

// A filtered child inside a filtered group composites in the group's source
// buffer, so its viewport clip is translated with the buffer: with the <svg>
// at (8, 200) on the page, the child's rect still shows.
func TestSVGContainer_NestedFilterOffPageOrigin(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><div style="height:192px"></div>
<svg width="100" height="100" style="display:block">
<filter id="f" color-interpolation-filters="sRGB"><feOffset dx="0" dy="0"/></filter>
<g style="filter:opacity(1)"><rect width="100" height="100" fill="green" filter="url(#f)"/></g></svg>`, 140, 320)
	wantPixel(t, img, 58, 250, canvasGreen, "the nested filtered rect")
}

// A shape whose fill box is empty still has a stroke bounding box, and a
// container counts it (HasValidBoundingBoxForContainer is !IsShapeEmpty,
// svg_content_container.cc:88-106, in ComputeStrokeBoundingBox, :254-266 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a group's drop-shadow of a
// horizontal <line> paints the shadow.
func TestSVGContainer_ChainOnHorizontalLine(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><g style="filter:drop-shadow(0 20px 0 green)">
<line x1="10" y1="20" x2="90" y2="20" stroke="green" stroke-width="10"/></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 20, canvasGreen, "the line")
	wantPixel(t, img, 50, 40, canvasGreen, "its shadow")
}

// A container's chain with a url() stage paints with an empty fill box, as
// its lone url() does (Blink installs the filter whatever the bbox):
// the userSpaceOnUse flood covers the filter region around a horizontal
// <line>.
func TestSVGContainer_ChainURLWithEmptyFillBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter>
<g style="filter:url(#f) blur(0px)"><line x1="10" y1="20" x2="90" y2="20" stroke="red" stroke-width="10"/></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 60, canvasGreen, "the flood")
}

// A container's lone url() keeps its document part: url(other.svg#f)
// resolves in other.svg even when the page has its own #f, as an SVG
// shape's does (resolveSVGFilterRef).
func TestSVGContainer_ExternalURLFilter(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100">
<filter id="f" color-interpolation-filters="sRGB"><feFlood flood-color="red"/></filter>
<g filter="url(other.svg#f)"><rect x="20" y="20" width="60" height="60" fill="blue"/></g></svg>`, 100, 100,
		fixedSVGFetcher(extGreenFlood, nil))
	wantPixel(t, img, 50, 50, canvasGreen, "the external filter's flood")
}

// A group whose stroke box is empty — its only child is a <text>, whose
// boxes louis14 leaves empty (LOU-795) — paints its children unfiltered
// rather than into a buffer a few pixels wide.
func TestSVGContainer_ChainWithEmptyStrokeBoxPaintsUnfiltered(t *testing.T) {
	for _, filter := range []string{"blur(1px)", "drop-shadow(2px 2px 0 green)", "url(#f) blur(1px)"} {
		img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="200" height="100"><filter id="f"><feOffset dx="0" dy="0"/></filter>
<g style="filter:`+filter+`"><text x="10" y="60" font-size="40" fill="green">TEXT</text></g></svg>`, 200, 100)
		if n := countPixels(img, func(c color.RGBA) bool { return c != canvasWhite }); n < 100 {
			t.Errorf("filter %q: %d painted pixels, want the text (at least 100)", filter, n)
		}
	}
}

// A filtered <g> in a <mask> is never laid out, yet its stroke box is
// known: its white rect still makes the mask opaque.
func TestSVGContainer_FilteredGroupInMask(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><mask id="m"><g style="filter:blur(1px)"><rect width="100" height="100" fill="white"/></g></mask>
<rect width="100" height="100" fill="green" mask="url(#m)"/></svg>`, 100, 100)
	wantPixel(t, img, 50, 50, canvasGreen, "the masked rect")
}

// transform-box: stroke-box on a <g> in a <mask> resolves against the
// group's stroke box though the mask content is never laid out: scale(0.5)
// about the box's centre leaves the mask's white at 25..75.
func TestSVGContainer_StrokeBoxTransformInMask(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><mask id="m"><g style="transform-box:stroke-box;transform-origin:center;transform:scale(0.5)"><rect width="100" height="100" fill="white"/></g></mask>
<rect width="100" height="100" fill="green" mask="url(#m)"/></svg>`, 100, 100)
	wantPixel(t, img, 50, 50, canvasGreen, "the mask's scaled white, at the centre")
	wantPixel(t, img, 10, 10, canvasWhite, "outside it")
}

// A nested <g> holding only a horizontal <line> counts toward its parent's
// stroke box, as Blink counts a container whose bounding box is valid
// (svg_content_container.cc:95-98 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the outer group's drop-shadow of it paints.
func TestSVGContainer_ChainOnNestedGroupOfLine(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><g style="filter:drop-shadow(0 20px 0 green)"><g>
<line x1="10" y1="20" x2="90" y2="20" stroke="green" stroke-width="10"/></g></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 40, canvasGreen, "the shadow")
}

// A url() and a CSS function in one filter value is a chain: its url() is
// fetched once, as the chain's stage, not first as a lone url() whose
// fragment runs to the last ')'.
func TestSVGShape_URLThenFunctionFetchesOnce(t *testing.T) {
	var fetches []string
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect x="20" y="20" width="60" height="60" fill="red" filter="url(other.svg#f) blur(0px)"/></svg>`, 100, 100,
		fixedSVGFetcher(extGreenFlood, &fetches))
	wantPixel(t, img, 50, 50, canvasGreen, "the external flood")
	if len(fetches) != 1 {
		t.Errorf("fetched the external document %d times, want 1", len(fetches))
	}
}

// A url() stage whose <filter> region is userSpaceOnUse does not depend on
// the element's box, so a group whose paint extent is empty (an unstroked
// horizontal line) still paints it, as Blink installs a filter on any
// element: the flood covers the region.
func TestSVGContainer_UserSpaceURLChainOnEmptyPaintBox(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><filter id="flood" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter>
<g style="filter:url(#flood) blur(1px)"><line x1="10" y1="50" x2="90" y2="50"/></g></svg>`, 100, 100)
	wantPixel(t, img, 50, 20, canvasGreen, "the flood")
}

// A backslash escapes a ')' or a quote inside a url() token: url(a\).svg#f)
// is one lone url() naming the document "a).svg".
func TestSVGShape_URLTokenEscapes(t *testing.T) {
	var docs []string
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect x="20" y="20" width="60" height="60" fill="red" filter="url(a\).svg#f)"/></svg>`, 100, 100,
		fixedSVGFetcher(extGreenFlood, &docs))
	wantPixel(t, img, 50, 50, canvasGreen, "the external flood")
	if len(docs) != 1 || !strings.HasSuffix(docs[0], "a).svg") {
		t.Errorf("fetched %q, want one fetch of a).svg", docs)
	}
}

// An external url() stage of a container's chain on an empty paint box is
// fetched once: the guard's look at its filterUnits and the chain share it.
func TestSVGContainer_EmptyPaintBoxExternalURLFetchesOnce(t *testing.T) {
	ext := `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f" filterUnits="userSpaceOnUse" x="0" y="0" width="100" height="100" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter></svg>`
	var fetches []string
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><g style="filter:url(other.svg#f) blur(1px)"><line x1="10" y1="50" x2="90" y2="50"/></g></svg>`, 100, 100,
		fixedSVGFetcher(ext, &fetches))
	wantPixel(t, img, 50, 20, canvasGreen, "the external flood")
	if len(fetches) != 1 {
		t.Errorf("fetched the external document %d times, want 1", len(fetches))
	}
}

// A filter value's url() decodes hex escapes: url(#\31 23) names #123.
func TestSVGShape_URLTokenHexEscape(t *testing.T) {
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><filter id="123" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter>
<rect x="20" y="20" width="60" height="60" fill="red" filter="url(#\31 23)"/></svg>`, 100, 100)
	wantPixel(t, img, 50, 50, canvasGreen, "the filter #123")
}

// A malformed url() (text outside its one string) is a bad-url token: no
// filter, and no fetch.
func TestSVGShape_BadURLTokenFetchesNothing(t *testing.T) {
	var fetches []string
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect x="20" y="20" width="60" height="60" fill="green" filter="url('a.svg#f' x)"/></svg>`, 100, 100,
		fixedSVGFetcher(extGreenFlood, &fetches))
	wantPixel(t, img, 50, 50, canvasGreen, "the unfiltered rect")
	if len(fetches) != 0 {
		t.Errorf("a bad url() fetched %q, want nothing", fetches)
	}
}

// extGreenFlood is an external SVG document whose #f floods green.
const extGreenFlood = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter></svg>`

// One paint pass fetches each external SVG document once, however many
// elements reference it.
func TestSVGShape_ExternalDocumentFetchedOncePerPaint(t *testing.T) {
	const ext = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter><filter id="g" color-interpolation-filters="sRGB"><feFlood flood-color="green"/></filter></svg>`
	var fetches []string
	img := render.RenderPage(t, `<!DOCTYPE html><style>body{margin:0}</style>
<svg width="100" height="100"><rect width="40" height="40" fill="red" filter="url(a.svg#f)"/>
<rect x="50" width="40" height="40" fill="red" filter="url(a.svg#g)"/>
<g filter="url(a.svg#f)"><rect y="50" width="40" height="40" fill="red"/></g></svg>`, 100, 100,
		fixedSVGFetcher(ext, &fetches))
	wantPixel(t, img, 20, 20, canvasGreen, "the first rect's filter")
	wantPixel(t, img, 70, 20, canvasGreen, "the second rect's filter")
	if len(fetches) != 1 {
		t.Errorf("fetched %q, want a.svg once", fetches)
	}
}
