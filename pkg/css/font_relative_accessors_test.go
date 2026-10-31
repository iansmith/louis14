package css

import (
	"testing"
)

// TestFontRelativeAccessors (D5, folded in from LOU-386): each Style length
// accessor resolves em / rem / vw against the style's own conversion context
// — font-size 20px, a 10px root, an 800x600 viewport — instead of em = 16,
// rem = 16 and a 0x0 viewport. Blink resolves every <length> once, at style
// build, through CSSToLengthConversionData
// (StyleResolverState::UnzoomedLengthConversionData,
// core/css/resolver/style_resolver_state.cc:242-262 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestFontRelativeAccessors(t *testing.T) {
	for _, tc := range []struct {
		name, decl string
		get        func(*Style) float64
		want       float64
	}{
		{"GetOutlineWidth/em", "outline-style: solid; outline-width: 1em", (*Style).GetOutlineWidth, 20},
		{"GetOutlineWidth/vw", "outline-style: solid; outline-width: 1vw", (*Style).GetOutlineWidth, 8},
		{"GetTextIndent", "text-indent: 2em", func(s *Style) float64 { v, _ := s.GetTextIndent(); return v }, 40},
		{"GetVerticalAlignOffset", "vertical-align: 1rem", (*Style).GetVerticalAlignOffset, 10},
		{"GetBorderSpacing", "border-spacing: 1em 2em", (*Style).GetBorderSpacing, 20},
		{"GetBorderSpacingV/two", "border-spacing: 1em 2em", (*Style).GetBorderSpacingV, 40},
		{"GetBorderSpacingV/one", "border-spacing: 1em", (*Style).GetBorderSpacingV, 20},
		{"GetBackgroundSize/width", "background-size: 1em 2vw", func(s *Style) float64 { return s.GetBackgroundSize().Width }, 20},
		{"GetBackgroundSize/height", "background-size: 1em 2vw", func(s *Style) float64 { return s.GetBackgroundSize().Height }, 16},
		{"GetClipRect/top", "clip: rect(1em, 2em, 3em, 1vw)", func(s *Style) float64 { return clipOf(t, s).Top }, 20},
		{"GetClipRect/right", "clip: rect(1em, 2em, 3em, 1vw)", func(s *Style) float64 { return clipOf(t, s).Right }, 40},
		{"GetClipRect/bottom", "clip: rect(1em, 2em, 3em, 1vw)", func(s *Style) float64 { return clipOf(t, s).Bottom }, 60},
		{"GetClipRect/left", "clip: rect(1em, 2em, 3em, 1vw)", func(s *Style) float64 { return clipOf(t, s).Left }, 8},
		{"GetTabSize", "tab-size: 1em", func(s *Style) float64 {
			v, isLength := s.GetTabSize()
			if !isLength {
				t.Errorf("tab-size: 1em is not a length")
			}
			return v
		}, 20},
		{"GetTextDecorationThickness/rem", "text-decoration-thickness: 1rem", (*Style).GetTextDecorationThickness, 10},
		{"GetTextDecorationThickness/vw", "text-decoration-thickness: 0.5vw", (*Style).GetTextDecorationThickness, 4},
		{"GetTextUnderlineOffset/vw", "text-underline-offset: 1vw", (*Style).GetTextUnderlineOffset, 8},
		{"GetTextUnderlineOffset/calc", "text-underline-offset: calc(1vw + 10%)", (*Style).GetTextUnderlineOffset, 10},
		{"GetTextUnderlineOffset/percent", "text-underline-offset: 50%", (*Style).GetTextUnderlineOffset, 10},
		{"GetTextDecorationThicknessResolved/vw", "text-decoration-thickness: 1vw",
			func(s *Style) float64 { return s.GetTextDecorationThicknessResolved().Value }, 8},
		{"GetTextDecorationThicknessResolved/calc", "text-decoration-thickness: calc(1vw + 10%)",
			func(s *Style) float64 { return s.GetTextDecorationThicknessResolved().Value }, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := cascadeHTML(t, `<style>:root { font-size: 10px; } #d { font-size: 20px; `+tc.decl+`; }</style>
				<div id="d"></div>`, nil)
			if got := tc.get(styleByID(t, styles, "d")); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.decl, got, tc.want)
			}
		})
	}
}

// clipOf returns s's clip rect, failing if clip does not parse.
func clipOf(t *testing.T, s *Style) *ClipRect {
	t.Helper()
	cr := s.GetClipRect()
	if cr == nil {
		t.Fatalf("clip did not parse")
	}
	return cr
}

// TestOutlineWidth_InheritsComputedLength: outline-width computes to an
// absolute length at style build, so `outline-width: inherit` copies the
// parent's px rather than re-resolving 1em against the child's font. Blink's
// outline-width converter is ConvertBorderWidth, which computes the length
// through CssToLengthConversionData (core/css/resolver/style_builder_converter.cc
// :1974-2002; core/css/css_properties.json5:4922-4930 @
// 5992657f556bcd9d207a4aca187488462f027cab). WPT css-ui/outline-008.
func TestOutlineWidth_InheritsComputedLength(t *testing.T) {
	styles := cascadeHTML(t, `<style>#p { font-size: 10px; outline: solid 1em transparent; }
		#c { font-size: 5px; outline-width: inherit; outline-style: solid; }</style>
		<div id="p"><div id="c"></div></div>`, nil)
	if got := styleByID(t, styles, "c").GetOutlineWidth(); got != 10 {
		t.Errorf("outline-width: inherit of the parent's 1em at 10px = %v, want 10", got)
	}
}

// TestTabSize_InheritsComputedLength: tab-size is inherited and computes a
// length to px at style build, so a 32px child of a 16px `tab-size: 2em`
// parent inherits 32px rather than re-resolving 2em against its own font.
// Blink's converter is ConvertLengthOrTabSpaces, which computes the length
// through CssToLengthConversionData and keeps a bare number as a space count
// (core/css/resolver/style_builder_converter.cc:2182-2194;
// core/css/css_properties.json5 tab-size `inherited: true` @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestTabSize_InheritsComputedLength(t *testing.T) {
	styles := cascadeHTML(t, `<style>#p { font-size: 16px; tab-size: 2em; }
		#c { font-size: 32px; }
		#n { tab-size: 4; } #nc { font-size: 32px; }</style>
		<pre id="p"><span id="c"></span></pre><pre id="n"><span id="nc"></span></pre>`, nil)
	if got, isLength := styleByID(t, styles, "c").GetTabSize(); got != 32 || !isLength {
		t.Errorf("inherited tab-size of the parent's 2em at 16px = (%v, %v), want (32, true)", got, isLength)
	}
	if got, isLength := styleByID(t, styles, "nc").GetTabSize(); got != 4 || isLength {
		t.Errorf("inherited tab-size: 4 = (%v, %v), want (4, false) spaces", got, isLength)
	}
}
