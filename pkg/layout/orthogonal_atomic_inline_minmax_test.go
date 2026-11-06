package layout

import (
	"strconv"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
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

// While sizing, an orthogonal atomic gets only the line's percentage block
// size, as its percentage inline size; its percentage block size is
// indefinite (LineBreaker's MinMaxConstraintSpaceBuilder::
// SetPercentageResolutionBlockSize, inline/line_breaker.cc:3185-3190;
// constraint_space_builder.h:743-745 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So under a vertical-lr parent
// with a 300px percentage block size, width:50% is 150 (three 40px children
// per line, two lines: 40 tall), and height:50% is auto (its 30px child).
func TestOrthogonalAtomicInlinePercentageSizes(t *testing.T) {
	for _, tc := range []struct {
		name, prop string
		kids       int
		want       float64
	}{
		{"width:50% of the line's block size", "width", 5, 40},
		{"height:50% is auto", "height", 0, 30},
	} {
		var kids []*html.Node
		styles := map[*html.Node]*css.Style{}
		for range tc.kids {
			k := makeNode("span")
			ks := css.NewStyle()
			ks.Properties["display"] = "inline-block"
			ks.Properties["width"] = "40px"
			ks.Properties["height"] = "20px"
			styles[k] = ks
			kids = append(kids, k)
		}
		if tc.kids == 0 {
			k := makeNode("div")
			ks := css.NewStyle()
			ks.Properties["width"] = "10px"
			ks.Properties["height"] = "30px"
			styles[k] = ks
			kids = append(kids, k)
		}
		child := makeNode("span", kids...)
		parent := makeNode("div", child)
		ps := css.NewStyle()
		ps.Properties["writing-mode"] = "vertical-lr"
		ps.Properties["font-size"] = "0"
		ps.Properties["line-height"] = "0"
		cs := css.NewStyle()
		cs.Properties["display"] = "inline-block"
		cs.Properties["writing-mode"] = "horizontal-tb"
		cs.Properties["font-size"] = "0"
		cs.Properties["line-height"] = "0"
		cs.Properties[tc.prop] = "50%"
		styles[parent], styles[child] = ps, cs
		root := buildTestTree(parent, styles)
		wdm := WritingDirectionMode{WritingModeVerticalLR, DirectionLTR}
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 200, BlockSize: 300}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 200, BlockSize: 300}).
			SetPercentageResolutionInlineSize(200).Build()
		if mm := ComputeMinMaxSizes(testContext(), root, space); mm.MinContent != tc.want || mm.MaxContent != tc.want {
			t.Errorf("%s: min/max %+v, want %v", tc.name, mm, tc.want)
		}
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
		// Sizing and layout give the orthogonal child one available inline
		// size, so its atoms wrap the same way in both and the parent's
		// inline size (height) is the child's height. With the parent's
		// block size indefinite that is Blink's orthogonal fallback, the ICB
		// clamped by the parent's own fixed block size
		// (SetOrthogonalFallbackInlineSizeIfNeeded, block_node.cc:1469 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a 100px-wide ancestor
		// does not narrow it (one line, 20), the parent's own width:100px
		// does (three lines, 60).
		{"100px ancestor", `<div style="width:100px"><div id=p style="display:inline-block"><div id=c style="display:inline-block; writing-mode:horizontal-tb">` + atoms + `</div></div></div>`,
			sameHeight(20)},
		{"100px parent", `<div id=p style="display:inline-block; width:100px"><div id=c style="display:inline-block; writing-mode:horizontal-tb">` + atoms + `</div></div>`,
			sameHeight(60)},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode:vertical-lr"><body style="margin:0; font-size:0; line-height:0">`+tc.html+`</body></html>`, nil)
		if msg := tc.check(fl); msg != "" {
			t.Errorf("%s: %s", tc.name, msg)
		}
	}
}

