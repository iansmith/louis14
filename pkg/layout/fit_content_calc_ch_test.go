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

// TestTableLengths_UseElementConversionData (LOU-454): border-spacing and
// collapsed border widths resolve through the table/cell style's conversion
// data, so ch is the font's "0" advance (Ahem: 1em), not 0.5em.
func TestTableLengths_UseElementConversionData(t *testing.T) {
	s := css.NewStyle()
	s.Set("font-size", "20px")
	s.SetZeroWidth(20)
	s.Set("border-spacing", "1ch 2ch")
	s.Set("border-top-style", "solid")
	s.Set("border-top-width", "1ch")
	tla := &TableLayoutAlgorithm{style: s}
	if i, b := tla.logicalBorderSpacing(); i != 20 || b != 40 {
		t.Errorf("border-spacing 1ch 2ch = %v, %v; want 20, 40", i, b)
	}
	if got := readBorderEdge(s, "top").width; got != 20 {
		t.Errorf("border-top-width 1ch = %v, want 20", got)
	}
}

// TestFitContentArg_MaxWithPercent (LOU-454): fit-content(max(10%, 2ch))
// resolves its percentage against the containing block like calc() does.
func TestFitContentArg_MaxWithPercent(t *testing.T) {
	s := css.NewStyle()
	s.Set("font-size", "20px")
	s.SetZeroWidth(20)
	got, ok := resolveFitContentArgInline("max(50%, 2ch)", s, 100)
	if !ok || got != 50 {
		t.Errorf("fit-content(max(50%%, 2ch)) of 100 = %v, %v; want 50, true", got, ok)
	}
}
