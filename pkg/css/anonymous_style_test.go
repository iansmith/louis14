package css

import (
	"reflect"
	"testing"
)

// anonymousBoxDisplays are the displays louis14 builds anonymous boxes with:
// anonymous block (block-in-inline, block/flex/grid/multicol inline runs),
// the CSS 2.1 §17.2.1 table wrappers, and block-ruby's inline ruby child.
var anonymousBoxDisplays = []DisplayType{
	DisplayBlock, DisplayTableCell, DisplayTableRow, DisplayTableRowGroup, DisplayRuby,
}

// TestCreateAnonymousStyleWithDisplay_CopiesParentUnicodeBidi (LOU-429):
// unicode-bidi is not inherited, but Blink's anonymous-style builder copies
// the parent's value onto every anonymous box
// (StyleResolver::CreateAnonymousStyleBuilderWithDisplay,
// builder.SetUnicodeBidi(parent_style.GetUnicodeBidi()),
// core/css/resolver/style_resolver.cc:3611 @
// 5992657f556bcd9d207a4aca187488462f027cab). A parent without the property
// leaves it absent, which is Blink's initial value (normal).
func TestCreateAnonymousStyleWithDisplay_CopiesParentUnicodeBidi(t *testing.T) {
	for _, display := range anonymousBoxDisplays {
		for _, bidi := range []string{"plaintext", "bidi-override", "isolate"} {
			parent := NewStyle()
			parent.Set("unicode-bidi", bidi)
			anon := CreateAnonymousStyleWithDisplay(parent, string(display))
			if got, ok := anon.Get("unicode-bidi"); !ok || got != bidi {
				t.Errorf("%s: unicode-bidi = %q (present %v), want parent's %q", display, got, ok, bidi)
			}
			if got := anon.GetDisplay(); got != display {
				t.Errorf("%s: display = %q", display, got)
			}
		}
		anon := CreateAnonymousStyleWithDisplay(NewStyle(), string(display))
		if got, ok := anon.Get("unicode-bidi"); ok {
			t.Errorf("%s: parent without unicode-bidi gave %q, want absent (initial normal)", display, got)
		}
	}
}

// TestCreateAnonymousStyleWithDisplay_CarriesAppliedTextDecorations (LOU-429):
// the anonymous box carries the parent's applied text-decoration vector, as
// Blink's builder does with
// builder.SetBaseTextDecorationData(parent_style.AppliedTextDecorationData())
// (style_resolver.cc:3612 @ 5992657f556bcd9d207a4aca187488462f027cab). The
// vector is copied, not aliased.
func TestCreateAnonymousStyleWithDisplay_CarriesAppliedTextDecorations(t *testing.T) {
	for _, display := range anonymousBoxDisplays {
		parent := NewStyle()
		parent.AppliedTextDecorations = []AppliedTextDecoration{{Lines: TextDecorationLineUnderline, Style: "solid"}}
		anon := CreateAnonymousStyleWithDisplay(parent, string(display))
		if !reflect.DeepEqual(anon.AppliedTextDecorations, parent.AppliedTextDecorations) {
			t.Errorf("%s: AppliedTextDecorations = %+v, want parent's %+v", display, anon.AppliedTextDecorations, parent.AppliedTextDecorations)
			continue
		}
		anon.AppliedTextDecorations[0].Lines = TextDecorationLineOverline
		if parent.AppliedTextDecorations[0].Lines != TextDecorationLineUnderline {
			t.Errorf("%s: anonymous style aliases the parent's decoration vector", display)
		}
	}
}

// TestCreateAnonymousStyleWithDisplay_InheritsCustomPropertiesAndVar
// (LOU-429): Blink starts the anonymous style from the parent's inherited
// values, custom properties included (ComputedStyleBuilder(InitialStyle(),
// parent_style), style_resolver.cc:3610 @
// 5992657f556bcd9d207a4aca187488462f027cab). louis14 copies the parent's
// declared inherited values verbatim together with every --*, so a var() on
// the anonymous style resolves exactly as it does on the parent, and an
// invalid-at-computed-value-time value stays invalid.
func TestCreateAnonymousStyleWithDisplay_InheritsCustomPropertiesAndVar(t *testing.T) {
	for _, display := range anonymousBoxDisplays {
		parent := NewStyle()
		parent.Set("--w", "10px")
		parent.Set("--c", "rgb(255, 0, 0)")
		parent.Set("color", "var(--c)")
		anon := CreateAnonymousStyleWithDisplay(parent, string(display))
		if got, ok := anon.Get("--w"); !ok || got != "10px" {
			t.Errorf("%s: --w = %q (present %v), want 10px", display, got, ok)
		}
		anon.Set("width", "var(--w)")
		if got, _ := anon.GetLength("width"); got != 10 {
			t.Errorf("%s: width: var(--w) = %v, want 10", display, got)
		}
		wantColor, _ := parent.Get("color")
		if got, ok := anon.Get("color"); !ok || got != wantColor {
			t.Errorf("%s: color = %q (present %v), want parent's %q", display, got, ok, wantColor)
		}
		if anon.Properties["color"] != parent.Properties["color"] {
			t.Errorf("%s: declared color = %q, want the parent's declared %q copied verbatim", display, anon.Properties["color"], parent.Properties["color"])
		}

		iacvt := NewStyle()
		iacvt.Set("color", "var(--missing)")
		if got, ok := CreateAnonymousStyleWithDisplay(iacvt, string(display)).Get("color"); ok {
			t.Errorf("%s: IACVT parent color gave %q on the anonymous style, want ok=false", display, got)
		}
	}
}

// TestNewBlockifiedStyle_InheritsCustomPropertiesBidiAndDecorations
// (LOU-429): the block-in-inline wrapper is an anonymous block built from the
// inline's style, so it gets the builder's custom properties, unicode-bidi and
// applied decorations, plus its own stacking-context copies.
func TestNewBlockifiedStyle_InheritsCustomPropertiesBidiAndDecorations(t *testing.T) {
	inline := NewStyle()
	inline.Set("display", "inline")
	inline.Set("--w", "10px")
	inline.Set("unicode-bidi", "isolate-override")
	inline.Set("opacity", "0.5")
	inline.AppliedTextDecorations = []AppliedTextDecoration{{Lines: TextDecorationLineUnderline, Style: "solid"}}

	s := NewBlockifiedStyle(inline)
	if got := s.GetDisplay(); got != DisplayBlock {
		t.Errorf("display = %q, want block", got)
	}
	if got, ok := s.Get("--w"); !ok || got != "10px" {
		t.Errorf("--w = %q (present %v), want 10px", got, ok)
	}
	s.Set("width", "var(--w)")
	if got, _ := s.GetLength("width"); got != 10 {
		t.Errorf("width: var(--w) = %v, want 10", got)
	}
	if got, ok := s.Get("unicode-bidi"); !ok || got != "isolate-override" {
		t.Errorf("unicode-bidi = %q (present %v), want isolate-override", got, ok)
	}
	if !reflect.DeepEqual(s.AppliedTextDecorations, inline.AppliedTextDecorations) {
		t.Errorf("AppliedTextDecorations = %+v, want the inline's %+v", s.AppliedTextDecorations, inline.AppliedTextDecorations)
	}
	if got, ok := s.Get("opacity"); !ok || got != "0.5" {
		t.Errorf("opacity = %q (present %v), want 0.5", got, ok)
	}
}
