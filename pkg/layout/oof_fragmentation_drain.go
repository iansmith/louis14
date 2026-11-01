package layout

import "louis14/pkg/css"

// Phase 25 Cmt-3: OOF-fragmentation drain at the fragmentation context root.
// Descendants whose containing block is in this multicol's own column flow
// are laid out by fragmentationContextOOFPart.LayoutFragmentainerDescendants
// (out_of_flow_layout.go), a port of Blink's. The inner-multicol branch below
// (layoutFragmentainerDescendant) is still the louis14 heuristic: it serves
// a containing block inside a nested inner multicol (multicol-nested-032),
// handles explicit-size OOFs only, and paints synthesized pieces. Plan WI-7
// replaces it with Blink's HandleMulticolsWithPendingOOFs /
// LayoutOOFsInMulticol (out_of_flow_layout_part.cc:1293-1557 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).

// HandleOofFragmentation is the entry point Blink's OutOfFlowLayoutPart::Run
// calls before LayoutCandidates. Drains pending fragmentainer descendants and
// MulticolsWithPendingOOFs entries on `builder` when it is the block
// fragmentation context root. No-op otherwise. isContainingBlockUnfinished
// (nil once layout is complete) reports a containing block that continues
// past this point; its descendants are delayed (see
// fragmentationContextOOFPart).
func (mla *MulticolLayoutAlgorithm) HandleOofFragmentation(builder *BoxFragmentBuilder, isContainingBlockUnfinished func(*LayoutInputNode) bool) {
	if !builder.IsBlockFragmentationContextRoot() {
		return
	}
	if !builder.HasOutOfFlowFragmentainerDescendants() &&
		!builder.HasMulticolsWithPendingOOFs() {
		return
	}

	var descendants []LogicalOofNodeForFragmentation
	builder.SwapOutOfFlowFragmentainerDescendants(&descendants)
	var pendingMulticols map[*LayoutInputNode]*MulticolWithPendingOOFs
	builder.SwapMulticolsWithPendingOOFs(&pendingMulticols)

	// A descendant whose containing block is inside a nested inner multicol
	// goes through the inner-multicol branch (plan WI-7 ports Blink's
	// HandleMulticolsWithPendingOOFs for it); the others are laid out in
	// this multicol's columns by LayoutFragmentainerDescendants.
	var outer []LogicalOofNodeForFragmentation
	for _, d := range descendants {
		if d.ContainingBlock.Fragment == nil {
			// Should not happen post-propagation — the parent fragment walk
			// always backfills CB.Fragment. Skip defensively.
			continue
		}
		// A descendant whose containing block is inside a column spanner is
		// not laid out in the inner multicol (LayoutOOFsInMulticol,
		// out_of_flow_layout_part.cc:1440-1449 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
		if innerMcNode := mla.findInnerMulticolForCB(builder, d.ContainingBlock.Fragment, pendingMulticols); innerMcNode != nil && !d.ContainingBlock.IsInsideColumnSpanner {
			mla.layoutFragmentainerDescendant(builder, d, innerMcNode)
			continue
		}
		outer = append(outer, d)
	}
	if len(outer) == 0 {
		return
	}
	part := mla.newFragmentationContextOOFPart(builder)
	part.isContainingBlockUnfinished = isContainingBlockUnfinished
	part.HandleFragmentation(outer, mla.fragmentainerProgression())
}

// newFragmentationContextOOFPart returns the OutOfFlowLayoutPart that lays
// out fragmentainer descendants in this multicol's columns.
func (mla *MulticolLayoutAlgorithm) newFragmentationContextOOFPart(builder *BoxFragmentBuilder) *fragmentationContextOOFPart {
	return &fragmentationContextOOFPart{
		OutOfFlowLayoutPart: OutOfFlowLayoutPart{ctx: mla.ctx, containingBlockWDM: mla.space.WritingDirection},
		containerBuilder:    builder,
	}
}

