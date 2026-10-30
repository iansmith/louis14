package layout

import (
	"strings"
	"testing"

	"louis14/pkg/html"
)

// TestLayout_LineHeightFontMetricUnitsUseCascadeFont (LOU-428): the layout
// engine hands the cascade its font selector, so an ex / ch / cap line-height
// computes to px against the element's real font (Ahem: x-height and
// cap-height 0.8em, "0" advance 1em) and a 40px child inherits that length.
// Blink: StyleResolverState::UpdateFont builds the font before line-height is
// converted (core/css/resolver/style_resolver_state.cc:553,
// core/css/resolver/style_cascade.cc:310-315 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestLayout_LineHeightFontMetricUnitsUseCascadeFont(t *testing.T) {
	for _, tc := range []struct {
		lineHeight string
		want       float64 // div: font 10px Ahem
	}{
		{"calc(1em + 1ex)", 18},
		{"calc(1em + 1ch)", 20},
		{"calc(1em + 1cap)", 18},
		{"2ex", 16},
	} {
		doc, _ := html.Parse(`<style>div { font: 10px Ahem; line-height: ` + tc.lineHeight +
			`; } span { font-size: 40px; }</style><div><span>x</span></div>`)
		le := NewLayoutEngine(800, 600)
		le.Layout(doc)
		for node, style := range le.ComputedStyles() {
			switch node.TagName {
			case "div":
				if got := style.GetLineHeight(); got != tc.want {
					t.Errorf("line-height %s: div = %v, want %v", tc.lineHeight, got, tc.want)
				}
				if raw := style.Properties["line-height"]; !strings.HasSuffix(raw, "px") {
					t.Errorf("line-height %s: div computed value %q, want a px length", tc.lineHeight, raw)
				}
			case "span":
				if got := style.GetLineHeight(); got != tc.want {
					t.Errorf("line-height %s: span = %v, want the div's %v", tc.lineHeight, got, tc.want)
				}
			}
		}
	}
}
