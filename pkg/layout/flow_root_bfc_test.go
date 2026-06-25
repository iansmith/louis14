package layout

// Indicator tests for LOU-216: display:flow-root BFC propagation.
//
// These tests assert three contracts required for the WPT css-display/
// display-flow-root-* tests:
//
//  1. display:flow-root list-item establishes a new formatting context
//     (createsFormattingContext must delegate to style.EstablishesNewFormattingContext).
//  2. A flow-root suppresses parent-child margin collapsing (margins are
//     contained, not propagated outward).
//  3. A flow-root's auto block-size extends to contain its float children
//     (float containment: hasOwnFloats → Gate B clears floats).
//
// Blink vetting log: verified against Chromium main @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
// Relevant Blink source:
//   third_party/blink/renderer/core/layout/block_layout_algorithm.cc — Layout()
//   margin-strut suppression at BFC root entry (~line 1180):
//     "Do not collapse margins ... B) This is a new formatting context."
//   float clearance for BFC auto-height at FinishLayout (~line 3200):
//     the float-clearance extension (css21 §10.6.7) via ExclusionSpace::ClearanceOffset.

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestFlowRoot_ListItemCreatesNewFormattingContext verifies that
// "display: flow-root list-item" establishes a new BFC.
//
// Mirrors WPT css-display/display-flow-root-list-item-001.html which uses
// `.li { display: flow-root list-item; }`. GetDisplay() folds this to
// DisplayListItem, so createsFormattingContext must call
// style.EstablishesNewFormattingContext() rather than checking only
// GetDisplay() == DisplayFlowRoot.
//
// Blink analog: BlockLayoutAlgorithm builds child constraint space with
// is_new_formatting_context=true for any child whose computed style has
// CreatesNewFormattingContext() returning true, which checks the inner
// "flow-root" keyword regardless of the outer keyword.
// third_party/blink/renderer/core/layout/block_layout_algorithm.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
func TestFlowRoot_ListItemCreatesNewFormattingContext(t *testing.T) {
	t.Run("display-flow-root-list-item-001", func(t *testing.T) {
		// createsFormattingContext must return true for display:flow-root list-item.
		// GetDisplay() folds "flow-root list-item" → DisplayListItem, so the
		// plain GetDisplay()==DisplayFlowRoot check misses it.
		liStyle := css.NewStyle()
		liStyle.Properties["display"] = "flow-root list-item"

		if !createsFormattingContext(liStyle) {
			t.Errorf("createsFormattingContext: display:flow-root list-item should return true; got false")
		}
	})
}

