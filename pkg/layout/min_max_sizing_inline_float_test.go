package layout

import "testing"

// TestInlineMinMax_FloatsContribute pins InlineNode's float contribution to
// the intrinsic sizes of an inline formatting context (ComputeContentSize's
// FloatsMaxSize and min-content float term, inline_node.cc:2008-2064 and
// :2236-2296 @ d4ecdfed88f962439247c2ad36b8fe47805b1520): at max-content a
// line's floats sit beside its text, so their max-content contributions
// (with margins) add to the line width; at min-content each float's
// min-content contribution is a lower bound. Ahem 10px: "XXXXX" is 50px.
func TestInlineMinMax_FloatsContribute(t *testing.T) {
	cases := []struct {
		name  string
		inner string
		outer string // containing block width
		want  float64
	}{
		{"max-content: float beside the text", `<div style="float:left; width:16px; height:16px"></div>XXXXX`, "200px", 66},
		{"max-content: float inside an inline", `<span><div style="float:left; width:16px; height:16px"></div>XXXXX</span>`, "200px", 66},
		{"max-content: float margins count", `<div style="float:right; width:16px; height:16px; margin-left:4px"></div>XXXXX`, "200px", 70},
		{"max-content: a clearing float starts a new row", `<div style="float:left; width:30px; height:5px"></div><div style="float:left; clear:left; width:30px; height:5px"></div>XXXXX`, "200px", 80},
		{"min-content: a wide float is a lower bound", `<div style="float:left; width:80px; height:16px"></div>XXXXX XXXXX`, "10px", 80},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:`+c.outer+`; `+fallbackTestFont+`"><div id="t" style="display:inline-block">`+c.inner+`</div></div>`, nil)
			b := findBox(fl.root, byID("t"))
			if b == nil || b.Width != c.want {
				t.Errorf("shrink-to-fit inline-block = %v, want width %v", boxDims(b), c.want)
			}
		})
	}
}
