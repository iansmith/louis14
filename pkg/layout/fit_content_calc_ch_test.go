package layout

import (
	"testing"

	"louis14/pkg/css"
)

// TestResolveFitContentArgInline_CalcWithPercentUsesElementCh (LOU-454): a
// fit-content(calc(% + ch)) argument resolves its ch term against the
// element's font ("0" advance), not a fixed 0.5em. Blink resolves the calc
// length through the element's CSSToLengthConversionData, whose FontSizes::Ch
// reads PrimaryFontWithDigitZero()->GetFontMetrics().ZeroWidth()
// (core/css/css_to_length_conversion_data.cc:117-126 @
// 5992657f556bcd9d207a4aca187488462f027cab). Ahem's "0" is 1em.
func TestResolveFitContentArgInline_CalcWithPercentUsesElementCh(t *testing.T) {
	s := css.NewStyle()
	s.Set("font-size", "20px")
	s.SetZeroWidth(20)
	got, ok := resolveFitContentArgInline("calc(10% + 2ch)", s, 100)
	if !ok || got != 50 {
		t.Errorf("fit-content(calc(10%% + 2ch)) of 100 = %v, %v; want 50, true", got, ok)
	}
}
