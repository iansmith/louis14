package layout

import "testing"

// A <button> with no baseline synthesizes one from its content box, not its
// margin box: HTMLButtonElement::AdjustStyle sets InlineBlockBaselineEdge to
// kContentBox (core/html/forms/html_button_element.cc:76-80), and
// LogicalBoxFragment::BaselineMetrics synthesizes off that edge
// (layout/logical_box_fragment.cc:52-83 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A layout-contained button has
// no baseline, so its content-box bottom (5px above its border-box bottom)
// sits on the line's baseline, as an empty inline-block with
// margin-bottom: -5px does. Mirrors WPT
// css-contain/contain-layout-button-001.tentative.
func TestContainedButtonBaselineFromContentBox(t *testing.T) {
	top := func(el string) float64 {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0">before`+el+`after</body>`, nil)
		b := findBox(fl.root, byID("b"))
		if b == nil {
			t.Fatal("no box")
		}
		return b.Y
	}
	button := top(`<button id=b style="border:5px solid; padding:0; margin-bottom:2px; contain:layout; width:0; height:0">b</button>`)
	ref := top(`<div id=b style="display:inline-block; border:5px solid; padding:0; margin-bottom:-5px; width:0; height:0"></div>`)
	if button != ref {
		t.Errorf("contained button top %v, want %v (content-box baseline)", button, ref)
	}
}

// layoutBaselineCase lays out `before<marker>EL after` and returns the
// element's box and the line's baseline, read from a zero-size inline-block
// marker whose synthesized baseline is its (empty) bottom margin edge.
func layoutBaselineCase(t *testing.T, el string, fetcher func(string) ([]byte, error)) (*Box, float64) {
	t.Helper()
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="width:200px">before<span id=m style="display:inline-block; width:0; height:0"></span>`+el+`after</div></body>`, fetcher)
	b, m := findBox(fl.root, byID("b")), findBox(fl.root, byID("m"))
	if b == nil || m == nil {
		t.Fatalf("missing boxes: %s %s", boxDims(b), boxDims(m))
	}
	return b, m.Y
}

// A baseline-less button's content-box block-end sits on the baseline, with
// the fragment's resolved border, scrollbar and padding subtracted
// (LogicalBoxFragment::BaselineMetrics kContentBox, logical_box_fragment.cc:
// 70-80 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); its overflow does not
// force margin-box synthesis (ShouldIgnoreOverflowPropertyForInlineBlockBaseline,
// html_button_element.cc:77; ForceInlineBaselineSynthesis,
// physical_box_fragment.h:169-173).
func TestButtonBaselineContentBoxEdge(t *testing.T) {
	const base = "contain:layout; border:5px solid; width:0; height:0; margin-bottom:2px"
	cases := []struct {
		name, style string
		endInset    float64 // border + scrollbar + padding at the block end
	}{
		{"percentage padding", "padding:10%", 5 + 20},
		{"asymmetric padding", "padding:0 0 7px 0", 5 + 7},
		{"overflow hidden", "padding:0; overflow:hidden", 5},
		{"scrollbar", "padding:0; overflow-x:scroll; overflow-y:hidden; height:20px; width:40px", 5 + 15},
		// An orthogonal button has no baseline in the line's writing mode
		// and still synthesizes off its content box.
		{"orthogonal", "padding:0 0 3px 0; writing-mode:vertical-lr", 5 + 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, baseline := layoutBaselineCase(t, `<button id=b style="`+base+`; `+tc.style+`">b</button>`, nil)
			if got := b.Y + b.Height - tc.endInset; got != baseline {
				t.Errorf("%s: content-box end at %v, baseline at %v (button %s)", tc.name, got, baseline, boxDims(b))
			}
		})
	}
}

// Only <button> synthesizes off its content box: a button-type <input> has
// no AdjustStyle that sets InlineBlockBaselineEdge (InputTypeView::AdjustStyle
// is empty, input_type_view.h:116, and BaseButtonInputType does not override
// it @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so a baseline-less one
// keeps the margin box.
func TestInputButtonBaselineMarginBoxEdge(t *testing.T) {
	b, baseline := layoutBaselineCase(t, `<input id=b type=submit value=x style="contain:layout; border:5px solid; padding:0; width:0; height:0; margin-bottom:2px">`, nil)
	if got := b.Y + b.Height + 2; got != baseline {
		t.Errorf("input submit: margin-box end at %v, baseline at %v (%s)", got, baseline, boxDims(b))
	}
}

// With a central baseline (a vertical line) a baseline-less button centres
// its content box, not its border box, on the baseline (SynthesizeMetrics
// with kCentralBaseline over the content size, logical_box_fragment.cc:52-56,
// :70-80). In vertical-rl the block-start is the right side: a 10px
// block-start padding puts the 0-wide content box 5px (the left border) in
// from the left border edge, and that is where the baseline runs.
func TestButtonCentralBaselineContentBoxCentre(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="writing-mode:vertical-rl; height:200px">before<span id=m style="display:inline-block; width:0; height:0"></span>`+
		`<button id=b style="contain:layout; border:5px solid; padding:0 10px 0 0; width:0; height:0">b</button>after</div></body>`, nil)
	b, m := findBox(fl.root, byID("b")), findBox(fl.root, byID("m"))
	if b == nil || m == nil {
		t.Fatalf("missing boxes: %s %s", boxDims(b), boxDims(m))
	}
	if got := b.X + 5; got != m.X {
		t.Errorf("central baseline: content-box centre at x=%v, baseline at x=%v (button %s)", got, m.X, boxDims(b))
	}
}
