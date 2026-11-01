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