// TestFlowRoot_MarginCollapseSuppressionWithChild verifies that a
// display:flow-root element suppresses parent-child margin collapsing.
//
// CSS Display §8 / CSS 2.1 §8.3.1: A block formatting context root does
// not collapse its margins with its children's margins.
//
// Mirrors WPT display-flow-root-001.html scenario 1:
//
//	<div style="display:flow-root"><div style="margin-top:20px; height:10px"></div></div>
//
// The flow-root must NOT propagate the child's margin outward.
// The inner div must be at block offset 20px (margin absorbed) inside
// the flow-root, and the flow-root's height must be 30px (20+10).
//
// Blink analog: PreviousInflowPosition initialization in
// BlockLayoutAlgorithm::Layout checks is_new_formatting_context to
// suppress margin-strut propagation at the BFC boundary.
// third_party/blink/renderer/core/layout/block_layout_algorithm.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
func TestFlowRoot_MarginCollapseSuppressionWithChild(t *testing.T) {
	t.Run("display-flow-root-001-margin-no-collapse", func(t *testing.T) {
		// A parent block (display:block) contains a flow-root child.
		// The flow-root child contains an inner div with margin-top:20px.
		// After layout, the flow-root child fragment (at Children[0] of parent)
		// must: have height=30px (child margin 20 + child height 10), and
		// the parent must NOT propagate any top margin upward.
		innerDiv := &html.Node{Type: html.ElementNode, TagName: "div"}
		flowRoot := &html.Node{
			Type:     html.ElementNode,
			TagName:  "div",
			Children: []*html.Node{innerDiv},
		}
		innerDiv.Parent = flowRoot
		outer := &html.Node{
			Type:     html.ElementNode,
			TagName:  "div",
			Children: []*html.Node{flowRoot},
		}
		flowRoot.Parent = outer

		styles := map[*html.Node]*css.Style{
			outer:    makeStyle("display", "block", "width", "400px"),
			flowRoot: makeStyle("display", "flow-root"),
			innerDiv: makeStyle("display", "block", "margin-top", "20px", "height", "10px"),
		}

		ctx := testContext()
		layoutRoot := buildTestTree(outer, styles)
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			Build()

		outerResult := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
		if outerResult == nil || outerResult.Fragment == nil {
			t.Fatal("outer layout returned nil")
		}

		if len(outerResult.Fragment.Children) == 0 {
			t.Fatal("outer fragment has no children")
		}

		// The flow-root child is at Children[0] of the outer fragment.
		flowRootFrag := outerResult.Fragment.Children[0]
		flowRootHeight := flowRootFrag.Fragment.Size.HeightF64()
		if flowRootHeight != 30 {
			t.Errorf("flow-root height = %v; want 30 (20px child margin-top contained + 10px child height); "+
				"margin must not escape the flow-root BFC", flowRootHeight)
		}

		// The flow-root must NOT have leaked margins upward (the outer block
		// should position it at blockOffset=0, not at negative to compensate
		// for a leaked margin). The flow-root child fragment block offset
		// in the outer container must be 0.
		flowRootBlockOffset := flowRootFrag.Offset.TopF64()
		if flowRootBlockOffset != 0 {
			t.Errorf("flow-root block offset in outer = %v; want 0 (no leaked margin from BFC root)", flowRootBlockOffset)
		}
	})
}

// TestFlowRoot_ContainsChildFloats verifies that a display:flow-root
// element's auto block-size extends to contain its float children.
//
// CSS 2.1 §10.6.7: For BFC roots, auto block-size extends to clear floats.
// Mirrors WPT display-flow-root-001.html scenario 2:
//
//	<div style="display:flow-root; width:200px"><div style="float:left; width:20px; height:40px"></div></div>
//
// The flow-root must grow to height=40px to contain the float.
//
// Blink analog: float-clearance extension in the auto-height pass (Gate B in
// block_layout.go mirrors Blink's css21 §10.6.7 computation via
// ExclusionSpace::ClearanceOffset in block_layout_algorithm.cc FinishLayout).
// third_party/blink/renderer/core/layout/block_layout_algorithm.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
func TestFlowRoot_ContainsChildFloats(t *testing.T) {
	t.Run("display-flow-root-001-float-containment", func(t *testing.T) {
		// A parent block (display:block) contains a flow-root child (display:flow-root)
		// that in turn contains a left float (20px × 40px).
		// The flow-root has no other in-flow children (auto height).
		// After layout, the flow-root fragment height must be 40px.
		floatDiv := &html.Node{Type: html.ElementNode, TagName: "div"}
		flowRoot := &html.Node{
			Type:     html.ElementNode,
			TagName:  "div",
			Children: []*html.Node{floatDiv},
		}
		floatDiv.Parent = flowRoot
		outer := &html.Node{
			Type:     html.ElementNode,
			TagName:  "div",
			Children: []*html.Node{flowRoot},
		}
		flowRoot.Parent = outer

		styles := map[*html.Node]*css.Style{
			outer:    makeStyle("display", "block", "width", "400px"),
			flowRoot: makeStyle("display", "flow-root", "width", "200px"),
			floatDiv: makeStyle("float", "left", "width", "20px", "height", "40px"),
		}

		ctx := testContext()
		layoutRoot := buildTestTree(outer, styles)
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			Build()

		outerResult := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
		if outerResult == nil || outerResult.Fragment == nil {
			t.Fatal("outer layout returned nil")
		}

		if len(outerResult.Fragment.Children) == 0 {
			t.Fatal("outer fragment has no children")
		}

		// The flow-root fragment is Children[0] of the outer block.
		flowRootFrag := outerResult.Fragment.Children[0]
		flowRootHeight := flowRootFrag.Fragment.Size.HeightF64()
		if flowRootHeight != 40 {
			t.Errorf("flow-root height = %v; want 40 (auto-height must extend to contain float children per CSS 2.1 §10.6.7)", flowRootHeight)
		}
	})
}