// fragmentainerProgression is the offset from one column to the next.
// Mirrors Blink's GetFragmentainerProgression for kFragmentColumn
// (fragmentation_utils.cc:310-319 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (mla *MulticolLayoutAlgorithm) fragmentainerProgression() LogicalOffset {
	return LogicalOffset{InlineOffset: mla.usedColWidth + mla.columnGapSize}
}

// layoutFragmentainerDescendant lays out a single deferred OOF descendant
// whose containing block is inside the nested inner multicol innerMcNode,
// slicing an explicit-size OOF across the inner multicol's columns as
// synthesized pieces (louis14 heuristic; plan WI-7).
func (mla *MulticolLayoutAlgorithm) layoutFragmentainerDescendant(
	builder *BoxFragmentBuilder,
	d LogicalOofNodeForFragmentation,
	innerMcNode *LayoutInputNode,
) {
	innerCols := mla.findInnerColumnFragmentainers(builder, innerMcNode)
	if len(innerCols) == 0 {
		return
	}

	// Resolve OOF dimensions against the CB's content box. For the case
	// covered by Cmt-3 (explicit width/height — `-032`'s abspos has both),
	// this returns the resolved sizes directly. Other configurations
	// (auto-sized, inset-based) are handled by the layoutCandidatesOnce
	// path; deferred for follow-up.
	cbStyle := d.ContainingBlock.Fragment.Style
	if cbStyle == nil {
		return
	}
	cbInline := d.ContainingBlock.Fragment.Size.WidthF64()
	cbBlock := d.ContainingBlock.Fragment.Size.HeightF64()
	if data := d.ContainingBlock.Fragment.BoxData; data != nil {
		cbInline -= data.Border.Left + data.Border.Right + data.Padding.Left + data.Padding.Right
		cbBlock -= data.Border.Top + data.Border.Bottom + data.Padding.Top + data.Padding.Bottom
	}

	oofStyle := d.Candidate.Node.Style()
	if oofStyle == nil {
		return
	}
	oofInline, oofBlock, ok := resolveExplicitOOFSize(oofStyle, cbInline, cbBlock)
	if !ok {
		// Implicit-size OOF — not handled by Cmt-3's drain. Future commits
		// will route through ComputeOofDimensions / layoutCandidatesOnce
		// for the IMCB path.
		return
	}

	// Lay the OOF out once at its full size to materialize its children
	// (background paints from the synthesized piece fragments — children
	// of the abspos won't paint until follow-up work splits inner content
	// across pieces; for `-032` the abspos has no children).
	oofWDM := NewWritingDirectionMode(oofStyle)
	wdm := mla.space.WritingDirection
	space := NewConstraintSpaceBuilder(wdm, oofWDM, true).
		SetAvailableSize(LogicalSize{InlineSize: oofInline, BlockSize: oofBlock}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: cbInline, BlockSize: cbBlock}).
		SetIsFixedInlineSize(true).
		SetIsFixedBlockSize(true).
		Build()
	oofResult := layoutElement(mla.ctx, d.Candidate.Node, space)
	if oofResult == nil || oofResult.Fragment == nil {
		return
	}

	// Determine which inner column the OOF starts in, using the descendant's
	// CB-relative static-position block offset. Mirrors
	// `ComputeStartFragmentIndexAndRelativeOffset`
	// (out_of_flow_layout_part.cc:3143-3210). Inner columns are listed in
	// fragmentation order (col 0..N within outer-fragment 0, then 0..N within
	// outer-fragment 1, ...).
	startBlock := d.Candidate.StaticPosition.Offset.BlockOffset
	startInline := d.Candidate.StaticPosition.Offset.InlineOffset
	startIdx := 0
	for startIdx < len(innerCols) {
		colBlock := innerCols[startIdx].Size.BlockSize
		if startBlock < colBlock {
			break
		}
		startBlock -= colBlock
		startIdx++
	}
	if startIdx >= len(innerCols) {
		// Static position is past every column — nothing to render.
		return
	}

	// Slice the OOF's block extent across successive inner columns. Each
	// piece is a synthesized PhysicalFragment carrying the OOF's style for
	// background/border paint at the right position+size.
	remaining := oofBlock
	pieceBlockStart := startBlock
	for k := startIdx; k < len(innerCols) && remaining > 0; k++ {
		col := innerCols[k]
		availInThisCol := col.Size.BlockSize - pieceBlockStart
		if availInThisCol <= 0 {
			pieceBlockStart = 0
			continue
		}
		pieceBlock := remaining
		if pieceBlock > availInThisCol {
			pieceBlock = availInThisCol
		}

		piece := mla.makeOOFPiece(d.Candidate.Node, oofStyle, oofInline, pieceBlock)
		piecePos := LogicalOffset{
			InlineOffset: col.OuterContentBoxOffset.InlineOffset + startInline,
			BlockOffset:  col.OuterContentBoxOffset.BlockOffset + pieceBlockStart,
		}
		builder.AddChild(piece, piecePos)

		remaining -= pieceBlock
		pieceBlockStart = 0
	}
}

