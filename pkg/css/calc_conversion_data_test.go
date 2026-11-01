package css

import (
	"testing"

	"louis14/pkg/html"
)

// ahemStyle20 returns a 20px style carrying Ahem's measured font metrics, as
// Style.UpdateFont would store them: "0" advance 1em (ZeroWidth 20), x-height
// and cap-height 0.8em (16). Ahem has no 水 glyph, so the ic advance is set to
// 1.5em (30) to stand for a CJK-capable font; every value differs from the
// unmeasured fallbacks (ch 0.5em, ex 0.5em, cap 0.7em, ic 1em).
func ahemStyle20() *Style {
	s := NewStyle()
	s.Set("font-size", "20px")
	s.SetZeroWidth(20)
	s.XHeight = 16
	s.CapHeight = 16
	s.IcWidth, s.HasIdeographicInlineSize = 30, true
	return s
}

// fontRelativeTerms are 2<unit> terms and their px value against ahemStyle20.
var fontRelativeTerms = []struct {
	term string
	want float64
}{
	{"2ch", 40},
	{"2ex", 32},
	{"2cap", 32},
	{"2ic", 60},
}

// TestCalcWithPercent_UsesElementConversionData (LOU-454): every calc() that
// carries a percentage resolves its font-relative terms through the element's
// conversion data, as Blink resolves every length through one
// CSSToLengthConversionData whose FontSizes::Ch / Ex / Cap / Ic read the
// element's font (core/css/css_to_length_conversion_data.cc:95-176 @
// 5992657f556bcd9d207a4aca187488462f027cab) — there is no context-specific ch
// constant. Covers the calc-with-percent path (text-indent), the intrinsic
// path (text-indent with % → 0) and the margin / padding paths.
func TestCalcWithPercent_UsesElementConversionData(t *testing.T) {
	for _, tc := range fontRelativeTerms {
		calc := "calc(10% + " + tc.term + ")"

		s := ahemStyle20()
		s.Set("text-indent", calc)
		if got, want := s.ResolveTextIndent(100), 10+tc.want; got != want {
			t.Errorf("text-indent %s of 100: ResolveTextIndent = %v, want %v", calc, got, want)
		}
		if got := s.ResolveTextIndentIntrinsic(); got != tc.want {
			t.Errorf("text-indent %s: ResolveTextIndentIntrinsic = %v, want %v", calc, got, tc.want)
		}

		s = ahemStyle20()
		s.Set("margin-left", calc)
		s.Set("padding-left", calc)
		if got, want := s.GetAllMarginsForWidth(100).Left, 10+tc.want; got != want {
			t.Errorf("margin-left %s of 100 = %v, want %v", calc, got, want)
		}
		if got, want := s.GetMarginForWidth(100).Left, 10+tc.want; got != want {
			t.Errorf("inline margin-left %s of 100 = %v, want %v", calc, got, want)
		}
		if got, want := s.GetPaddingForWidth(100).Left, 10+tc.want; got != want {
			t.Errorf("padding-left %s of 100 = %v, want %v", calc, got, want)
		}
	}
}

// TestRootFontSize_ResolvesAgainstInitialStyleFont (LOU-454): the root
// element's font-size resolves against its parent style, which for the root is
// the initial style with the document's initial font — StyleResolver sets
// InitialStyleForElement() as the parent when there is none
// (core/css/resolver/style_resolver.cc:1995-2000), built by
// InitialStyleBuilderForElement → FontBuilder::CreateInitialFont (:2612-2652),
// and FontSizeConversionData reads ParentStyle()'s font
// (core/css/resolver/style_resolver_state.cc:264-266), all @
// 5992657f556bcd9d207a4aca187488462f027cab. So ch / ex / cap / ic there come
// from the initial font's metrics, not fixed em fractions. fakeFontSelector's
// 16px initial font: "0" 0.6em, x-height 0.4em, cap-height 0.8em, 水 0.9em.
func TestRootFontSize_ResolvesAgainstInitialStyleFont(t *testing.T) {
	for _, tc := range []struct {
		fontSize string
		want     float64
	}{
		{"10ch", 96},
		{"2ex", 12.8},
		{"2cap", 25.6},
		{"2ic", 28.8},
		{"calc(1em + 1ch)", 25.6},
	} {
		doc, _ := html.Parse(`<html style="font-size: ` + tc.fontSize + `"><body>x</body></html>`)
		styles := ApplyStylesToDocumentWithFontSelector(doc, 800, 600, fakeFontSelector{})
		if got := styleByTag(t, styles, "html").GetFontSize(); !closeTo(got, tc.want) {
			t.Errorf("root font-size %s = %v, want %v", tc.fontSize, got, tc.want)
		}
	}
}

