package layout

import "testing"

// chrome152 labels expected values probed with getClientRects in Chrome 152
// (the Claude desktop app's browser pane, Chrome/152.0.7977.167).
const chrome152 = "Chrome 152 getClientRects"

// TestFixedContainerFlexGridTable pins flex, grid and table containers as
// containing blocks of their fixed-position descendants when they are fixed
// containers (here: a transform). Blink derives IsFixedContainer from style
// for every box kind (LayoutObject::ComputeIsFixedContainer,
// layout_object.cc:1849-1900 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// and the OutOfFlowLayoutPart every container runs reads it
// (out_of_flow_layout_part.cc:542-545; IsContainingBlockForCandidate,
// :2746-2791 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `f`
// (top:10px; left:5px) is placed against the container's padding box at
// (30,20), so at (35,30); against the viewport it would be at (5,10).
func TestFixedContainerFlexGridTable(t *testing.T) {
	for _, display := range []string{"flex", "grid", "table"} {
		t.Run(display, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="height:20px"></div>`+
				`<div style="display:`+display+`;width:100px;height:100px;margin-left:30px;transform:translateX(0)">`+
				`<div id=f style="position:fixed;top:10px;left:5px;width:10px;height:10px"></div></div>`)
			checkRectsFrom(t, got, map[string][]fragRect{
				"f": {{35, 30, 10, 10}},
			}, chrome152)
		})
	}
}

// TestOOFStaticPositionCarriesChildRelativeOffset pins the static position of
// a fixed-position box that escapes a position:relative flex item, grid
// item, table cell and inline-block: the containers propagate their
// children's out-of-flow descendants as they add them, shifting the static
// position by the child's offset, border+padding and relative offset
// (FragmentBuilder::PropagateFromFragment → PropagateOOFPositionedInfo,
// fragment_builder.cc:357-405, :762 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The relpos child (top:10px;
// left:7px; padding:3px) sits at the origin, so `f` is at (10,13); in the
// table cell (vertical-align:middle, 50px tall, no in-flow content) the
// content box is moved down by 25px (BoxFragmentBuilder::
// MoveChildrenInDirection moves the candidates, box_fragment_builder.cc:
// 450-457), so `f` is at (10,38).
func TestOOFStaticPositionCarriesChildRelativeOffset(t *testing.T) {
	const rel = `position:relative;top:10px;left:7px;width:50px;height:50px;padding:3px`
	const f = `<div id=f style="position:fixed;width:5px;height:5px"></div>`
	for _, tc := range []struct {
		name, body string
		want       fragRect
	}{
		{"flex", `<div style="display:flex"><div style="` + rel + `">` + f + `</div></div>`, fragRect{10, 13, 5, 5}},
		{"grid", `<div style="display:grid"><div style="` + rel + `">` + f + `</div></div>`, fragRect{10, 13, 5, 5}},
		{"table-cell", `<table style="border-spacing:0"><tr><td style="` + rel + `">` + f + `</td></tr></table>`, fragRect{10, 38, 5, 5}},
		{"inline-block", `<div><span style="display:inline-block;` + rel + `">` + f + `</span></div>`, fragRect{10, 13, 5, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font-size:0">`+tc.body)
			checkRectsFrom(t, got, map[string][]fragRect{"f": {tc.want}}, chrome152)
		})
	}
}