// innerColumnFragmentainer records an inner multicol column box's position
// and size in the outer multicol's content-box logical coordinates.
type innerColumnFragmentainer struct {
	OuterContentBoxOffset LogicalOffset
	Size                  LogicalSize
}

// findInnerColumnFragmentainers walks `builder.children` (the outer
// multicol's column fragments) recursively to find every column-box child
// of `innerMcNode`. Returns them in fragmentation order. HTB-only.
func (mla *MulticolLayoutAlgorithm) findInnerColumnFragmentainers(
	builder *BoxFragmentBuilder, innerMcNode *LayoutInputNode,
) []innerColumnFragmentainer {
	var result []innerColumnFragmentainer
	for _, outerCol := range builder.children {
		walkForInnerColumns(outerCol.fragment, outerCol.offset, innerMcNode, &result)
	}
	return result
}

func walkForInnerColumns(
	frag *PhysicalFragment, parentOffset LogicalOffset,
	innerMcNode *LayoutInputNode, result *[]innerColumnFragmentainer,
) {
	if frag == nil {
		return
	}
	for _, child := range frag.Children {
		if child.Fragment == nil {
			continue
		}
		// HTB-only: physical (left, top) maps directly to logical
		// (inline, block).
		childOff := LogicalOffset{
			InlineOffset: parentOffset.InlineOffset + child.Offset.LeftF64(),
			BlockOffset:  parentOffset.BlockOffset + child.Offset.TopF64(),
		}
		if child.Fragment.LayoutNode == innerMcNode {
			for _, inner := range child.Fragment.Children {
				if inner.Fragment != nil && inner.Fragment.IsColumnBox() {
					innerOff := LogicalOffset{
						InlineOffset: childOff.InlineOffset + inner.Offset.LeftF64(),
						BlockOffset:  childOff.BlockOffset + inner.Offset.TopF64(),
					}
					*result = append(*result, innerColumnFragmentainer{
						OuterContentBoxOffset: innerOff,
						Size: LogicalSize{
							InlineSize: inner.Fragment.Size.WidthF64(),
							BlockSize:  inner.Fragment.Size.HeightF64(),
						},
					})
				}
			}
			continue
		}
		walkForInnerColumns(child.Fragment, childOff, innerMcNode, result)
	}
}

// findInnerMulticolForCB picks the pending multicol whose subtree contains
// the descendant's CB fragment. For -032's single-pending-multicol case this
// just returns the only entry; for multi-pending cases it walks each
// candidate's subtree looking for the CB fragment.
func (mla *MulticolLayoutAlgorithm) findInnerMulticolForCB(
	builder *BoxFragmentBuilder,
	cbFragment *PhysicalFragment,
	pendingMulticols map[*LayoutInputNode]*MulticolWithPendingOOFs,
) *LayoutInputNode {
	if len(pendingMulticols) == 0 {
		return nil
	}
	if len(pendingMulticols) == 1 {
		for n := range pendingMulticols {
			return n
		}
	}
	for n := range pendingMulticols {
		for _, outerCol := range builder.children {
			if subtreeContainsFragment(outerCol.fragment, n, cbFragment) {
				return n
			}
		}
	}
	return nil
}

