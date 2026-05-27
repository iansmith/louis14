package layout

import "louis14/pkg/css"

// Phase 25 Cmt-3: OOF-fragmentation drain pipeline. Mirrors Blink's
// `OutOfFlowLayoutPart::HandleFragmentation` /
// `HandleMulticolsWithPendingOOFs` / `LayoutOOFsInMulticol` /
// `LayoutFragmentainerDescendants` / `ComputeStartFragmentIndexAndRelativeOffset`
// (out_of_flow_layout_part.cc:589-695, 1265-1529, 1531-1758, 3143-3210).
//
// Scope (this commit). The drain handles the simplest representative case
// from `multicol-nested-032`: a positioned descendant whose CB lives inside
// an inner multicol nested in an outer multicol, with HTB-only writing
// modes. Layout-time considerations not yet wired:
//   - Nested children of the OOF (the synthesized piece fragments only paint
//     the OOF's own background/border, not its descendants).
//   - Inset-based sizing (works for explicit width/height; insets fall through
//     to a plain LayoutCandidates path).
//   - Vertical writing modes (the offset arithmetic is HTB-axis-aligned).
//   - Non-multicol fragmentation roots (paged media, etc.).
//
// These are tracked for follow-up commits in Phase 25.

// HandleOofFragmentation is the entry point Blink's OutOfFlowLayoutPart::Run
// calls before LayoutCandidates. Drains pending fragmentainer descendants and
// MulticolsWithPendingOOFs entries on `builder` when it is the block
// fragmentation context root. No-op otherwise.
//
// The drain produces synthetic OOF child fragments laid out per-fragmentainer
// (inner multicol column, outer multicol column, paged area, ...) and adds
// them as children of `builder`, positioned in the builder's content-box
// coordinate system. Mirrors the post-`HandleFragmentation` state in Blink
// where each fragmentainer's child list has been augmented with OOF results.
func (mla *MulticolLayoutAlgorithm) HandleOofFragmentation(builder *BoxFragmentBuilder) {
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

	// Per-descendant: lay out the OOF and slice it across the inner
	// multicol's column fragmentainers. For -032's case there is exactly one
	// pending inner multicol; if multiple are present each descendant matches
	// the multicol whose inner-column tree contains its CB fragment.
	for _, d := range descendants {
		if d.ContainingBlock.Fragment == nil {
			// Should not happen post-propagation — the parent fragment walk
			// always backfills CB.Fragment. Skip defensively.
			continue
		}
		mla.layoutFragmentainerDescendant(builder, d, pendingMulticols)
	}
}