// TestPositionedFlexInMulticolDefersAbspos pins a position:relative flex
// container inside a multicol as the containing block of its abspos, which
// it defers to the multicol (should_add_outer_fragmentainer_children_,
// out_of_flow_layout_part.cc:546-548, :1190-1203 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) through its fragment's
// fragmentainer descendants: top:120px in 100px columns puts `a` 20px into
// column 2, at (100,20). Guard for the flex FragmentedOofData path (LOU-519).
func TestPositionedFlexInMulticolDefersAbspos(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="`+oofProbeMulticol+`width:200px;height:100px">`+
		`<div style="display:flex;position:relative;height:150px">`+
		`<div id=a style="position:absolute;top:120px;left:0;width:10px;height:10px"></div></div></div>`)
	checkRectsFrom(t, got, map[string][]fragRect{"a": {{100, 20, 10, 10}}}, chrome152)
}

// fixedContainerInMulticolProbe is a multicol (2 x 100px columns at (30,20))
// holding a transformed block `t`, a fixed container that breaks into both
// columns. inner is placed in t after a 120px block, so in t's second
// fragment.
func fixedContainerInMulticolProbe(inner string) string {
	return `<!DOCTYPE html><body style="margin:0;font-size:0"><div style="height:20px"></div>` +
		`<div style="` + oofProbeMulticol + `width:200px;height:100px;margin-left:30px">` +
		`<div id=t style="transform:translateX(0);padding-top:4px"><div style="height:120px"></div>` + inner + `</div></div>`
}

// fixedInAbs is an abspos holding a fixed box with insets (`fx`, top:110px
// against t's stitched padding box: 10px into column 2) and one at its
// static position (`fs`).
const fixedInAbs = `<div id=abs style="position:absolute;width:20px;height:20px">` +
	`<div id=fx style="position:fixed;top:110px;left:5px;width:10px;height:10px"></div>` +
	`<div id=fs style="position:fixed;width:10px;height:10px"></div></div>`

// TestFixedContainerInsideFragmentationContext pins the fixedpos containing
// block of a fixed box inside a fragmentainer descendant: the fixed
// container `t` inside the multicol, found while the abspos's fragmentainer
// descendant data propagates out of it (FragmentBuilder::
// AdjustFixedposContainerInfo, fragment_builder.cc:1108-1143; the fixedpos
// branch of PropagateOOFPositionedInfo, :783-816; LayoutOOFsInFragmentainer
// passing it on, out_of_flow_layout_part.cc:3057-3087 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `fx` is laid out against t's
// stitched padding box, in column 2; louis14 put it against the viewport.
func TestFixedContainerInsideFragmentationContext(t *testing.T) {
	got := oofPageRects(t, fixedContainerInMulticolProbe(
		`<div id=rel style="position:relative;height:40px"><div style="height:30px"></div>`+
			`<div style="position:absolute;top:10px;width:20px;height:20px">`+
			`<div id=fx style="position:fixed;top:110px;left:5px;width:10px;height:10px"></div>`+
			`<div id=fs style="position:fixed;width:10px;height:10px"></div></div></div>`))
	checkRectsFrom(t, got, map[string][]fragRect{
		"fx": {{135, 30, 10, 10}},
		"fs": {{130, 54, 10, 10}},
	}, chrome152)
}

// TestFixedContainerBetweenNestedMulticols pins the fixedpos containing
// block of a fixed box inside an inner multicol's pending OOF when the fixed
// container `t` is inside the outer multicol but outside the inner one: the
// pending multicol entry records t as its fixedpos containing block as it
// propagates out of t, with the multicol offset kept relative to t and made
// stitched by AdjustFixedposContainingBlockForInnerMulticols
// (fragment_builder.cc:842-917, :1108-1143; box_fragment_builder.cc:823-845),
// and TransferOutOfFlowCandidates makes the fixed a fragmentainer descendant
// contained by t (fragment_builder.cc:682-710 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFixedContainerBetweenNestedMulticols(t *testing.T) {
	got := oofPageRects(t, fixedContainerInMulticolProbe(
		`<div style="`+oofProbeMulticol+`width:100px;height:60px">`+
			`<div id=rel style="position:relative"><div style="height:30px"></div>`+fixedInAbs+`</div></div>`))
	checkRectsFrom(t, got, map[string][]fragRect{
		"abs": {{130, 74, 20, 20}},
		"fx":  {{135, 30, 10, 10}},
		"fs":  {{130, 74, 10, 10}},
	}, chrome152)
}

// TestContainedOOFContainingBlockIsPaddingBox pins the padding box as the
// containing block of the out-of-flow boxes a positioned multicol and table
// contain (Blink's default_containing_block_, out_of_flow_layout_part.cc:
// 562-596 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): top:0;left:0 lands
// inside the 5px border, not inside the 10px padding.
func TestContainedOOFContainingBlockIsPaddingBox(t *testing.T) {
	const box = `position:relative;padding:10px;border:5px solid;width:100px;height:50px;`
	const oofs = `<div id=s style="position:absolute;top:0;left:0;width:6px;height:6px"></div>` +
		`<div id=e style="position:absolute;bottom:0;right:0;width:6px;height:6px"></div>`
	for _, tc := range []struct {
		name, body string
		want       map[string][]fragRect
	}{
		{"multicol", `<div style="columns:2;column-fill:auto;` + box + `">` + oofs + `</div>`,
			map[string][]fragRect{"s": {{5, 5, 6, 6}}, "e": {{119, 69, 6, 6}}}},
		{"table", `<table style="border-spacing:0;` + box + `"><tr><td>` + oofs + `</td></tr></table>`,
			map[string][]fragRect{"s": {{5, 5, 6, 6}}, "e": {{89, 39, 6, 6}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font-size:0">`+tc.body)
			checkRectsFrom(t, got, tc.want, chrome152)
		})
	}
}