func subtreeContainsFragment(
	frag *PhysicalFragment, multicolNode *LayoutInputNode, target *PhysicalFragment,
) bool {
	if frag == nil {
		return false
	}
	if frag == target {
		// Stand-alone hit — without scoping by multicol, can't disambiguate.
		// For pending-multicol correlation the caller already filtered to
		// nodes with FragmentedOofData; treat any CB-match within an outer
		// column as belonging to the matched multicol.
		return true
	}
	for _, child := range frag.Children {
		if child.Fragment == nil {
			continue
		}
		if child.Fragment.LayoutNode == multicolNode {
			if subtreeContainsFragment(child.Fragment, multicolNode, target) {
				return true
			}
		}
		if subtreeContainsFragment(child.Fragment, multicolNode, target) {
			return true
		}
	}
	return false
}

// makeOOFPiece synthesizes a PhysicalFragment representing one
// fragmentainer-bounded slice of an OOF. Style comes from the OOF node so
// background/border paint correctly. HTB-only — for vertical writing modes
// the size axes need a logical→physical conversion.
func (mla *MulticolLayoutAlgorithm) makeOOFPiece(
	oofNode *LayoutInputNode, oofStyle *css.Style,
	inlineSize, blockSize float64,
) *PhysicalFragment {
	wdm := mla.space.WritingDirection
	physSize := ToPhysicalSize(LogicalSize{InlineSize: inlineSize, BlockSize: blockSize}, wdm.WM)
	return &PhysicalFragment{
		Size:             oldSizeToGeom(physSize),
		WritingDirection: wdm,
		Type:             FragmentBox,
		BoxType:          BoxTypeNormal,
		LayoutNode:       oofNode,
		Node:             oofNode.DOMNode,
		Style:            oofStyle,
	}
}

// resolveExplicitOOFSize resolves the OOF's CSS width/height against the
// CB's content box. Returns (inline, block, ok) — ok is false when either
// dimension is auto and would require IMCB resolution. For the inline axis,
// width:100% (or any percentage) and length values are explicit. For the
// block axis, the height property must be explicit (length or percentage
// against a definite CB block-size). Mirrors the simple branches of
// `absolute_utils.cc::ComputeOofInlineDimensions` /
// `ComputeOofBlockDimensions` for the case where both axes are explicit.
func resolveExplicitOOFSize(style *css.Style, cbInline, cbBlock float64) (float64, float64, bool) {
	inline, ok := resolveExplicitAxis(style, "width", cbInline)
	if !ok {
		return 0, 0, false
	}
	block, ok := resolveExplicitAxis(style, "height", cbBlock)
	if !ok {
		return 0, 0, false
	}
	return inline, block, true
}

func resolveExplicitAxis(style *css.Style, prop string, cbExtent float64) (float64, bool) {
	// CSS Values 4 §10: math functions with a % term resolve against the
	// containing block's axis extent. Mirrors fragment_geometry.Resolve*
	// for in-flow elements — out-of-flow elements need the same dispatch
	// because their direct property reads bypass the constraint-space
	// percent-base flow. Without this branch, e.g. width: calc(100vw + 50%)
	// on absolute-positioned children returns false (the % term won't
	// resolve under GetLength's base-0 calc context) and the element falls
	// back to auto-sizing — see css-values/vh-calc-support-pct.
	if val, ok := style.Get(prop); ok && css.IsMathFunctionWithPercent(val) && cbExtent > 0 {
		if result, calcOK := style.EvalMathWithBase(val, cbExtent); calcOK {
			return result, true
		}
	}
	if v, ok := style.GetLength(prop); ok {
		return v, true
	}
	if pct, ok := style.GetPercentage(prop); ok {
		return cbExtent * pct / 100.0, true
	}
	return 0, false
}
