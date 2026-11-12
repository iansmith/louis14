package css

import "testing"

// TestGridShorthandSetsItsSixLonghands checks the grid shorthand's three
// forms against Blink's Grid::ParseShorthand
// (core/css/properties/shorthands/shorthands_custom.cc:3773 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a <'grid-template'> sets the
// template longhands and resets the auto ones; an auto-flow form sets the
// auto longhands and resets the template ones it does not name (LOU-849).
func TestGridShorthandSetsItsSixLonghands(t *testing.T) {
	// Each case lists the longhands that differ from the reset values.
	resets := map[string]string{
		"grid-template-rows": "none", "grid-template-columns": "none",
		"grid-template-areas": "none", "grid-auto-flow": "row",
		"grid-auto-rows": "auto", "grid-auto-columns": "auto",
	}
	tests := []struct {
		decl string
		want map[string]string
	}{
		{"grid: repeat(3, 10px) / repeat(2, 10px)", map[string]string{
			"grid-template-rows": "repeat(3, 10px)", "grid-template-columns": "repeat(2, 10px)"}},
		{"grid: none", nil},
		{"grid: auto-flow dense 20px / 10px 30px", map[string]string{
			"grid-template-columns": "10px 30px", "grid-auto-flow": "dense", "grid-auto-rows": "20px"}},
		{"grid: auto-flow / 10px", map[string]string{"grid-template-columns": "10px"}},
		{"grid: 10px 20px / dense auto-flow 5px", map[string]string{
			"grid-template-rows": "10px 20px", "grid-auto-flow": "column dense", "grid-auto-columns": "5px"}},
		{"grid: 10px / auto-flow", map[string]string{
			"grid-template-rows": "10px", "grid-auto-flow": "column"}},
		{`grid: "a b" 10px "c d" / 1fr 1fr`, map[string]string{
			"grid-template-areas": `"a b" "c d"`, "grid-template-rows": "10px auto",
			"grid-template-columns": "1fr 1fr"}},
		{`grid: [p] "a" 10px [x] [y] "b"`, map[string]string{
			"grid-template-areas": `"a" "b"`, "grid-template-rows": "[p] 10px [x y] auto"}},
	}
	for _, tt := range tests {
		got := parseDeclarations(tt.decl).Declarations
		for lh, want := range resets {
			if w, ok := tt.want[lh]; ok {
				want = w
			}
			if got[lh] != want {
				t.Errorf("%s: %s = %q, want %q", tt.decl, lh, got[lh], want)
			}
		}
		if _, ok := got["grid"]; ok {
			t.Errorf("%s: the grid shorthand itself is stored: %v", tt.decl, got)
		}
	}
}
