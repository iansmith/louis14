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

// The paint pass's external SVG cache builds each external filter once:
// the same document, fragment and viewport give the same filter.
func TestExternalSVGCache_BuildsFilterOnce(t *testing.T) {
	fetches := 0
	c := newExternalSVGCache(func(string) ([]byte, error) {
		fetches++
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg"><filter id="f"><feFlood/></filter></svg>`), nil
	})
	lc := svg.NewSVGLengthContext(geometry.SizeF{Width: 100, Height: 100})
	a, b := c.filter("a.svg", "f", lc), c.filter("a.svg", "f", lc)
	if a == nil || a != b || fetches != 1 {
		t.Errorf("filters %p, %p after %d fetches, want one filter from one fetch", a, b, fetches)
	}
}