func closeTo(a, b float64) bool {
	d := a - b
	return d < 1e-4 && d > -1e-4
}

// TestStyleHolderLengths_UseElementConversionData (LOU-454): text-shadow and
// background-position lengths (plain and inside calc()) read the element's
// font metrics, not the selectorless initial-font fallbacks. Blink resolves
// both through the element's CSSToLengthConversionData (FontSizes::Ch,
// core/css/css_to_length_conversion_data.cc:117-126 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestStyleHolderLengths_UseElementConversionData(t *testing.T) {
	s := ahemStyle20()
	s.Set("text-shadow", "2ch 2ex black")
	if sh := s.GetTextShadow(); len(sh) != 1 || sh[0].OffsetX != 40 || sh[0].OffsetY != 32 {
		t.Errorf("text-shadow 2ch 2ex = %+v, want offsets 40, 32", sh)
	}

	for _, tc := range []struct {
		pos      string
		wantX    float64
		wantXOff float64
	}{
		{"2ch 0", 40, 0},
		{"calc(10% + 2ch) 0", -10, 40},
	} {
		s := ahemStyle20()
		s.Set("background-position", tc.pos)
		got := s.GetBackgroundPosition()
		if got.X != tc.wantX || got.XOffset != tc.wantXOff {
			t.Errorf("background-position %s: X=%v XOffset=%v, want %v, %v", tc.pos, got.X, got.XOffset, tc.wantX, tc.wantXOff)
		}
		s.Set("background-image", "url(a.png)")
		if l := s.GetBackgroundLayers(); l == nil || l.Position.X != tc.wantX || l.Position.XOffset != tc.wantXOff {
			t.Errorf("background layer position %s = %+v, want X=%v XOffset=%v", tc.pos, l, tc.wantX, tc.wantXOff)
		}
	}
}

// TestMathFunctionWithPercent_MinMaxClampLikeCalc (LOU-454): min() / max() /
// clamp() carrying a percentage resolve against the percentage base exactly
// as calc() does — Blink treats every CSSMathFunctionValue alike, evaluating
// the CalculationValue with the base at use time
// (core/css/css_math_function_value.cc ToCalcValue,
// platform/geometry/calculation_value.cc Evaluate @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestMathFunctionWithPercent_MinMaxClampLikeCalc(t *testing.T) {
	const v = "max(10%, 2ch)" // 40 against Ahem 20px and a base of 100
	s := ahemStyle20()
	s.Set("margin-left", v)
	s.Set("padding-left", v)
	s.Set("text-indent", "min(10%, 2ch)")
	if got := s.GetAllMarginsForWidth(100).Left; got != 40 {
		t.Errorf("margin-left %s of 100 = %v, want 40", v, got)
	}
	if got := s.GetMarginForWidth(100).Left; got != 40 {
		t.Errorf("inline margin-left %s of 100 = %v, want 40", v, got)
	}
	if got := s.GetPaddingForWidth(100).Left; got != 40 {
		t.Errorf("padding-left %s of 100 = %v, want 40", v, got)
	}
	if got := s.ResolveTextIndent(1000); got != 40 {
		t.Errorf("text-indent min(10%%, 2ch) of 1000 = %v, want 40", got)
	}
	if got := s.ResolveTextIndentIntrinsic(); got != 0 {
		t.Errorf("text-indent min(10%%, 2ch) intrinsic = %v, want 0 (%% → 0)", got)
	}
	s.Set("flex-basis", "clamp(10%, 2ch, 50%)")
	if fbv := s.GetFlexBasisValue(); !fbv.IsCalc {
		t.Errorf("flex-basis clamp(10%%, 2ch, 50%%) = %+v, want a deferred math value (IsCalc)", fbv)
	}
}
