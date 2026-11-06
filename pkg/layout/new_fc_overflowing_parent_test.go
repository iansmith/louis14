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

// Floats and new-FC children in a parent whose content box does not match
// its BFC root's: the layout opportunity is the parent's content box, with
// only exclusions narrowing it, on both sides. An exclusion past the BFC
// root's edge still counts, and the BFC root's own edge never does
// (ExclusionSpace::FindLayoutOpportunity, layout/exclusions/
// exclusion_space.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFloatsInParentOverflowingBFC(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		wantA      [2]float64 // x, y
		wantB      [2]float64
	}{
		// Parent overflows the 300px BFC root by 100px. The first right
		// float sits at the parent's end, past the BFC root's end; the
		// second must stack inward, not overlap it.
		{"two right floats", `<div style="width:400px"><div id=a style="float:right;width:50px;height:50px"></div>` +
			`<div id=b style="float:right;width:50px;height:50px"></div></div>`, [2]float64{350, 0}, [2]float64{300, 0}},
		// A right float at [300,400] leaves [0,300]; a margin:auto new-FC
		// child centres there.
		{"right float and margin:auto new-FC", `<div style="width:400px"><div id=a style="float:right;width:100px;height:50px"></div>` +
			`<div id=b style="display:flow-root;width:100px;height:20px;margin:0 auto"></div></div>`, [2]float64{300, 0}, [2]float64{100, 0}},
		// Parent starts 100px before the BFC root's start: no start float,
		// so nothing narrows its start. The new-FC child centres in
		// [-100,300] and the left float sits at the parent's start.
		{"negative inline origin", `<div style="margin-left:-100px"><div id=a style="display:flow-root;width:100px;height:20px;margin:0 auto"></div>` +
			`<div id=b style="float:left;width:50px;height:50px"></div></div>`, [2]float64{50, 0}, [2]float64{-100, 20}},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:flow-root;width:300px">`+tc.html+`</div></body>`, nil)
		for _, w := range []struct {
			id   string
			want [2]float64
		}{{"a", tc.wantA}, {"b", tc.wantB}} {
			if b := findBox(fl.root, byID(w.id)); b == nil || b.X != w.want[0] || b.Y != w.want[1] {
				t.Errorf("%s: #%s %s, want at (%v,%v)", tc.name, w.id, boxDims(b), w.want[0], w.want[1])
			}
		}
	}
}
