package css

import "testing"

// TestChUnit_MeasuredZeroAdvance (LOU-414): a font whose "0" glyph has a zero
// advance gives ch = 0, not the 0.5em fallback. Only an unmeasured ch falls
// back. Blink: SimpleFontData::ZeroInlineSize uses the zero glyph's advance
// whenever the glyph exists (platform/fonts/simple_font_data.cc:561), and
// FontSizes::Ch returns it unchanged (core/css/css_to_length_conversion_data.cc:117),
// both @ 5992657f556bcd9d207a4aca187488462f027cab. WPT: css-values/ch-unit-016.
func TestChUnit_MeasuredZeroAdvance(t *testing.T) {
	newStyle := func() *Style {
		s := NewStyle()
		s.Set("font-size", "20px")
		s.Set("width", "calc(100px + 5ch)")
		return s
	}

	measured := newStyle()
	measured.SetZeroWidth(0)
	if got, _ := measured.GetLength("width"); got != 100 {
		t.Errorf("measured zero-advance ch: calc(100px + 5ch) = %v, want 100 (ch = 0)", got)
	}

	unmeasured := newStyle()
	if got, _ := unmeasured.GetLength("width"); got != 150 {
		t.Errorf("unmeasured ch: calc(100px + 5ch) = %v, want 150 (ch falls back to 0.5em = 10px)", got)
	}
}

// TestChUnit_AnonymousStylesKeepMeasuredZeroWidth (LOU-414): anonymous boxes
// copy the parent's measured "0" advance together with HasZeroWidth, so ch on
// an anonymous box uses the parent font's measurement, not the 0.5em fallback.
func TestChUnit_AnonymousStylesKeepMeasuredZeroWidth(t *testing.T) {
	parent := NewStyle()
	parent.Set("font-size", "20px")
	parent.SetZeroWidth(0)
	for name, newAnon := range map[string]func(*Style) *Style{
		"NewAnonymousBlockStyle":         NewAnonymousBlockStyle,
		"NewAnonymousTableCellStyle":     NewAnonymousTableCellStyle,
		"NewAnonymousTableRowStyle":      NewAnonymousTableRowStyle,
		"NewAnonymousInlineRubyStyle":    NewAnonymousInlineRubyStyle,
		"NewAnonymousTableRowGroupStyle": NewAnonymousTableRowGroupStyle,
	} {
		s := newAnon(parent)
		s.Set("width", "calc(100px + 5ch)")
		if got, _ := s.GetLength("width"); got != 100 {
			t.Errorf("%s: calc(100px + 5ch) = %v, want 100 (parent's measured ch = 0)", name, got)
		}
	}
}

// TestAnonymousStylesCopyStyleResolutionContext (LOU-428): every anonymous-box
// style carries its parent's full resolution context — all font metrics, the
// used font-size and the font selector — as Blink builds an anonymous style
// from the parent style, inheriting its Font
// (StyleResolver::CreateAnonymousStyleBuilderWithDisplay,
// core/css/resolver/style_resolver.cc:3607 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestAnonymousStylesCopyStyleResolutionContext(t *testing.T) {
	parent := NewStyle()
	parent.Set("font-size", "20px")
	parent.ViewportWidth, parent.ViewportHeight = 800, 600
	parent.SetZeroWidth(11)
	parent.UsedFontSize, parent.UsedFontSizeSet = 18, true
	parent.XHeight, parent.CapHeight, parent.LhSize, parent.IcWidth = 9, 14, 30, 19
	parent.BaseDir = "base/"
	parent.fontSelector = fakeFontSelector{}
	for name, newAnon := range map[string]func(*Style) *Style{
		"NewAnonymousBlockStyle":         NewAnonymousBlockStyle,
		"NewAnonymousTableCellStyle":     NewAnonymousTableCellStyle,
		"NewAnonymousTableRowStyle":      NewAnonymousTableRowStyle,
		"NewAnonymousInlineRubyStyle":    NewAnonymousInlineRubyStyle,
		"NewAnonymousTableRowGroupStyle": NewAnonymousTableRowGroupStyle,
	} {
		s := newAnon(parent)
		if s.ViewportWidth != 800 || s.ViewportHeight != 600 || s.ChWidth != 11 || !s.HasZeroWidth ||
			s.UsedFontSize != 18 || !s.UsedFontSizeSet || s.XHeight != 9 || s.CapHeight != 14 ||
			s.LhSize != 30 || s.IcWidth != 19 || s.BaseDir != "base/" || s.fontSelector != parent.fontSelector {
			t.Errorf("%s dropped resolution context: viewport %vx%v ch %v/%v used %v/%v ex %v cap %v lh %v ic %v base %q selector %v",
				name, s.ViewportWidth, s.ViewportHeight, s.ChWidth, s.HasZeroWidth, s.UsedFontSize, s.UsedFontSizeSet,
				s.XHeight, s.CapHeight, s.LhSize, s.IcWidth, s.BaseDir, s.fontSelector)
		}
	}
}
