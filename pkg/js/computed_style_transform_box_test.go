package js

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// getComputedStyle serializes transform-box from the computed style, so an
// unset property reads as its initial value view-box rather than ""
// (TransformBox::CSSValueFromComputedStyleInternal, css_properties.json5:6597
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). WPT
// css-transforms/transform-box/cssbox-initial and svgbox-initial print
// their error text when it does not.
func TestComputedStyle_TransformBox(t *testing.T) {
	n := &html.Node{}
	unset := css.NewStyle()
	c := &computedStyleAccessor{node: n, styles: map[*html.Node]*css.Style{n: unset}}
	if got := c.resolve("transform-box"); got != "view-box" {
		t.Errorf("unset transform-box resolved to %q, want view-box", got)
	}
	set := css.NewStyle()
	set.Set("transform-box", "fill-box")
	c.styles[n] = set
	if got := c.resolve("transform-box"); got != "fill-box" {
		t.Errorf("transform-box: fill-box resolved to %q, want fill-box", got)
	}
}
