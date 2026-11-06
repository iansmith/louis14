package layout

import "testing"

// A size-contained multicol still takes its intrinsic inline size from the
// column formula: its column content counts as empty
// (CalculateMinMaxSizesIgnoringChildren, length_utils.cc:1929-1934), then
// column-width and column-count x width + gaps apply
// (ColumnLayoutAlgorithm::ComputeMinMaxSizes, column_layout_algorithm.cc:
// 433-495 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So a shrink-to-fit
// `columns: 2 40px; column-gap: 20px` box is 100px wide whatever its content.
// Mirrors WPT css-contain/contain-size-multicol-001/002.
func TestSizeContainedMulticolIntrinsicWidth(t *testing.T) {
	const content = `<div style="width:300px; height:30px"></div>`
	for _, pos := range []string{"position:absolute", "float:left"} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=m style="`+pos+
			`; contain:size; columns:2 40px; column-gap:20px; min-height:10px">`+content+`</div></body>`, nil)
		if m := findBox(fl.root, byID("m")); m == nil || m.Width != 100 {
			t.Errorf("%s size-contained multicol: %s, want 100 wide", pos, boxDims(m))
		}
	}
}

// The same contained-multicol intrinsic size reaches every entry point and
// every containment form (ColumnLayoutAlgorithm::ComputeMinMaxSizes,
// column_layout_algorithm.cc:433-495; CalculateMinMaxSizesIgnoringChildren
// seeds the column content with contain-intrinsic-inline-size,
// length_utils.cc:1904-1934 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestSizeContainedMulticolEntryPoints(t *testing.T) {
	const multicol = `columns:2 40px; column-gap:20px`
	cases := []struct {
		name, body string
		id         string
		want       float64
	}{
		{"inline-size containment", `<div id=m style="position:absolute; contain:inline-size; ` + multicol + `"><div style="width:300px; height:10px"></div></div>`, "m", 100},
		{"spanner skipped", `<div id=m style="position:absolute; contain:size; ` + multicol + `"><div style="column-span:all; width:500px; height:10px"></div></div>`, "m", 100},
		{"contain-intrinsic-width seeds the columns", `<div id=m style="position:absolute; contain:size; contain-intrinsic-width:50px; column-count:2; column-gap:20px"></div>`, "m", 120},
		{"min-width: max-content", `<div id=m style="contain:size; ` + multicol + `; width:10px; min-width:max-content"></div>`, "m", 100},
		// column-count 3, gap 20px: min-content = 0 x 3 + 2 x 20 = 40, the
		// flex item's automatic minimum.
		{"flex item content minimum", `<div style="display:flex; width:10px"><div id=m style="contain:size; column-count:3; column-gap:20px; min-height:5px"></div></div>`, "m", 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.body+`</body>`, nil)
			if b := findBox(fl.root, byID(tc.id)); b == nil || b.Width != tc.want {
				t.Errorf("%s: %s, want %v wide", tc.name, boxDims(b), tc.want)
			}
		})
	}
}

// The fit-content() contribution path measures a contained box's content
// as empty too: a size-contained child's min-width: fit-content(200px)
// clamps against content 0, so it contributes its 10px width and the
// shrink-to-fit parent is 10px wide.
func TestSizeContainedFitContentContribution(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div id=p style="position:absolute">`+
		`<div style="contain:size; width:10px; min-width:fit-content(200px); height:5px"><div style="width:300px"></div></div></div></body>`, nil)
	if p := findBox(fl.root, byID("p")); p == nil || p.Width != 10 {
		t.Errorf("parent of a contained fit-content child: %s, want 10 wide", boxDims(p))
	}
}
