package css

import "testing"

// TestAdjustComputedStyle_Display covers StyleAdjuster's display
// adjustments (style_adjuster.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// blockification against the layout parent (skipping display:contents
// ancestors), float/out-of-flow blockification, math on non-MathML
// elements, and display:contents → none for unusual HTML and SVG elements.
func TestAdjustComputedStyle_Display(t *testing.T) {
	styles := cascadeHTML(t, `<!DOCTYPE html>
<div style="display:flex">
  <span id="item">x</span>
  <div id="c1" style="display:contents">
    <span id="through-contents">y</span>
    <div id="c2" style="display:contents"><i id="nested">z</i></div>
  </div>
  <br id="br">
  <span id="ib" style="display:inline-block">ib</span>
  <span id="if" style="display:inline-flex">if</span>
</div>
<div style="display:grid"><span id="grid-item" style="display:inline-grid">g</span></div>
<div><div style="display:contents"><span id="not-blockified">n</span></div></div>
<span id="float" style="float:left">f</span>
<span id="abs" style="position:absolute">a</span>
<span id="flow-root-float" style="display:inline-block; float:left">f</span>
<div id="math" style="display:math">m</div>
<div id="block-math" style="display:block math">m</div>
<img id="img" style="display:contents">
<input id="input" style="display:contents">
<br id="br-contents" style="display:contents">
<svg id="svg" style="display:contents"><g id="g" style="display:contents"><rect id="rect" style="display:contents"/></g></svg>
`, nil)
	cases := []struct {
		id   string
		want DisplayType
	}{
		{"item", DisplayBlock},
		{"c1", DisplayContents},
		{"through-contents", DisplayBlock},
		{"c2", DisplayContents},
		{"nested", DisplayBlock},
		{"br", DisplayBlock},
		{"ib", DisplayBlock},
		{"if", DisplayFlex},
		{"grid-item", DisplayGrid},
		{"not-blockified", DisplayInline},
		{"float", DisplayBlock},
		{"abs", DisplayBlock},
		{"flow-root-float", DisplayBlock},
		{"math", DisplayInline},
		{"block-math", DisplayBlock},
		{"img", DisplayNone},
		{"input", DisplayNone},
		{"br-contents", DisplayNone},
		{"svg", DisplayNone},
		{"g", DisplayContents},
		{"rect", DisplayNone},
	}
	for _, c := range cases {
		if got := styleByID(t, styles, c.id).GetDisplay(); got != c.want {
			t.Errorf("#%s display = %q, want %q", c.id, got, c.want)
		}
	}
	// OriginalDisplay keeps the pre-adjustment value: an out-of-flow box
	// that was inline-level takes an inline static position.
	if abs := styleByID(t, styles, "abs"); abs.OriginalDisplay() != DisplayInline || !abs.IsOriginalDisplayInlineType() {
		t.Errorf("#abs OriginalDisplay = %q, want inline", abs.OriginalDisplay())
	}
	if styleByID(t, styles, "if").IsOriginalDisplayInlineType() != true {
		t.Errorf("#if (inline-flex flex item) IsOriginalDisplayInlineType = false, want true")
	}
	if !styleByID(t, styles, "c1").IsInBlockifyingDisplay() {
		t.Errorf("#c1 (display:contents flex child) IsInBlockifyingDisplay = false, want true")
	}
	if !styleByID(t, styles, "c1").BlockifiesChildren() {
		t.Errorf("#c1 BlockifiesChildren = false, want true (contents && IsInBlockifyingDisplay)")
	}
}

// TestGetDisplay_Math pins the math display values (css_value_id_mappings.h
// kMath / kBlockMath @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and their
// EquivalentBlockDisplay mapping (style_adjuster.cc:271-272).
func TestGetDisplay_Math(t *testing.T) {
	for in, want := range map[string]DisplayType{
		"math": DisplayMath, "inline math": DisplayMath, "block math": DisplayBlockMath,
	} {
		s := NewStyle()
		s.Set("display", in)
		if got := s.GetDisplay(); got != want {
			t.Errorf("display:%s → %q, want %q", in, got, want)
		}
	}
	if got := EquivalentBlockDisplay(DisplayMath); got != DisplayBlockMath {
		t.Errorf("EquivalentBlockDisplay(math) = %q, want block math", got)
	}
	if !isSupportedDeclaration("display", "math") {
		t.Errorf("@supports (display: math) = false, want true")
	}
}

// TestAdjustComputedStyle_IsInBlockifyingDisplayComputedFresh: Blink computes
// IsInBlockifyingDisplay per style; a style cloned from a blockified one and
// adjusted under a non-blockifying layout parent must not keep it.
func TestAdjustComputedStyle_IsInBlockifyingDisplayComputedFresh(t *testing.T) {
	flex := NewStyle()
	flex.Set("display", "flex")
	s := NewStyle()
	s.Set("display", "contents")
	AdjustComputedStyle(nil, s, flex)
	if !s.IsInBlockifyingDisplay() {
		t.Fatalf("contents child of flex: IsInBlockifyingDisplay = false, want true")
	}
	clone := s.Clone()
	block := NewStyle()
	block.Set("display", "block")
	AdjustComputedStyle(nil, clone, block)
	if clone.IsInBlockifyingDisplay() || clone.BlockifiesChildren() {
		t.Errorf("clone under block parent: IsInBlockifyingDisplay=%v BlockifiesChildren=%v, want false/false",
			clone.IsInBlockifyingDisplay(), clone.BlockifiesChildren())
	}
}
