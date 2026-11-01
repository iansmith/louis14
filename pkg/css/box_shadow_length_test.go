package css

import "testing"

// TestBoxShadowLengthsResolveAgainstElement: box-shadow offsets, blur and
// spread resolve with the element's conversion data (em against its own
// font-size), per StyleBuilderConverter::ConvertShadow
// (style_builder_converter.cc:2697-2712 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// WPT css-backgrounds/box-shadow-multiple-001, box-shadow-overlapping-001/-003.
func TestBoxShadowLengthsResolveAgainstElement(t *testing.T) {
	s := ParseInlineStyle("font-size: 50px; box-shadow: purple 1em -1em 0.5em 0.2em, inset 2em 0 blue", nil)
	got := s.GetBoxShadow()
	if len(got) != 2 {
		t.Fatalf("got %d shadows, want 2", len(got))
	}
	if g := got[0]; g.OffsetX != 50 || g.OffsetY != -50 || g.Blur != 25 || g.Spread != 10 {
		t.Errorf("shadow 0 = (%v, %v, blur %v, spread %v), want (50, -50, 25, 10)", g.OffsetX, g.OffsetY, g.Blur, g.Spread)
	}
	if g := got[1]; g.OffsetX != 100 || g.OffsetY != 0 || !g.Inset {
		t.Errorf("shadow 1 = (%v, %v, inset %v), want (100, 0, inset)", g.OffsetX, g.OffsetY, g.Inset)
	}
}