// TestGridAreaContainingBlock pins a positioned grid's out-of-flow
// children: one placed within a grid area is contained by the area, the
// padding edge standing in for an auto line (GetContainingBlockInfo,
// out_of_flow_layout_part.cc:798-834, :902-906;
// GridLayoutAlgorithm::ComputeOutOfFlowItemContainingRect,
// grid/grid_layout_algorithm.cc:2742-2791), and the static position is the
// area's start, the padding box's when not placed (PlaceOutOfFlowItems,
// :2602-2681, all @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The grid is
// at (0,0) with a 5px border and 10px padding; columns 30px 40px, rows 20px
// 50px.
func TestGridAreaContainingBlock(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font-size:0">`+
		`<div style="display:grid;position:relative;padding:10px;border:5px solid;width:100px;height:100px;grid-template-columns:30px 40px;grid-template-rows:20px 50px">`+
		`<div id=a style="position:absolute;width:6px;height:6px"></div>`+
		`<div id=b style="position:absolute;grid-column:2;grid-row:2;top:0;left:0;width:100%;height:4px"></div>`+
		`<div id=c style="position:absolute;grid-column:2/3;right:0;width:10px;height:10px"></div>`+
		`<div id=d style="position:absolute;top:0;left:0;width:7px;height:7px"></div>`+
		`<div id=e style="position:absolute;grid-column:2;grid-row:2;width:8px;height:8px"></div>`+
		`<div id=f style="position:absolute;grid-row:1/3;bottom:0;left:0;width:5px;height:50%"></div>`+
		`</div>`)
	checkRectsFrom(t, got, map[string][]fragRect{
		"a": {{5, 5, 6, 6}},
		"b": {{45, 35, 80, 4}},
		"c": {{75, 5, 10, 10}},
		"d": {{5, 5, 7, 7}},
		"e": {{45, 35, 8, 8}},
		"f": {{5, 50, 5, 35}},
	}, chrome152)
}

// TestRootOutOfFlowLayoutPartContentSize pins the content size the root's
// OutOfFlowLayoutPart finds inline containing blocks' line boxes in: the
// root's own content box, not the viewport its containing block is.
func TestRootOutOfFlowLayoutPartContentSize(t *testing.T) {
	ctx := &LayoutContext{ViewportWidth: 800, ViewportHeight: 600}
	wdm := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	space := ConstraintSpace{WritingDirection: wdm, IsRootElement: true}
	padding := LogicalEdges{InlineStart: 5, InlineEnd: 5, BlockStart: 2, BlockEnd: 2}
	p := NewOutOfFlowLayoutPart(ctx, NewBoxFragmentBuilder(wdm), space, LogicalSize{InlineSize: 310, BlockSize: 204}, padding, 0)
	if want := (LogicalSize{InlineSize: 300, BlockSize: 200}); p.contentSize != want {
		t.Errorf("content size = %v, want %v (the root's content box)", p.contentSize, want)
	}
	if want := (LogicalSize{InlineSize: 800, BlockSize: 600}); p.containingBlockSize != want {
		t.Errorf("containing block size = %v, want the viewport %v", p.containingBlockSize, want)
	}
}

