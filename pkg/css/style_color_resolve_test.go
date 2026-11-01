package css

import "testing"

// A color-mix() that references currentColor resolves at use time against
// the element's color, per StyleColor::Resolve → UnresolvedColorMix::Resolve
// (style_color.cc:463, :297 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
const currentColorMix = "color-mix(in srgb, transparent 0%, currentColor 100%)"

var green = Color{R: 0, G: 128, B: 0, A: 1}

// WPT css-backgrounds/color-mix-currentcolor-outline-repaint(-parent).
func TestOutlineColorResolvesCurrentColorInColorMix(t *testing.T) {
	s := ParseInlineStyle("color: green; outline: 5px solid "+currentColorMix, nil)
	r, g, b, a := s.GetOutlineColor()
	if got := (Color{R: r, G: g, B: b, A: a}); got != green {
		t.Errorf("outline color = %+v, want %+v", got, green)
	}
}

// The other currentColor-defaulting color getters share the resolution.
func TestColorGettersResolveCurrentColorInColorMix(t *testing.T) {
	s := ParseInlineStyle("color: green; column-rule-color: "+currentColorMix+
		"; text-emphasis-color: "+currentColorMix+"; text-decoration-color: "+currentColorMix, nil)
	if got := s.GetColumnRuleColor(); got != green {
		t.Errorf("column-rule-color = %+v, want %+v", got, green)
	}
	if got := s.GetTextEmphasisColor(); got != green {
		t.Errorf("text-emphasis-color = %+v, want %+v", got, green)
	}
	if got, ok := s.GetTextDecorationColor(); !ok || got != green {
		t.Errorf("text-decoration-color = %+v (ok=%v), want %+v", got, ok, green)
	}
	if got := s.VisitedDependentColor("border-top-color"); got != green {
		t.Errorf("unset border-top-color = %+v, want currentColor %+v", got, green)
	}
	s.Set("border-top-color", currentColorMix)
	if got := s.VisitedDependentColor("border-top-color"); got != green {
		t.Errorf("border-top-color = %+v, want %+v", got, green)
	}
}
