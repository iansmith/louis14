package layout

import "testing"

// TestFloatClear_LogicalValuesUseContainingBlockDirection pins that float and
// clear's inline-start/inline-end resolve against the containing block's
// direction, not the element's own. Mirrors ComputedStyle::Floating and
// ComputedStyle::Clear (style/computed_style.h:1516-1530, :1605-1620 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called with the parent space's
// direction or the container's style (floats_utils.cc:48-50,
// block_layout_algorithm.cc:2336 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFloatClear_LogicalValuesUseContainingBlockDirection(t *testing.T) {
	const box = "width:100px; height:5px;"
	cases := []struct {
		name  string
		cb    string
		inner string
		wantX float64
		wantY float64
	}{
		{"float inline-start, rtl float in ltr block", "ltr",
			`<div id="t" style="float:inline-start; direction:rtl; ` + box + `"></div>`, 0, 0},
		{"float inline-end, rtl float in ltr block", "ltr",
			`<div id="t" style="float:inline-end; direction:rtl; ` + box + `"></div>`, 200, 0},
		{"float inline-start, ltr float in rtl block", "rtl",
			`<div id="t" style="float:inline-start; direction:ltr; ` + box + `"></div>`, 200, 0},
		{"float inline-start in an rtl inline in an ltr block", "ltr",
			`<span style="direction:rtl"><div id="t" style="float:inline-start; ` + box + `"></div></span>`, 0, 0},
		{"clear inline-start, rtl block in ltr block", "ltr",
			`<div style="float:left; ` + box + `"></div><div id="t" style="clear:inline-start; direction:rtl; ` + box + `"></div>`, 0, 5},
		{"clear inline-end, rtl block in ltr block, float on the other side", "ltr",
			`<div style="float:left; ` + box + `"></div><div id="t" style="clear:inline-end; direction:rtl; ` + box + `"></div>`, 0, 0},
		{"clear on a float, rtl float in ltr block", "ltr",
			`<div style="float:left; ` + box + `"></div><div id="t" style="float:left; clear:inline-start; direction:rtl; ` + box + `"></div>`, 0, 5},
		{"clear in an rtl inline in an ltr block", "ltr",
			`<span style="direction:rtl"><div style="float:left; ` + box + `"></div><div id="t" style="clear:inline-start; ` + box + `"></div></span>`, 0, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="display:flow-root; width:300px; direction:`+c.cb+`">`+c.inner+`</div>`, nil)
			b := findBox(fl.root, byID("t"))
			if b == nil || b.X != c.wantX || b.Y != c.wantY {
				t.Errorf("box = %v, want at (%v, %v)", boxDims(b), c.wantX, c.wantY)
			}
		})
	}
}

// TestInlineMinMax_FloatClearUseContainingBlockDirection pins the same rule
// for a line's floats at max-content: a float clearing the previous float's
// side starts a new row (FloatsMaxSize::ComputeMaxSizeForLine,
// inline/inline_node.cc:2034-2054 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// with both resolved against the block's style. Ahem 10px: "XXXXX" is 50px.
func TestInlineMinMax_FloatClearUseContainingBlockDirection(t *testing.T) {
	inner := `<div style="float:left; width:30px; height:5px"></div><div style="float:left; clear:inline-start; direction:rtl; width:30px; height:5px"></div>XXXXX`
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:200px; `+fallbackTestFont+`"><div id="t" style="display:inline-block">`+inner+`</div></div>`, nil)
	b := findBox(fl.root, byID("t"))
	if b == nil || b.Width != 80 {
		t.Errorf("shrink-to-fit inline-block = %v, want width 80", boxDims(b))
	}
}

// TestBlockMinMax_FloatClearUseContainingBlockDirection pins it for the
// block-level walk that sums float rows (BlockLayoutAlgorithm::
// ComputeMinMaxSizes, block_layout_algorithm.cc:430, :507 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestBlockMinMax_FloatClearUseContainingBlockDirection(t *testing.T) {
	inner := `<div style="float:left; width:30px; height:5px"></div><div style="float:left; clear:inline-start; direction:rtl; width:30px; height:5px"></div><div style="width:10px; height:5px"></div>`
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:200px"><div id="t" style="display:inline-block">`+inner+`</div></div>`, nil)
	b := findBox(fl.root, byID("t"))
	if b == nil || b.Width != 30 {
		t.Errorf("shrink-to-fit inline-block = %v, want width 30", boxDims(b))
	}
}

// TestFloatClear_LastInvalidDeclarationKeepsEarlierValue pins the layout
// result of an invalid clear value after a valid one, in a rule and in the
// style attribute: the earlier value still clears
// (CSSParserFastPaths::IsValidKeywordPropertyAndValue,
// parser/css_parser_fast_paths.cc:1195-1199 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFloatClear_LastInvalidDeclarationKeepsEarlierValue(t *testing.T) {
	const box = "width:100px; height:5px;"
	for _, c := range []struct{ name, sheet, attr string }{
		{"rule", `<style>#t{clear:left;clear:inline}</style>`, ""},
		{"style attribute", "", "clear:left;clear:inline;"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html>`+c.sheet+`<body style="margin:0"><div style="display:flow-root; width:300px"><div style="float:left; `+box+`"></div><div id="t" style="`+c.attr+box+`"></div></div>`, nil)
			b := findBox(fl.root, byID("t"))
			if b == nil || b.X != 0 || b.Y != 5 {
				t.Errorf("box = %v, want at (0, 5)", boxDims(b))
			}
		})
	}
}

// TestFloat_EnvFallbackIsUsed pins float through env(): a pending substitution
// at parse time (parser/css_variable_parser.cc:582-587 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), the fallback at computed time.
func TestFloat_EnvFallbackIsUsed(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><style>#t{float:env(x,right)}</style><body style="margin:0"><div style="display:flow-root; width:300px"><div id="t" style="width:100px; height:5px"></div></div>`, nil)
	b := findBox(fl.root, byID("t"))
	if b == nil || b.X != 200 {
		t.Errorf("box = %v, want x=200", boxDims(b))
	}
}
