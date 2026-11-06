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
