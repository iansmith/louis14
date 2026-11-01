package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"louis14/pkg/html"
)

// oofPageRects parses src, lays it out at 800x600 and returns, per id, the
// page-space rect of every box the element generated (one per fragment), in
// box-tree order.
func oofPageRects(t *testing.T, src string) map[string][]fragRect {
	t.Helper()
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	out := map[string][]fragRect{}
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil && b.Node.Type == html.ElementNode {
			if id, ok := b.Node.GetAttribute("id"); ok {
				out[id] = append(out[id], fragRect{b.X, b.Y, b.Width, b.Height})
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	return out
}

// oofProbeMulticol is the column-count:2; column-gap:0; column-fill:auto
// multicol the OOF-fragmentation probes are built on.
const oofProbeMulticol = "column-count:2;column-gap:0;column-fill:auto;"

// TestOOFStaticPositionIsFragmentLocal pins Blink's fragment-local static
// position for an OOF whose containing block is outside the fragmentation
// context. The abspos follows a 120px block inside a non-positioned wrapper
// that breaks at 100, so its static position is 20px into column 2 and,
// with the ICB as its containing block, that is where it is placed. Blink
// records (BorderScrollbarPadding().inline_start,
// previous_inflow_position.logical_block_offset) with no consumed
// block-size (block_layout_algorithm.cc:1654-1656 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); the static position is made
// stitched only for a fragmentainer descendant, at its containing block, by
// BoxFragmentBuilder::AdjustFragmentainerDescendant
// (box_fragment_builder.cc:781-809 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Chrome 155 getClientRects: a = [50,20,50,80] (plan probe pA).
func TestOOFStaticPositionIsFragmentLocal(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="`+oofProbeMulticol+`width:100px;height:100px"><div>`+
		`<div style="height:120px"></div>`+
		`<div id=a style="position:absolute;width:50px;height:80px"></div>`+
		`</div></div>`)
	checkRects(t, got, map[string][]fragRect{"a": {{50, 20, 50, 80}}})
}

// TestLayoutFragmentainerDescendants pins Blink's layout of fragmentainer
// descendants at the fragmentation context root (plan probe pB): the
// containing block `rel` (200px of in-flow content) is fragmented across
// both columns of a 50x100 multicol, and its abspos descendants are laid
// out per column by OutOfFlowLayoutPart::LayoutFragmentainerDescendants
// (out_of_flow_layout_part.cc:1559-1784 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - a1 (auto height, 200px content, static position at stitched 200)
//     starts past the last column, so new columns are created for it
//     (LayoutOOFsInFragmentainer → ColumnLayoutAlgorithm::CreateEmptyColumn,
//     out_of_flow_layout_part.cc:2955-2973, column_layout_algorithm.cc:500-519)
//     and it is really laid out, fragmented, in them (GenerateFragment,
//     :2793-2902);
//   - fx, a fixed child of a1, is a regular candidate whose static position
//     is the end of a1's last fragment (AddOOFToFragmentainer, :3028-3152);
//   - a2 (top:150, 100 tall) starts in column 2 and continues in a new one
//     (ComputeStartFragmentIndexAndRelativeOffset, :3183-3250);
//   - a3 (bottom:0) is resolved against the stitched CB block-size.
//
// Chrome 155 getClientRects.
func TestLayoutFragmentainerDescendants(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<div style="`+oofProbeMulticol+`width:50px;height:100px">`+
		`<div style="position:relative"><div style="height:200px"></div>`+
		`<div id=a1 style="position:absolute;width:25px"><div style="height:200px"></div>`+
		`<div id=fx style="position:fixed;width:10px;height:10px"></div></div>`+
		`<div id=a2 style="position:absolute;top:150px;left:0;width:10px;height:100px"></div>`+
		`<div id=a3 style="position:absolute;bottom:0;left:0;width:10px;height:50px"></div>`+
		`</div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"a1": {{50, 0, 25, 100}, {75, 0, 25, 100}},
		"fx": {{75, 100, 10, 10}},
		"a2": {{25, 50, 10, 50}, {50, 0, 10, 50}},
		"a3": {{25, 50, 10, 50}},
	})
}

// TestOOFsParticipateInColumnBalancing pins Blink's OOF-aware column
// balancing: while balancing a column line, the fragmentainer descendants
// of the new columns are laid out in a dry run (a ColumnBalancingInfo,
// out_of_flow_layout_part.h:47-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) that reports the new columns
// they would need and their minimal space shortage, so the columns stretch
// to hold them (column_layout_algorithm.cc:1087-1129 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Before a spanner the
// fragmentainer descendants found so far are laid out in the line's columns
// (HandleFragmentation before LayoutSpanner, column_layout_algorithm.cc:
// 640-643 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not in the columns
// after the spanner. Chrome 155 getClientRects.
func TestOOFsParticipateInColumnBalancing(t *testing.T) {
	t.Run("balanced", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div id=mc style="column-count:2;column-gap:0;width:100px">`+
			`<div style="position:relative"><div style="height:40px"></div>`+
			`<div id=a style="position:absolute;width:50px;height:160px;top:0"></div>`+
			`</div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"mc": {{0, 0, 100, 80}},
			"a":  {{0, 0, 50, 80}, {50, 0, 50, 80}},
		})
	})
	t.Run("before a spanner", func(t *testing.T) {
		got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
			`<div style="column-count:2;column-fill:auto;column-gap:0;width:100px">`+
			`<div style="position:relative"><div style="height:100px"></div>`+
			`<div id=a style="position:absolute;width:50px;height:200px;top:0"></div></div>`+
			`<div id=sp style="column-span:all"></div><div style="height:100px"></div></div>`)
		checkRects(t, got, map[string][]fragRect{
			"a":  {{0, 0, 50, 100}, {50, 0, 50, 100}},
			"sp": {{0, 100, 100, 0}},
		})
	})
}

// TestOOFsInsideColumnSpanner pins Blink's handling of OOFs inside a column
// spanner (plan probe pD): the spanner is laid out by the multicol and added
// with AddResult (column_layout_algorithm.cc:1427 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which propagates its OOF
// descendants to the multicol (fragment_builder.cc:357-405). Searching for a
// containing block jumps from a spanner straight to the multicol
// (FindColumnSpannerContainer, layout_object.cc:166-202), so the
// position:relative ancestor inside the multicol is not the containing
// block: `a` is placed against the ICB and `s` at its static position, the
// spanner's content-box start. Chrome 155 getClientRects.
func TestOOFsInsideColumnSpanner(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="height:30px"></div>`+
		`<div style="columns:3;column-gap:0;width:300px;margin-left:20px">`+
		`<div style="position:relative;margin-top:7px"><div style="column-span:all;height:50px">`+
		`<div id=a style="position:absolute;top:5px;left:5px;width:10px;height:10px"></div>`+
		`<div id=s style="position:absolute;width:10px;height:10px"></div>`+
		`</div></div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"a": {{5, 5, 10, 10}},
		"s": {{20, 37, 10, 10}},
	})
}

// TestSpannerTagsFragmentainerDescendants pins the column-spanner tag on a
// fragmentainer descendant propagated out of a spanner: Blink's
// PropagateOOFFragmentainerDescendants sets is_inside_column_spanner when
// the propagating fragment is a column spanner (fragment_builder.cc:974-975
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the inner multicol's
// pending-OOF layout leaves the descendant to the next fragmentation context
// (LayoutOOFsInMulticol, out_of_flow_layout_part.cc:1440-1449).
func TestSpannerTagsFragmentainerDescendants(t *testing.T) {
	spanner := makeNode("div")
	styles := map[*html.Node]*css.Style{spanner: makeStyle("display", "block", "column-span", "all")}
	spannerNode := buildTestTree(spanner, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	spannerFragment := &PhysicalFragment{
		Node:       spanner,
		Style:      spannerNode.Style(),
		LayoutNode: spannerNode,
		FragmentedOofData: &FragmentedOofData{
			OofPositionedFragmentainerDescendants: []LogicalOofNodeForFragmentation{{}},
		},
	}
	builder := NewBoxFragmentBuilder(wdm)
	builder.PropagateOOFFragmentainerDescendants(spannerFragment, LogicalOffset{}, LogicalOffset{}, layoutunit.LayoutUnit{}, nil, nil, nil)
	var got []LogicalOofNodeForFragmentation
	builder.SwapOutOfFlowFragmentainerDescendants(&got)
	if len(got) != 1 || !got[0].ContainingBlock.IsInsideColumnSpanner {
		t.Errorf("descendants %+v, want one with ContainingBlock.IsInsideColumnSpanner", got)
	}
}

// TestTransformedMulticolIsFixedContainer pins a transformed multicol as the
// containing block of its fixed-position descendants (plan probe pC): Blink
// derives IsFixedContainer from the box's style for every box kind
// (LayoutObject::ComputeIsFixedContainer, layout_object.cc:1849-1900 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) and the multicol's
// OutOfFlowLayoutPart reads it (out_of_flow_layout_part.cc:544-545). `f`
// (top:10px; left:5px) is placed against the multicol's padding box at
// (30,20); `fs` keeps its static position after a 150px block, in column 2.
// Chrome 155 getClientRects.
func TestTransformedMulticolIsFixedContainer(t *testing.T) {
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0"><div style="height:20px"></div>`+
		`<div style="`+oofProbeMulticol+`width:100px;height:100px;margin-left:30px;transform:translateX(0)">`+
		`<div style="height:150px"></div>`+
		`<div id=f style="position:fixed;top:10px;left:5px;width:10px;height:10px"></div>`+
		`<div id=fs style="position:fixed;width:10px;height:10px"></div></div>`)
	checkRects(t, got, map[string][]fragRect{
		"f":  {{35, 30, 10, 10}},
		"fs": {{80, 70, 10, 10}},
	})
}