// TestOOFStaticPositionWithPercentagePadding pins the border+padding a
// propagated static position moves past to the laid-out child's, so
// percentage padding counts: Blink's static positions are relative to the
// border box and PropagateOOFPositionedInfo adds the child's offset
// (fragment_builder.cc:762 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and
// louis14's content-box static positions add the fragment's own border and
// padding. A flex item with padding:10% of 200px puts `f` at (20,20); a
// block with padding 5% 0 0 10% of 300px, after the 90px flex container,
// puts `g` at (30,105).
func TestOOFStaticPositionWithPercentagePadding(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font-size:0">`+
		`<div style="display:flex;width:200px"><div style="padding:10%;width:50px;height:50px">`+
		`<div id=f style="position:fixed;width:5px;height:5px"></div></div></div>`+
		`<div style="width:300px"><div style="padding:5% 0 0 10%;height:20px">`+
		`<div id=g style="position:fixed;width:5px;height:5px"></div></div></div>`)
	checkRectsFrom(t, got, map[string][]fragRect{
		"f": {{20, 20, 5, 5}},
		"g": {{30, 105, 5, 5}},
	}, chrome152)
}

// chrome157 labels expected values probed with getBoundingClientRect in
// headless Chrome 157 (157.0.8103.52).
const chrome157 = "Chrome 157 getBoundingClientRect"

// TestOOFStaticPositionAlignment pins a block-level out-of-flow box with
// auto insets at its static position, aligned by its justify-self (auto: the
// parent's justify-items; normal: start) within a zero-thickness line
// across the parent's content box, and by its align-self (align-items does
// not apply in block layout) against a zero-height line, so end puts the
// whole box above its static block position. Blink sets the static
// position's edges from them (InlineStaticPositionEdge /
// BlockStaticPositionEdge, block_layout_algorithm_utils.cc:236-326, in
// BlockLayoutAlgorithm::HandleOutOfFlowPositioned,
// block_layout_algorithm.cc:1688-1716 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `a` (20x20) is the only child
// of a 75x50 block with a 5px border at the top-left of the 1px-bordered
// positioned container, so its static point is (6,6).
func TestOOFStaticPositionAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, abs, parent string
		want              fragRect
	}{
		{"start", "justify-self:start", "", fragRect{6, 6, 20, 20}},
		{"center", "justify-self:center", "", fragRect{33.5, 6, 20, 20}},
		{"end", "justify-self:end", "", fragRect{61, 6, 20, 20}},
		{"auto, justify-items:end", "", "justify-items:end", fragRect{61, 6, 20, 20}},
		{"auto, justify-items:center", "", "justify-items:center", fragRect{33.5, 6, 20, 20}},
		{"align-self:end", "align-self:end", "", fragRect{6, -14, 20, 20}},
		{"align-self:center", "align-self:center", "", fragRect{6, -4, 20, 20}},
		{"self-end, vertical-rl", "justify-self:self-end;writing-mode:vertical-rl", "", fragRect{6, 6, 20, 20}},
		{"self-end, rtl", "justify-self:self-end;direction:rtl", "", fragRect{6, 6, 20, 20}},
		{"left", "justify-self:left", "", fragRect{6, 6, 20, 20}},
		{"right, rtl parent", "justify-self:right", "direction:rtl", fragRect{6, 6, 20, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div style="position:relative;width:100px;height:100px;border:1px solid">`+
				`<div style="width:75px;height:50px;border:5px solid;`+tc.parent+`">`+
				`<div id=a style="position:absolute;width:20px;height:20px;`+tc.abs+`"></div></div></div>`)
			checkRectsFrom(t, got, map[string][]fragRect{"a": {tc.want}}, chrome157)
		})
	}
}

// TestOOFInsetAlignment pins an out-of-flow box whose insets are both set
// in an axis: it is aligned within its inset-modified containing block by
// justify-self (the containing block's inline axis, even for an orthogonal
// box) and align-self. Self-start / self-end take the box's own edges,
// left / right the containing block's direction, and an overflowing box
// (end, safe end or center) is shifted back to the containing block's
// start. Mirrors GetAlignmentInsetBias (absolute_utils.cc:51-104) and
// ComputeAlignment (:495-542), as ComputeUnclampedIMCB (:196) and
// ComputeInsets (:326) use them @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
// `a` (20x20, inset:0) is in a 100x100 positioned container with a 1px
// border.
func TestOOFInsetAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, abs, container string
		want                 fragRect
	}{
		{"start", "justify-self:start", "", fragRect{1, 1, 20, 20}},
		{"center", "justify-self:center", "", fragRect{41, 1, 20, 20}},
		{"end", "justify-self:end", "", fragRect{81, 1, 20, 20}},
		{"align-self:center", "align-self:center", "", fragRect{1, 41, 20, 20}},
		{"align-self:end", "align-self:end", "", fragRect{1, 81, 20, 20}},
		{"end, vertical-rl box", "justify-self:end;writing-mode:vertical-rl", "", fragRect{81, 1, 20, 20}},
		{"align-self:end, vertical-rl box", "align-self:end;writing-mode:vertical-rl", "", fragRect{1, 81, 20, 20}},
		{"self-end, rtl box", "justify-self:self-end;direction:rtl", "", fragRect{1, 1, 20, 20}},
		{"self-end, vertical-rl box", "justify-self:self-end;writing-mode:vertical-rl", "", fragRect{1, 1, 20, 20}},
		{"left", "justify-self:left", "", fragRect{1, 1, 20, 20}},
		{"right, rtl container", "justify-self:right", "direction:rtl", fragRect{81, 1, 20, 20}},
		{"end, overflowing", "justify-self:end;width:150px", "", fragRect{1, 1, 150, 20}},
		{"safe end, overflowing", "justify-self:safe end;width:150px", "", fragRect{1, 1, 150, 20}},
		{"center, overflowing", "justify-self:center;width:150px", "", fragRect{1, 1, 150, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div style="position:relative;width:100px;height:100px;border:1px solid;`+tc.container+`">`+
				`<div id=a style="position:absolute;inset:0;width:20px;height:20px;`+tc.abs+`"></div></div>`)
			checkRectsFrom(t, got, map[string][]fragRect{"a": {tc.want}}, chrome157)
		})
	}
}