// sameHeight checks that the parent #p, sized to its orthogonal child #c,
// is as tall as #c, and that both are want tall.
func sameHeight(want float64) func(fl fallbackLayout) string {
	return func(fl fallbackLayout) string {
		p, c := findBox(fl.root, byID("p")), findBox(fl.root, byID("c"))
		if p == nil || c == nil || c.Height != want || p.Height != c.Height {
			return "parent " + boxDims(p) + ", child " + boxDims(c) + ", want both " + strconv.FormatFloat(want, 'f', -1, 64) + " tall"
		}
		return ""
	}
}

// A ruby base is broken by a max-content sub-line breaker during layout,
// but that is not an intrinsic-sizing pass: Blink keys sizing on the root
// breaker's mode (LineBreaker, inline/line_breaker.cc:3092-3096 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). So during layout an atomic in
// a ruby base, orthogonal or not, is laid out: its item result carries a
// layout result.
func TestAtomicInRubyBaseIsLaidOut(t *testing.T) {
	for _, wm := range []string{"horizontal-tb", "vertical-rl"} {
		atom := makeNode("span")
		rtText := &html.Node{Type: html.TextNode, Text: "a"}
		rt := makeNode("rt", rtText)
		ruby := makeNode("ruby", atom, rt)
		root := makeNode("div", ruby)
		atomStyle := css.NewStyle()
		atomStyle.Properties["display"] = "inline-block"
		atomStyle.Properties["writing-mode"] = wm
		atomStyle.Properties["width"] = "30px"
		atomStyle.Properties["height"] = "20px"
		rubyStyle := css.NewStyle()
		rubyStyle.Properties["display"] = "ruby"
		rtStyle := css.NewStyle()
		rtStyle.Properties["display"] = "ruby-text"
		data := collectForNodes(t, root, map[*html.Node]*css.Style{root: css.NewStyle(), ruby: rubyStyle, atom: atomStyle, rt: rtStyle})
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		space := NewConstraintSpaceBuilder(wdm, wdm, false).
			SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			SetPercentageResolutionInlineSize(400).Build()
		lb := NewLineBreaker(data, testContext(), space, text.DefaultFontConfig(), LineBreakerContent)
		var line LineInfo
		lb.NextLine(&line)
		found := false
		for _, col := range line.RubyColumns {
			for _, r := range col.BaseLine.Results {
				if r.Item != nil && r.Item.Type == InlineItemAtomicInline {
					found = true
					if r.LayoutResult == nil {
						t.Errorf("%s atomic in a ruby base has no layout result", wm)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no atomic in a ruby base sub-line", wm)
		}
	}
}

// rubyBaseAtomicResult lays a line out at a definite 400px over
// <div><ruby>ATOM<rt>a</rt></ruby></div> and returns the base sub-line's
// atomic item result.
func rubyBaseAtomicResult(t *testing.T, atomStyle *css.Style, atomChildren ...*html.Node) *InlineItemResult {
	t.Helper()
	atom := makeNode("span", atomChildren...)
	rt := makeNode("rt", &html.Node{Type: html.TextNode, Text: "a"})
	ruby := makeNode("ruby", atom, rt)
	root := makeNode("div", ruby)
	rubyStyle, rtStyle := css.NewStyle(), css.NewStyle()
	rubyStyle.Properties["display"] = "ruby"
	rtStyle.Properties["display"] = "ruby-text"
	styles := map[*html.Node]*css.Style{root: css.NewStyle(), ruby: rubyStyle, atom: atomStyle, rt: rtStyle}
	for _, c := range atomChildren {
		cs := css.NewStyle()
		cs.Properties["display"] = "inline-block"
		cs.Properties["width"] = "40px"
		cs.Properties["height"] = "20px"
		styles[c] = cs
	}
	data := collectForNodes(t, root, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, false).
		SetAvailableSize(LogicalSize{InlineSize: 100, BlockSize: Indefinite}).
		SetPercentageResolutionInlineSize(100).Build()
	lb := NewLineBreaker(data, testContext(), space, text.DefaultFontConfig(), LineBreakerContent)
	for range 5 {
		var line LineInfo
		if !lb.NextLine(&line) {
			break
		}
		for _, col := range line.RubyColumns {
			for i, r := range col.BaseLine.Results {
				if r.Item != nil && r.Item.Type == InlineItemAtomicInline {
					return &col.BaseLine.Results[i]
				}
			}
		}
	}
	t.Fatal("no atomic in the ruby base sub-line")
	return nil
}

// A ruby base sub-line breaker shares its parent's constraint space
// (LineBreaker, inline/line_breaker.cc:3066-3067, 3532-3560 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a percentage margin resolves
// against the line's 100px, not the sub-breaker's unbounded width, and an
// atomic is laid out at the line's available inline size, so an auto-width
// inline-block shrink-to-fits to 100px and wraps its five 40px children onto
// three lines.
func TestRubyBaseAtomicUsesTheLineSpace(t *testing.T) {
	img := css.NewStyle()
	img.Properties["display"] = "inline-block"
	img.Properties["width"] = "10px"
	img.Properties["height"] = "10px"
	img.Properties["margin-left"] = "10%"
	if r := rubyBaseAtomicResult(t, img); r.Margins.InlineStart != 10 {
		t.Errorf("margin-left:10%% in a ruby base resolved to %v, want 10", r.Margins.InlineStart)
	}
	ib := css.NewStyle()
	ib.Properties["display"] = "inline-block"
	ib.Properties["font-size"] = "0"
	var kids []*html.Node
	for range 5 {
		kids = append(kids, makeNode("span"))
	}
	if r := rubyBaseAtomicResult(t, ib, kids...); r.LayoutResult == nil || r.LayoutResult.Fragment.Size.WidthF64() != 100 || r.LayoutResult.Fragment.Size.HeightF64() != 60 {
		var w float64
		if r.LayoutResult != nil {
			w = r.LayoutResult.Fragment.Size.WidthF64()
		}
		t.Errorf("auto-width inline-block in a ruby base is %v wide, want 100x60 (shrink-to-fit at 100px: two 40px per line, three lines)", w)
	}
}

// One derivation of an orthogonal atomic's available inline size for sizing
// and layout, from the container's initial fragment geometry: an abspos
// container's block size from its insets counts (left:0; right:700px is
// 100 wide), and a definite 0 is definite.
func TestOrthogonalAvailableBlockFromContainerGeometry(t *testing.T) {
	atoms := ""
	for range 5 {
		atoms += `<span style="display:inline-block;width:40px;height:20px"></span>`
	}
	child := `<div id=c style="display:inline-block; writing-mode:horizontal-tb">` + atoms + `</div>`
	for _, tc := range []struct {
		name, parent string
		want         float64
	}{
		{"abspos insets", `position:absolute; left:0; right:700px`, 60},
		{"definite width 0", `display:inline-block; width:0`, 100},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><html style="writing-mode:vertical-lr"><body style="margin:0; font-size:0; line-height:0">`+
			`<div id=p style="`+tc.parent+`">`+child+`</div></body></html>`, nil)
		if msg := sameHeight(tc.want)(fl); msg != "" {
			t.Errorf("%s: %s", tc.name, msg)
		}
	}
}

// An atomic inline is laid out in its container's space, the content box:
// the line's own width (text-wrap:balance narrowing, nowrap's unbounded
// width, a float beside it, text-indent) shapes only the line opportunity
// (BlockNode::LayoutAtomicInline in the parent space, block_node.cc;
// LineBreaker::HandleAtomicInline, inline/line_breaker.cc:3042-3160 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Its ten 40px children wrap at
// 300, so it is 300 wide, and the line places it.
func TestAtomicInlineLaidOutInContainerContentBox(t *testing.T) {
	atoms := ""
	for range 10 {
		atoms += `<span style="display:inline-block;width:40px;height:20px"></span>`
	}
	ib := `<div id=b style="display:inline-block; white-space:normal; text-wrap:wrap">` + atoms + `</div>`
	for _, tc := range []struct {
		name, container, before string
		wantX, wantY            float64
	}{
		{"text-wrap:balance", `text-wrap:balance`, `<span style="display:inline-block;width:10px;height:20px"></span> `, -1, -1},
		{"nowrap", `white-space:nowrap`, ``, 0, 0},
		{"beside a float, moved below it", ``, `<div style="float:left;width:100px;height:50px"></div>`, 0, 50},
		{"text-indent", `text-indent:100px`, ``, 100, 0},
	} {
		fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0; line-height:0"><div style="width:300px; `+tc.container+`">`+tc.before+ib+`</div></body>`, nil)
		b := findBox(fl.root, byID("b"))
		if b == nil || b.Width != 300 || (tc.wantX >= 0 && (b.X != tc.wantX || b.Y != tc.wantY)) {
			t.Errorf("%s: #b %s, want 300 wide at (%v,%v)", tc.name, boxDims(b), tc.wantX, tc.wantY)
		}
	}
}

// The text-wrap:balance probe breaks lines as final layout does: its
// atomics are laid out at the container's content inline size, with the
// content box as their percentage base, not at the probe width. A 100px
// inline-block then a width:50% one, in a 400px container probed at 250: the
// second is 200 wide, does not fit beside the first, so two lines in the
// probe as in layout.
func TestBalanceProbeLaysAtomicsOutInContentBox(t *testing.T) {
	a, c := makeNode("span"), makeNode("span")
	root := makeNode("div", a, c)
	as, cs, rs := css.NewStyle(), css.NewStyle(), css.NewStyle()
	as.Properties["display"] = "inline-block"
	as.Properties["width"] = "100px"
	as.Properties["height"] = "10px"
	cs.Properties["display"] = "inline-block"
	cs.Properties["width"] = "50%"
	cs.Properties["height"] = "10px"
	rs.Properties["font-size"] = "0"
	data := collectForNodes(t, root, map[*html.Node]*css.Style{root: rs, a: as, c: cs})
	ctx := testContext()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	fonts := text.DefaultFontConfig()

	probe := countLinesForWidth(data, ctx, wdm, 250, fonts, 400)
	// As layoutInlineChildren sets the line up: the line width narrowed,
	// the content box the atomics' space.
	lineSpace := ConstraintSpace{
		AvailableSize:                  oldLogicalToGeom(LogicalSize{InlineSize: 250, BlockSize: Indefinite}),
		PercentageResolutionSize:       oldLogicalToGeom(LogicalSize{InlineSize: 400, BlockSize: Indefinite}),
		PercentageResolutionInlineSize: 400,
		WritingDirection:               wdm,
	}
	lb := NewLineBreaker(data, ctx, lineSpace, fonts, LineBreakerContent)
	lb.availableWidth, lb.containerInlineSize = 250, 400
	layout := 0
	var line LineInfo
	for lb.NextLine(&line) {
		layout++
	}
	if probe != 2 || layout != 2 {
		t.Errorf("lines at 250 in a 400px container: probe %d, layout %d, want 2 and 2", probe, layout)
	}
}

// The container geometry intrinsic sizing reads for its orthogonal atomics
// clamps a definite block size by min/max-block-size, as
// CalculateInitialFragmentGeometry does: a scroller with height:300px and
// max-height:200px passes 200 down as the orthogonal fallback block size, so
// the orthogonal inline-block inside its inline-block child wraps five
// 50px-tall atoms onto two 20px columns, 40 wide, in sizing as in layout.
func TestIntrinsicContainerBlockSizeIsClamped(t *testing.T) {
	atoms := ""
	for range 5 {
		atoms += `<span style="display:inline-block;width:20px;height:50px"></span>`
	}
	fl := layoutFallbackHTML(t, `<!DOCTYPE html><body style="margin:0; font-size:0; line-height:0"><div id=f style="float:left; overflow:auto; height:300px; max-height:200px">`+
		`<div style="display:inline-block"><div id=b style="display:inline-block; writing-mode:vertical-rl">`+atoms+`</div></div></div></body>`, nil)
	f, b := findBox(fl.root, byID("f")), findBox(fl.root, byID("b"))
	if f == nil || b == nil || b.Width != 40 || f.Width != b.Width {
		t.Errorf("scroller %s, orthogonal inline-block %s, want both 40 wide", boxDims(f), boxDims(b))
	}
}
