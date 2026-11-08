package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/layout/svg"
)

// An element filter on a paint context without a renderer builds nothing,
// so the element paints unfiltered, rather than dereferencing one.
func TestBuildSVGElementFilter_NilRenderer(t *testing.T) {
	pctx := &svgPaintContext{viewport: geometry.NewRectF(0, 0, 100, 100), DeviceFromUser: geometry.Identity()}
	style := css.NewStyle()
	style.Set("filter", "url(a.svg#f) blur(1px)")
	box := geometry.NewRectF(0, 0, 10, 10)
	if _, handled := buildSVGElementFilter(pctx, style.GetFilter(), box, box, css.Color{}); handled {
		t.Error("built a filter without a renderer")
	}
}

// floodFilterDoc is an SVG document holding <filter id="f"> with one feFlood.
const floodFilterDoc = `<svg xmlns="http://www.w3.org/2000/svg"><filter id="f"><feFlood/></filter></svg>`

// The paint pass's external SVG cache builds each external filter once:
// the same document, fragment and viewport give the same filter.
func TestExternalSVGCache_BuildsFilterOnce(t *testing.T) {
	fetches := 0
	c := newExternalSVGCache(func(string) ([]byte, error) {
		fetches++
		return []byte(floodFilterDoc), nil
	})
	lc := svg.NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 100})
	a, b := c.filter("a.svg", "f", lc), c.filter("a.svg", "f", lc)
	if a == nil || a != b || fetches != 1 {
		t.Errorf("filters %p, %p after %d fetches, want one filter from one fetch", a, b, fetches)
	}
}

// A shape with an empty fill box under a lone same-document url() that
// resolves paints nothing, with or without a renderer: the lookup is a
// registry lookup and needs none (LOU-774 round 3).
func TestPaintWithFilter_EmptyBoxResolvesWithoutRenderer(t *testing.T) {
	root, err := svg.ParseExternalSVG([]byte(floodFilterDoc))
	if err != nil {
		t.Fatal(err)
	}
	elt, _ := svg.FindElementByID(root, "f")
	reg := svg.NewSVGResourceRegistry()
	reg.Register(svg.NewSVGResourceFilter(elt, svg.NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 100}), nil))
	sp := &svgShapePainter{
		ctx:   &svgPaintContext{viewport: geometry.NewRectF(0, 0, 100, 100), DeviceFromUser: geometry.Identity(), Resources: reg},
		shape: &svg.SVGShape{},
	}
	style := css.NewStyle()
	style.Set("filter", "url(#f)")
	if !sp.paintWithFilter(style.GetFilter()) {
		t.Error("an empty-box shape under a resolvable url(#f) painted unfiltered without a renderer; want nothing painted")
	}
}
