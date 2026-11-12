package css

import "testing"

// TestViewportLogicalUnits checks vi / vb and their s/l/d variants: a
// percent of the viewport's inline or block size in the element's own
// writing mode, also for its font-size, whatever the declaration order.
// Blink: CSSLengthResolver::ViewportInlineSizePercent / BlockSizePercent
// (core/css/css_length_resolver.cc:19-25) with the writing mode of
// StyleResolverState::UnzoomedLengthConversionData
// (core/css/resolver/style_resolver_state.cc:219-242) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (LOU-862).
func TestViewportLogicalUnits(t *testing.T) {
	// The viewport is 800 wide and 600 tall.
	styles := cascadeHTML(t, `<style>
		#h { font-size: 10vi; width: 10vb }
		#v1 { writing-mode: vertical-lr; font-size: 10vi; width: 10vb }
		#v2 { font-size: 10vb; writing-mode: vertical-lr; width: 10svi }
		#vp { writing-mode: vertical-rl }
		#child { font-size: 10dvi; width: 10LVB }
		#back { writing-mode: horizontal-tb; font-size: 10vi; width: 10vb }
		#calc { writing-mode: vertical-lr; font-size: calc(5vi + 5vi); width: min(10vb, 1000px) }
	</style><body><div id="h"></div><div id="v1"></div><div id="v2"></div>
	<div id="vp"><div id="child"></div><div id="back"></div></div>
	<div id="calc"></div></body>`, nil)
	for _, tt := range []struct {
		id              string
		fontSize, width float64
	}{
		{"h", 80, 60},
		{"v1", 60, 80},
		{"v2", 80, 60},
		{"child", 60, 80},
		{"back", 80, 60},
		{"calc", 60, 80},
	} {
		t.Run(tt.id, func(t *testing.T) {
			s := styleByID(t, styles, tt.id)
			if got := s.GetFontSize(); got != tt.fontSize {
				t.Errorf("font-size = %v, want %v", got, tt.fontSize)
			}
			if got := lengthOf(t, s, "width"); got != tt.width {
				t.Errorf("width = %v, want %v", got, tt.width)
			}
		})
	}
}
