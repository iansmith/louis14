package layout

import (
	"strconv"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// An orthogonal atomic inline contributes its block size to its line's
// min/max-content: LineBreaker measures it with
// ComputeMinAndMaxContentContribution (inline/line_breaker.cc:3182-3194 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which lays an orthogonal child
// out. So a horizontal 200x100 img inline in a vertical-lr inline-block
// gives the inline-block an inline size (height) of 100 + 2 border.
// Mirrors WPT css-writing-modes/img-intrinsic-size-contribution-002.
func TestOrthogonalAtomicInlineContributesBlockSize(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode: vertical-lr;"><body style="margin:0">`+
		`<div id=i style="display:inline-block; border: 1px solid orange;"><img src="x.png" style="writing-mode: horizontal-tb; width:200px; height:100px"></div></body></html>`, nil)
	if i := findBox(fl.root, byID("i")); i == nil || i.Height != 102 {
		t.Errorf("inline-block %s, want height 102", boxDims(i))
	}
}

// The orthogonal atomic's contribution for each case, as the vertical-lr
// inline-block parent's inline size (height). The child is laid out at its
// min/max-content border-box inline size, so its padding does not make it
// wrap; percentage margins resolve to 0 while sizing; a definite parent
// block size gives the child's percentage inline size its base.
func TestOrthogonalAtomicInlineContributionCases(t *testing.T) {
	atom := func(w int) string {
		return `<span style="display:inline-block;width:` + strconv.Itoa(w) + `px;height:20px"></span>`
	}
	for _, tc := range []struct {
		name, parent, child string
		want                float64
	}{
		{"padding", ``, `<div style="display:inline-block; writing-mode:horizontal-tb; padding:0 10px">` + atom(40) + atom(40) + `</div>`, 20},
		{"definite parent block size", `width:300px`, `<div style="display:inline-block; writing-mode:horizontal-tb; width:50%">` + atom(100) + atom(100) + `</div>`, 40},
		{"percentage margin", ``, `<img src="x.png" style="writing-mode:horizontal-tb; width:200px; height:100px; margin-top:10%">`, 100},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode:vertical-lr"><body style="margin:0; font-size:0; line-height:0">`+
			`<div id=p style="display:inline-block; `+tc.parent+`">`+tc.child+`</div></body></html>`, nil)
		if p := findBox(fl.root, byID("p")); p == nil || p.Height != tc.want {
			t.Errorf("%s: parent %s, want height %v", tc.name, boxDims(p), tc.want)
		}
	}
}

// A percentage block size on an orthogonal atomic resolves while sizing
// against its containing block's size in that axis, which is the parent's
// inline percentage resolution size: the line breaker passes the parent's
// percentage resolution size on to it (inline/line_breaker.cc:3185-3190 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). height:50% of 200 is 100.
func TestOrthogonalAtomicInlinePercentageBlockSize(t *testing.T) {
	img := makeNode("img")
	img.Attributes = map[string]string{"src": "x.png"}
	parent := makeNode("div", img)
	ps := css.NewStyle()
	ps.Properties["writing-mode"] = "vertical-lr"
	ps.Properties["font-size"] = "0"
	ps.Properties["line-height"] = "0"
	is := css.NewStyle()
	is.Properties["writing-mode"] = "horizontal-tb"
	is.Properties["width"] = "40px"
	is.Properties["height"] = "50%"
	is.Properties["display"] = "inline"
	root := buildTestTree(parent, map[*html.Node]*css.Style{parent: ps, img: is})
	wdm := WritingDirectionMode{WritingModeVerticalLR, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 300}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 300}).
		SetPercentageResolutionInlineSize(200).Build()
	if mm := ComputeMinMaxSizes(testContext(), root, space); mm.MinContent != 100 || mm.MaxContent != 100 {
		t.Errorf("min/max %+v, want 100/100", mm)
	}
}

// Round 2 of the review: the percentage base, the available inline size,
// and ruby sub-lines.
func TestOrthogonalAtomicInlineRound2(t *testing.T) {
	atoms := ""
	for range 5 {
		atoms += `<span style="display:inline-block;width:40px;height:20px"></span>`
	}
	for _, tc := range []struct {
		name, html string
		check      func(fl fallbackLayout) string
	}{
		// An indefinite parent block size leaves the child's percentage
		// block size indefinite, so height:50% is auto (30 from its child),
		// not 50% of 0.
		{"indefinite percentage base", `<div id=p style="display:inline-block"><div style="display:inline-block; writing-mode:horizontal-tb; height:50%"><div style="width:10px;height:30px"></div></div></div>`,
			func(fl fallbackLayout) string {
				if p := findBox(fl.root, byID("p")); p == nil || p.Height != 30 {
					return "parent " + boxDims(p) + ", want height 30"
				}
				return ""
			}},
		// Sizing and layout give the orthogonal child the same available
		// inline size (the 100px-wide vertical-lr ancestor's block size), so
		// its text wraps the same way in both: three lines, 60 tall, and the
		// parent sized to that.
		{"same available inline size", `<div style="width:100px"><div id=p style="display:inline-block"><div id=c style="display:inline-block; writing-mode:horizontal-tb">` + atoms + `</div></div></div>`,
			func(fl fallbackLayout) string {
				p, c := findBox(fl.root, byID("p")), findBox(fl.root, byID("c"))
				if p == nil || c == nil || c.Height != 60 || p.Height != c.Height {
					return "parent " + boxDims(p) + ", child " + boxDims(c) + ", want both 60 tall"
				}
				return ""
			}},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode:vertical-lr"><body style="margin:0; font-size:0; line-height:0">`+tc.html+`</body></html>`, nil)
		if msg := tc.check(fl); msg != "" {
			t.Errorf("%s: %s", tc.name, msg)
		}
	}
}

// A ruby base is broken by a max-content sub-line breaker during layout,
// but that is not an intrinsic-sizing pass: Blink keys sizing on the root
// breaker's mode (LineBreaker, inline/line_breaker.cc:3092-3096 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So an orthogonal atomic in a
// ruby base is laid out, and margins resolve as in layout.
func TestOrthogonalAtomicInRubyBaseIsLaidOut(t *testing.T) {
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:20px"><div style="width:400px"><ruby>`+
		`<span id=o style="display:inline-block; writing-mode:vertical-rl; width:30px; height:20px"></span>`+
		`<span id=m style="display:inline-block; width:10px; height:10px; margin-left:10%"></span>`+
		`<rt>a</rt></ruby></div></body>`, nil)
	o, m := findBox(fl.root, byID("o")), findBox(fl.root, byID("m"))
	if o == nil || o.Width != 30 || o.Height != 20 {
		t.Errorf("orthogonal atomic in a ruby base %s, want laid out 30x20", boxDims(o))
	}
	if o != nil && m != nil && m.X-(o.X+o.Width) == 0 {
		t.Errorf("atomic after it at %s: its 10%% margin-left resolved to 0", boxDims(m))
	}
}
