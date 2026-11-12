package css

import "testing"

// TestUserAgentSVGOverflow checks svg.css's overflow rules
// (core/css/svg.css:48-54 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// svg:not(:root), symbol, image, marker, pattern and foreignObject get
// overflow: hidden, and svg:not(:root) overflow-clip-margin: content-box,
// unless the author sets them (LOU-878).
func TestUserAgentSVGOverflow(t *testing.T) {
	styles := cascadeHTML(t, `<style>#auth { overflow: visible }</style>
	<div><svg id="s"><symbol id="sym"/><image id="img"/><marker id="mk"/>
	<pattern id="pt"/><foreignObject id="fo"/><rect id="r"/>
	<svg id="nested"/></svg><svg id="auth"/></div>`, nil)
	for _, tt := range []struct{ id, overflow, clipMargin string }{
		{"s", "hidden", "content-box"},
		{"nested", "hidden", "content-box"},
		{"sym", "hidden", ""},
		{"img", "hidden", ""},
		{"mk", "hidden", ""},
		{"pt", "hidden", ""},
		{"fo", "hidden", ""},
		{"r", "", ""},
		{"auth", "visible", "content-box"},
	} {
		s := styleByID(t, styles, tt.id)
		if got, _ := s.Get("overflow"); got != tt.overflow {
			t.Errorf("#%s overflow = %q, want %q", tt.id, got, tt.overflow)
		}
		if got, _ := s.Get("overflow-clip-margin"); got != tt.clipMargin {
			t.Errorf("#%s overflow-clip-margin = %q, want %q", tt.id, got, tt.clipMargin)
		}
	}
}