// TestFlowRoot_AvoidsAdjacentFloat verifies that a display:flow-root element
// is positioned to avoid overlapping an adjacent float sibling.
//
// CSS 2.1 §9.5 / §10.3.3: A BFC element must not overlap float margin boxes.
// When a left float (20px wide) precedes a display:flow-root sibling, the
// flow-root's inline start offset must be >= 20px (the float's inline size).
//
// Mirrors WPT display-flow-root-001.html scenario 3:
//
//	<div style="border:1px solid; margin-bottom:20px">
//	  <div class="float"></div>         <!-- float:left; width:20px; height:40px -->
//	  <span style="display:flow-root; border:1px solid">x</span>
//	</div>
//
// Blink analog: BlockLayoutAlgorithm::Layout places a new-FC child using
// ExclusionSpace::FindAvailableInlineSize to get floatStartOff, then adds
// it to childInlineOffset.
// third_party/blink/renderer/core/layout/block_layout_algorithm.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f
func TestFlowRoot_AvoidsAdjacentFloat(t *testing.T) {
	t.Run("display-flow-root-001-float-avoidance", func(t *testing.T) {
		// outer (display:block, 400px wide, border:1px) contains:
		//   floatDiv (float:left, 20px x 40px)
		//   flowRoot (display:flow-root, border:1px solid)
		//
		// CSS 2.1 §9.5: the flow-root must be positioned to the right of the float.
		// Its inline offset relative to the outer's content box must be >= 20px.
		floatDiv := &html.Node{Type: html.ElementNode, TagName: "div"}
		flowRoot := &html.Node{Type: html.ElementNode, TagName: "span"}
		outer := &html.Node{
			Type:     html.ElementNode,
			TagName:  "div",
			Children: []*html.Node{floatDiv, flowRoot},
		}
		floatDiv.Parent = outer
		flowRoot.Parent = outer

		styles := map[*html.Node]*css.Style{
			outer:    makeStyle("display", "block", "width", "400px", "border", "1px solid"),
			floatDiv: makeStyle("float", "left", "width", "20px", "height", "40px"),
			flowRoot: makeStyle("display", "flow-root", "border", "1px solid"),
		}

		ctx := testContext()
		layoutRoot := buildTestTree(outer, styles)
		wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
		space := NewConstraintSpaceBuilder(wdm, wdm, true).
			SetAvailableSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			SetPercentageResolutionSize(LogicalSize{InlineSize: 400, BlockSize: Indefinite}).
			Build()

		outerResult := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
		if outerResult == nil || outerResult.Fragment == nil {
			t.Fatal("outer layout returned nil")
		}

		// Find the flow-root fragment among the outer's children.
		// The float fragment is first; the flow-root fragment follows.
		// We identify the flow-root fragment by its display:flow-root style.
		var flowRootFragOffset float64
		var found bool
		for _, ch := range outerResult.Fragment.Children {
			if ch.Fragment == nil {
				continue
			}
			// The float is 20px wide. The flow-root is the non-float child
			// that should be positioned at InlineOffset >= 20.
			// We check all children and find the one with the larger InlineOffset.
			inlineOff := ch.Offset.LeftF64()
			if !found || inlineOff > flowRootFragOffset {
				flowRootFragOffset = inlineOff
				found = true
			}
		}

		if !found {
			t.Fatal("outer fragment has no children")
		}

		// The flow-root's inline start offset must be >= 20px (the float width)
		// so it doesn't overlap the float.
		if flowRootFragOffset < 20 {
			t.Errorf("flow-root inline offset = %v; want >= 20 (must be positioned after the 20px-wide float per CSS 2.1 §9.5)", flowRootFragOffset)
		}
	})
}

