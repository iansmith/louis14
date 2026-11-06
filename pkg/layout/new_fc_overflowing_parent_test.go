package layout

import "testing"

// A new-formatting-context child's layout opportunity spans its parent's
// content box; only floats narrow it, not the BFC root's own edge
// (ExclusionSpace::FindLayoutOpportunity bounded by the child available
// inline size, layout/exclusions/exclusion_space.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So when the parent overflows
// its BFC root, an orthogonal margin:auto child still centres in the
// parent's whole content box: 100px border + (600 - 100) / 2 = 350.
// Mirrors WPT css-contain/contain-body-t-o-001/002, whose body overflows
// html.
func TestNewFCChildOfOverflowingParentCentresInParent(t *testing.T) {
	for _, tc := range []struct {
		height string
		want   float64
	}{{"600px", 350}, {"500px", 300}, {"400px", 250}} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="writing-mode:vertical-lr; height:600px; width:300px; display:flow-root">`+
			`<div style="height:`+tc.height+`; border-top:100px solid; border-bottom:100px solid">`+
			`<div id=p style="writing-mode:horizontal-tb; margin:auto; height:100px; width:50px"></div></div></div></body>`, nil)
		if p := findBox(fl.root, byID("p")); p == nil || p.Y != tc.want {
			t.Errorf("parent content %s: child %s, want top %v", tc.height, boxDims(p), tc.want)
		}
	}
}