// layoutFragmentainerDescendant lays out a single deferred OOF descendant
// against the column flow whose fragmentainers contain its CB, splitting the
// OOF's block extent across successive columns. Mirrors Blink's
// `LayoutFragmentainerDescendants` per-descendant body
// (out_of_flow_layout_part.cc:1531-1758).
//
// Two cases are handled:
//
//  1. The CB is inside a nested inner multicol (CB.Fragment lives under an
//     entry in `pendingMulticols`). The OOF slices across the inner multicol's
//     column fragmentainers — the existing path for `multicol-nested-032`.
//
//  2. C38: the CB is a non-multicol positioned block fragmented across THIS
//     outer multicol's columns. The OOF slices across this multicol's own
//     column children — covers `column-balancing-with-span-and-oof-{001,002}`.
func (mla *MulticolLayoutAlgorithm) layoutFragmentainerDescendant(
	builder *BoxFragmentBuilder,
	d LogicalOofNodeForFragmentation,
	pendingMulticols map[*LayoutInputNode]*MulticolWithPendingOOFs,
) {
	innerMcNode := mla.findInnerMulticolForCB(builder, d.ContainingBlock.Fragment, pendingMulticols)
	if innerMcNode == nil {
		// C38: outer-multicol CB case. The CB is an ordinary positioned block
		// whose fragments live in THIS multicol's column flow.
		mla.layoutFragmentainerDescendantInOuter(builder, d)
		return
	}

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

// layoutFragmentainerDescendantInOuter lays out a single deferred OOF
// descendant whose CB is a non-multicol positioned block fragmented across
// THIS multicol's columns. C38 — mirrors Blink's
// `LayoutFragmentainerDescendants` outer-CB branch
// (out_of_flow_layout_part.cc:1531-1758).
//
// Algorithm:
//  1. Gather this multicol's column-box children in fragmentation order.
//  2. Find the CB's fragments by LayoutNode identity across those columns;
//     record each CB fragment's column index and column-relative block offset.
//  3. Resolve the OOF's CB-stitched static position into a (column, offset)
//     pair by walking the CB fragments. If the static position is past every
//     CB fragment, advance into subsequent columns of THIS multicol (the
//     OOF's stitched extent flows past the CB into the multicol's column
//     flow). Zero-height fragmentainers (e.g. a spanner-detector empty col)
//     are skipped.
//  4. Slice the OOF's block extent across successive columns starting from
//     the resolved (column, offset), emitting one synthesized piece fragment
//     per column.
func (mla *MulticolLayoutAlgorithm) layoutFragmentainerDescendantInOuter(
	builder *BoxFragmentBuilder, d LogicalOofNodeForFragmentation,
) {
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
	// For now require explicit width/height on the OOF (matches the inner
	// path's scope guard). IMCB / inset-based sizing is deferred follow-up.
	oofInline, oofBlock, ok := resolveExplicitOOFSize(oofStyle, cbInline, cbBlock)
	if !ok {
		return
	}

	// Step 1: enumerate this multicol's column-box children in fragmentation
	// order. Each is the multicol's own per-column fragmentainer.
	cols := mla.findOuterColumnFragmentainers(builder)
	if len(cols) == 0 {
		return
	}

	// Step 2: find the CB's fragments across these columns. A single OOF
	// candidate may have been promoted from one CB fragment, but the CB
	// itself has multiple fragments — we need them all to compute the
	// stitched-CB-to-column-flow mapping.
	cbNode := d.ContainingBlock.Fragment.LayoutNode
	if cbNode == nil {
		return
	}
	cbFrags := findCBFragmentsInColumns(cols, cbNode)
	if len(cbFrags) == 0 {
		return
	}

	// Step 3a: lay the OOF out once at its full size to materialize children.
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

	// Step 3b: resolve the OOF's CB-stitched start block position.
	//
	// Insets (`top`/`bottom`) override the static position per CSS 2.1
	// §10.6.4: if `top` is set, use it directly (CB-relative); else if
	// `bottom` is set, anchor from the CB's stitched-bottom; else fall back
	// to the static position. The stitched CB block-size is the sum of CB
	// fragment block-sizes — which equals the CB's logical block-size as
	// known at OOF-resolution time. For our outer-CB case we have CB
	// fragments listed in `cbFrags`; their sum is the stitched extent.
	stitchedCBBlock := 0.0
	for _, cf := range cbFrags {
		stitchedCBBlock += cf.cbFragmentBlock
	}
	cbPhys := PhysicalSize{Width: cbInline, Height: stitchedCBBlock}
	insets := oofStyle.GetPositionOffsetResolved(cbPhys.Width, cbPhys.Height)
	var staticBlock float64
	switch {
	case insets.HasTop:
		staticBlock = insets.Top
	case insets.HasBottom:
		staticBlock = stitchedCBBlock - insets.Bottom - oofBlock
	default:
		staticBlock = d.Candidate.StaticPosition.Offset.BlockOffset
	}
	startColIdx := -1
	startColOffset := 0.0
	cumulativeCB := 0.0
	for _, cf := range cbFrags {
		if staticBlock < cumulativeCB+cf.cbFragmentBlock {
			startColIdx = cf.colIdx
			startColOffset = cf.colRelativeOffset + (staticBlock - cumulativeCB)
			break
		}
		cumulativeCB += cf.cbFragmentBlock
	}
	if startColIdx < 0 {
		// Static position is past every CB fragment. Continue into the
		// multicol's column flow after the LAST CB fragment's column.
		lastCBCol := cbFrags[len(cbFrags)-1]
		remainder := staticBlock - cumulativeCB
		// Advance through subsequent columns of THIS multicol, eating their
		// block-sizes against the remainder. Zero-height columns (spanner
		// detector emptys) are stepped over without consuming the remainder
		// since they contribute no block extent.
		startColIdx = lastCBCol.colIdx + 1
		for startColIdx < len(cols) {
			cb := cols[startColIdx].Size.BlockSize
			if cb <= 0 {
				startColIdx++
				continue
			}
			if remainder < cb {
				startColOffset = remainder
				break
			}
			remainder -= cb
			startColIdx++
		}
		if startColIdx >= len(cols) {
			return // off the end of the column flow
		}
	}

	// Step 3c: account for the OOF's CB-relative inline static position. For
	// this case the abspos's column-relative inline position is the
	// CB-fragment's column-relative-inline-offset + the OOF's static inline.
	// The CB-fragment's column-relative-inline-offset is captured per CB
	// fragment; for the start column use cbFrags[start CB fragment].
	// For simplicity we use the start column's CB fragment if any.
	startInline := d.Candidate.StaticPosition.Offset.InlineOffset
	for _, cf := range cbFrags {
		if cf.colIdx == startColIdx {
			startInline += cf.colRelativeInlineOffset
			break
		}
	}

	// Step 4: slice the OOF's block extent across successive columns. The
	// per-column block-extent used for slicing is the column's BALANCE-SLOT
	// height (the colBlockSize chosen by the balancer for this row), not the
	// column fragment's intrinsic block-size which may be zero for an empty
	// trailing column. Mirrors Blink's behaviour: an OOF crosses the
	// fragmentainer flow at the chosen column height regardless of the
	// column's content occupancy.
	rowSlot := mla.outerColumnRowSlot(cols, startColIdx)
	remaining := oofBlock
	pieceBlockStart := startColOffset
	for k := startColIdx; k < len(cols) && remaining > 0; k++ {
		col := cols[k]
		slot := col.Size.BlockSize
		if slot <= 0 {
			// Empty trailing column in this row: use the row's slot height.
			slot = rowSlot
		} else {
			// New row encountered — refresh rowSlot for subsequent empty
			// columns within it.
			rowSlot = slot
		}
		if slot <= 0 {
			// Still zero — nothing to slice into.
			continue
		}
		availInThisCol := slot - pieceBlockStart
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

// outerColumnRowSlot returns the BALANCE-SLOT block-size for the row that
// starts at `startColIdx`. Columns are grouped into rows by their
// OuterContentBoxOffset.BlockOffset; the slot is the maximum column
// block-size in the row (i.e. the row's column-height as picked by the
// balancer). When the start column has a positive block-size, that's the
// slot; otherwise we scan siblings sharing the same row's block-offset.
func (mla *MulticolLayoutAlgorithm) outerColumnRowSlot(
	cols []innerColumnFragmentainer, startColIdx int,
) float64 {
	if startColIdx < 0 || startColIdx >= len(cols) {
		return 0
	}
	rowBlockOffset := cols[startColIdx].OuterContentBoxOffset.BlockOffset
	maxBS := cols[startColIdx].Size.BlockSize
	for _, c := range cols {
		if c.OuterContentBoxOffset.BlockOffset == rowBlockOffset && c.Size.BlockSize > maxBS {
			maxBS = c.Size.BlockSize
		}
	}
	return maxBS
}

// cbFragmentInColumn records a single CB fragment's location within one of
// this multicol's column children. Used by the outer-CB drain path to map
// CB-stitched coordinates onto the multicol's column-flow.
type cbFragmentInColumn struct {
	colIdx                  int
	cbFragmentBlock         float64 // block-size of this CB fragment
	colRelativeOffset       float64 // CB fragment's block-offset within the column
	colRelativeInlineOffset float64 // CB fragment's inline-offset within the column
}

// findCBFragmentsInColumns walks each column's fragment tree looking for
// fragments whose LayoutNode equals cbNode. Returns one entry per CB fragment
// in fragmentation order. HTB-only.
func findCBFragmentsInColumns(
	cols []innerColumnFragmentainer, cbNode *LayoutInputNode,
) []cbFragmentInColumn {
	var out []cbFragmentInColumn
	for ci, col := range cols {
		// Walk inside this column for any descendant fragment with LayoutNode == cbNode.
		walkForCBFragments(col, ci, LogicalOffset{}, cbNode, &out)
	}
	return out
}

func walkForCBFragments(
	col innerColumnFragmentainer, colIdx int,
	parentOffset LogicalOffset, cbNode *LayoutInputNode, out *[]cbFragmentInColumn,
) {
	// col.OuterContentBoxOffset is where the column is in the multicol; we
	// walk INSIDE the column. To compute column-relative offsets we accumulate
	// against the column's own top-left, not the multicol's.
	walkChildrenForCBFragments(col, col.columnFragment(), LogicalOffset{}, colIdx, cbNode, out)
}

func walkChildrenForCBFragments(
	col innerColumnFragmentainer, frag *PhysicalFragment,
	parentInColumn LogicalOffset, colIdx int, cbNode *LayoutInputNode,
	out *[]cbFragmentInColumn,
) {
	if frag == nil {
		return
	}
	for _, ch := range frag.Children {
		if ch.Fragment == nil {
			continue
		}
		childInCol := LogicalOffset{
			InlineOffset: parentInColumn.InlineOffset + ch.Offset.LeftF64(),
			BlockOffset:  parentInColumn.BlockOffset + ch.Offset.TopF64(),
		}
		if ch.Fragment.LayoutNode == cbNode {
			*out = append(*out, cbFragmentInColumn{
				colIdx:                  colIdx,
				cbFragmentBlock:         ch.Fragment.Size.HeightF64(),
				colRelativeOffset:       childInCol.BlockOffset,
				colRelativeInlineOffset: childInCol.InlineOffset,
			})
			// Don't recurse into the CB itself for further CB fragments —
			// CB cannot contain another fragment of itself.
			continue
		}
		walkChildrenForCBFragments(col, ch.Fragment, childInCol, colIdx, cbNode, out)
	}
}

// findOuterColumnFragmentainers returns this multicol's own column-box
// children in fragmentation order. They are the immediate column-box children
// of `builder.children` (multicol_layout.go:1595 commits column fragments
// onto the builder). Spanners and other non-column children are excluded.
func (mla *MulticolLayoutAlgorithm) findOuterColumnFragmentainers(
	builder *BoxFragmentBuilder,
) []innerColumnFragmentainer {
	var out []innerColumnFragmentainer
	for _, ch := range builder.children {
		if ch.fragment == nil || !ch.fragment.IsColumnBox() {
			continue
		}
		out = append(out, innerColumnFragmentainer{
			OuterContentBoxOffset: ch.offset,
			Size: LogicalSize{
				InlineSize: ch.fragment.Size.WidthF64(),
				BlockSize:  ch.fragment.Size.HeightF64(),
			},
			columnFrag: ch.fragment,
		})
	}
	return out
}

// innerColumnFragmentainer records an inner multicol column box's position
// and size in the outer multicol's content-box logical coordinates.
type innerColumnFragmentainer struct {
	OuterContentBoxOffset LogicalOffset
	Size                  LogicalSize
	columnFrag            *PhysicalFragment // populated only by findOuterColumnFragmentainers for outer-CB drain
}

// columnFragment returns the PhysicalFragment of this column. Used by the
// outer-CB drain path to walk the column's subtree for CB fragments.
func (c innerColumnFragmentainer) columnFragment() *PhysicalFragment {
	return c.columnFrag
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
	if v, ok := style.GetLength(prop); ok {
		return v, true
	}
	if pct, ok := style.GetPercentage(prop); ok {
		return cbExtent * pct / 100.0, true
	}
	return 0, false
}