// TestNewFC_TooWideForFloatDropsBelowAndResetsInline verifies that a new-FC
// box (here display:flex) that is too wide to fit beside an intruding float
// is pushed BELOW the float AND reset to the float-free inline position
// (InlineOffset == 0), not left shifted by the float's inline size.
//
// This is the LOU-326 bug: a 480px flex container can't fit beside a 150px
// left float in a 600px container, so it must drop below the float. Blink's
// BlockLayoutAlgorithm::LayoutNewFormattingContext iterates layout
// opportunities; once pushed past the float's block range it lands in the
// full-width opportunity whose rect.LineStartOffset() == origin line offset,
// so child_bfc_offset.line_offset is 0 (X=0), not the float-shifted position.
//
// Mirrors WPT css-flexbox/flexbox_fbfc.html (#float{width:25%;float:left} +
// #flex{width:80%;display:flex} in a 600px body): "Yellow box should be below
// the blue box" with the flex border box at X=0.
//
// Blink analog: BlockLayoutAlgorithm::LayoutNewFormattingContext opportunity
// loop — line_left_offset = opportunity.rect.LineStartOffset(), and the
// below-float opportunity has no line-left floats.
// third_party/blink/renderer/core/layout/block_layout_algorithm.cc:2082-2225 @
// d694f1edc784ebb2ce84dedde5ae3905d50c14f2
func TestNewFC_TooWideForFloatDropsBelowAndResetsInline(t *testing.T) {
	// outer (display:block, 600px wide) contains:
	//   floatDiv (float:left, 150px x 20px)
	//   flex     (display:flex, 480px wide)
	//
	// 480 (flex) > 600 - 150 (space beside float) = 450, so the flex box
	// must drop below the float. After dropping, it must reset to X=0.
	floatDiv := &html.Node{Type: html.ElementNode, TagName: "div"}
	flex := &html.Node{Type: html.ElementNode, TagName: "div"}
	outer := &html.Node{
		Type:     html.ElementNode,
		TagName:  "div",
		Children: []*html.Node{floatDiv, flex},
	}
	floatDiv.Parent = outer
	flex.Parent = outer

	styles := map[*html.Node]*css.Style{
		outer:    makeStyle("display", "block", "width", "600px"),
		floatDiv: makeStyle("float", "left", "width", "150px", "height", "20px"),
		flex:     makeStyle("display", "flex", "width", "480px", "height", "30px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(outer, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 600, BlockSize: Indefinite}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 600, BlockSize: Indefinite}).
		Build()

	outerResult := NewBlockLayoutAlgorithm(ctx, layoutRoot, space).Layout()
	if outerResult == nil || outerResult.Fragment == nil {
		t.Fatal("outer layout returned nil")
	}

	// Find the flex fragment: it's the 480px-wide child.
	var flexLeft, flexTop float64
	found := false
	for _, ch := range outerResult.Fragment.Children {
		if ch.Fragment == nil {
			continue
		}
		if ch.Fragment.Size.WidthF64() == 480 {
			flexLeft = ch.Offset.LeftF64()
			flexTop = ch.Offset.TopF64()
			found = true
		}
	}
	if !found {
		t.Fatal("did not find the 480px-wide flex fragment among outer's children")
	}

	// Must be pushed below the 20px-tall float.
	if flexTop < 20 {
		t.Errorf("flex block offset = %v; want >= 20 (must drop below the 20px float per CSS 2.1 §9.5)", flexTop)
	}
	// Must reset to the float-free inline position once below the float.
	if flexLeft != 0 {
		t.Errorf("flex inline offset = %v; want 0 (once below the float, no float intrudes so the new-FC box returns to the line-left edge)", flexLeft)
	}
}
