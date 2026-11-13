package css

import (
	"slices"
	"testing"
)

// A legacy alias names the property it is an alias for: the parser resolves
// it, so `grid-column-gap: 20px` sets column-gap (alias_for entries,
// third_party/blink/renderer/core/css/css_properties.json5:10927-10938 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). As
// the same property, an alias and its target cascade in source order.
func TestLegacyGapAliasesSetTheirTarget(t *testing.T) {
	for _, tc := range []struct{ decls, rowGap, columnGap string }{
		{`grid-column-gap: 20px`, "", "20px"},
		{`grid-row-gap: 7px`, "7px", ""},
		{`grid-gap: 3px 4px`, "3px", "4px"},
		{`column-gap: 1px; grid-column-gap: 20px`, "", "20px"},
		{`grid-column-gap: 20px; column-gap: 1px`, "", "1px"},
		{`row-gap: 1px; grid-row-gap: 7px`, "7px", ""},
		{`grid-row-gap: 7px; row-gap: 1px`, "1px", ""},
		{`row-gap: 1px; grid-gap: 3px 4px`, "3px", "4px"},
		// An invalid alias value drops, as its target's would.
		{`row-gap: 5px; grid-row-gap: -1px`, "5px", ""},
	} {
		t.Run(tc.decls, func(t *testing.T) {
			styles := cascadeHTML(t, `<style>#t { display: grid; `+tc.decls+` }</style><div id="t"></div>`, nil)
			s := styleByID(t, styles, "t")
			if got, _ := s.Get("row-gap"); got != tc.rowGap {
				t.Errorf("row-gap = %q, want %q", got, tc.rowGap)
			}
			if got, _ := s.Get("column-gap"); got != tc.columnGap {
				t.Errorf("column-gap = %q, want %q", got, tc.columnGap)
			}
		})
	}
}

// The CSSOM and @supports name an alias's target too: setting or removing
// grid-column-gap is setting or removing column-gap, and an alias is as
// supported as its target (alias_for, as above).
func TestAliasIsItsTargetInCSSOMAndSupports(t *testing.T) {
	d := parseDeclarations(`column-gap: 1px`)
	if !d.SetDeclaration("grid-column-gap", "20px") {
		t.Fatal("SetDeclaration(grid-column-gap) = false, want set")
	}
	if got, _ := d.Value("column-gap"); got != "20px" {
		t.Errorf("column-gap = %q after setting grid-column-gap, want 20px", got)
	}
	if got, _ := d.Value("grid-column-gap"); got != "20px" {
		t.Errorf("grid-column-gap = %q, want 20px", got)
	}
	d.RemoveDeclaration("grid-column-gap")
	if got, ok := d.Value("column-gap"); ok {
		t.Errorf("column-gap = %q after removing grid-column-gap, want none", got)
	}
	for _, name := range []string{"grid-column-gap", "grid-row-gap", "grid-gap", "-webkit-transform"} {
		if !isSupportedCSSProperty(name) {
			t.Errorf("isSupportedCSSProperty(%q) = false, want true", name)
		}
	}
}

// transition-property may name an alias; the transition is its target's.
// Blink keeps the alias ident in the value (it serializes as written) and
// resolves the property it names (ConsumeTransitionProperty,
// core/css/properties/css_parsing_utils.cc:9612-9636 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the resolution is where a
// transition is built, not in the parser.
func TestTransitionPropertyAliasIsItsTarget(t *testing.T) {
	s := NewStyle()
	s.Set("transition-property", "-webkit-transform, grid-row-gap")
	s.Set("transition-duration", "1s")
	var got []string
	for _, tr := range resolveTransitions(s) {
		got = append(got, tr.property)
	}
	if want := []string{"transform", "row-gap"}; !slices.Equal(got, want) {
		t.Errorf("transition properties = %q, want %q", got, want)
	}
}
