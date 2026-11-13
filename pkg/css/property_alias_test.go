package css

import "testing"

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