// TestOOFInlineStaticPositionAlignment pins an out-of-flow box in a line
// (here after "XX ", in a 25px-tall line of 10px Ahem) at its static
// position aligned by its justify-self within a zero-thickness line in the
// inline direction (a block-level box sits on the next line, at the line's
// start) and by its align-self within the line box's block extent: a center
// or end block edge moves its static point to the line box's middle or end.
// justify-items / align-items do not apply in inline layout. Self-start /
// self-end resolve against the box's line item's container direction: for
// a block-level box its block container's, for an inline-level box the
// direction of its own bidi level, not the `direction` of the inline
// element around it (span: an RTL <span> around an LTR box). Blink sets the
// edges in FragmentBuilder::AddOutOfFlowInlineChildCandidate
// (fragment_builder.cc:545-585) from LogicalLineBuilder's OOF container
// direction (logical_line_builder.cc:312-322 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `a` (20x20) is in a 100x100
// positioned container with a 1px border.
func TestOOFInlineStaticPositionAlignment(t *testing.T) {
	const rtlSpan = "direction:rtl"
	for _, tc := range []struct {
		name, abs, span string
		want            fragRect
	}{
		{"start", "", "", fragRect{1, 26, 20, 20}},
		{"justify-self:end", "justify-self:end", "", fragRect{-19, 26, 20, 20}},
		{"justify-self:center", "justify-self:center", "", fragRect{-9, 26, 20, 20}},
		{"align-self:end", "align-self:end", "", fragRect{1, 31, 20, 20}},
		{"align-self:center", "align-self:center", "", fragRect{1, 28.5, 20, 20}},
		{"self-end, rtl box", "justify-self:self-end;direction:rtl", "", fragRect{1, 26, 20, 20}},
		{"self-start, ltr box in an rtl span", "justify-self:self-start;direction:ltr", rtlSpan, fragRect{1, 26, 20, 20}},
		{"self-end, ltr box in an rtl span", "justify-self:self-end;direction:ltr", rtlSpan, fragRect{-19, 26, 20, 20}},
		{"inline-level self-start, ltr box in an rtl span", "display:inline-block;justify-self:self-start;direction:ltr", rtlSpan, fragRect{21, 1, 20, 20}},
		{"inline-level self-end, ltr box in an rtl span", "display:inline-block;justify-self:self-end;direction:ltr", rtlSpan, fragRect{1, 1, 20, 20}},
		{"align-self:end, inline-level", "align-self:end;display:inline-block", "", fragRect{21, 6, 20, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div style="position:relative;width:100px;height:100px;border:1px solid">`+
				`<span style="font:10px/25px Ahem;`+tc.span+`">XX <div id=a style="position:absolute;width:20px;height:20px;`+tc.abs+`"></div><br>XX</span></div>`)
			checkRectsFrom(t, got, map[string][]fragRect{"a": {tc.want}}, chrome157)
		})
	}
}

// TestOOFGridStaticPositionAlignment pins an out-of-flow child of a grid
// that is its containing block at its static position aligned within its
// grid area (the padding box when not placed) by justify-self / align-self,
// auto taking the grid's justify-items / align-items, auto margins not
// applying, in each axis with auto insets. Blink moves the static position
// to the area's middle or end (AlignmentOffsetForOutOfFlow,
// grid/grid_layout_utils.cc:466-503, in GridLayoutAlgorithm::
// PlaceOutOfFlowItems, grid/grid_layout_algorithm.cc:2602-2681 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). `a` (20x20) is in a
// 100x100 positioned grid with a 1px border.
func TestOOFGridStaticPositionAlignment(t *testing.T) {
	for _, tc := range []struct {
		name, grid, abs string
		want            fragRect
	}{
		{"items center", "justify-items:center;align-items:center", "", fragRect{41, 41, 20, 20}},
		{"self end", "", "justify-self:end;align-self:end", fragRect{81, 81, 20, 20}},
		{"inline insets set", "justify-items:center;align-items:center", "left:0;right:0", fragRect{1, 41, 20, 20}},
		{"block insets set", "justify-items:center;align-items:center", "top:0;bottom:0", fragRect{41, 1, 20, 20}},
		{"in an area", "grid-template-columns:40px 60px;grid-template-rows:30px 70px", "grid-column:2;grid-row:2;justify-self:center;align-self:end", fragRect{61, 81, 20, 20}},
		{"auto margins ignored", "justify-items:center", "margin-left:auto", fragRect{41, 1, 20, 20}},
		{"self-end, rtl box", "", "justify-self:self-end;direction:rtl", fragRect{1, 1, 20, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
				`<div style="display:grid;position:relative;width:100px;height:100px;border:1px solid;`+tc.grid+`">`+
				`<div id=a style="position:absolute;width:20px;height:20px;`+tc.abs+`"></div></div>`)
			checkRectsFrom(t, got, map[string][]fragRect{"a": {tc.want}}, chrome157)
		})
	}
}

// TestOOFBlockLevelStaticPositionVisualOrder pins which line a block-level
// out-of-flow box in a line takes: the next one only when inline content
// precedes it in the line's visual order (after bidi reordering), walked
// from the line's start. In an RTL run inside an LTR line, a box after the
// run's text is visually before it, so it stays on the line. The box is
// resolved for bidi as a U+FFFC neutral, taking the run's level, and an
// inline box's open and close tags are not inline content. Mirrors
// PlaceOutOfFlowObjects (inline/inline_layout_algorithm.cc:760-855),
// InlineNode::SegmentBidiRuns (inline/inline_node.cc:1361-1430) and
// LogicalLineItem::HasInFlowFragment (inline/logical_line_item.h:162-166 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). want is from Chrome headless.
func TestOOFBlockLevelStaticPositionVisualOrder(t *testing.T) {
	for _, tc := range []struct {
		name, span string
		want       fragRect
	}{
		{"rtl embedding", "direction:rtl;unicode-bidi:embed", fragRect{1, 1, 20, 20}},
		{"rtl isolate", "direction:rtl;unicode-bidi:isolate", fragRect{1, 1, 20, 20}},
		{"control: ltr", "", fragRect{1, 26, 20, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font:10px/25px serif">`+
				`<div style="position:relative;width:200px;height:100px;border:1px solid">`+
				`<span style="`+tc.span+`">אבג <div id=a style="position:absolute;width:20px;height:20px"></div></span>X</div>`)
			checkRectsFrom(t, got, map[string][]fragRect{"a": {tc.want}}, chrome157)
		})
	}
}
