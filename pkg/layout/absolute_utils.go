package layout

// absolute_utils.go ports Blink's core/layout/absolute_utils.{h,cc}:
// the IMCB (Inset-Modified Containing Block) machinery used by the OOF
// (out-of-flow / position:absolute / position:fixed) layout pass.
//
// This file contains the *pure algorithmic* half of the port — types and
// functions that take resolved geometry and static-position inputs and
// compute an IMCB, margins, and final insets. It has no dependencies on
// ConstraintSpace or the layout engine, so it can be unit-tested
// independently.
//
// The integration layer (ComputeOofInlineDimensions / ComputeOofBlockDimensions
// and the OutOfFlowLayoutPart call sites) is ported in a follow-up commit.
//
// Explicit non-ports (Blink has them, we will port when needed):
//   - Anchor positioning (LogicalAnchorCenterPosition, anchor-center)
//   - Table-specific IMCB clamp in ComputeInsetModifiedContainingBlock
//   - Fragmentation column/page context for OOF
// TODO(anchor-positioning) / TODO(table-imcb-clamp) / TODO(fragmentation)
// markers are placed where Blink's signature takes these.

// InsetBias identifies which edge of the IMCB "wins" when distributing
// leftover space after the box has been sized. Mirrors the anonymous
// enum InsetModifiedContainingBlock::InsetBias in Blink's absolute_utils.cc.
type InsetBias int

const (
	// BiasStart pins the box to the start edge; leftover space grows the end side.
	BiasStart InsetBias = iota
	// BiasEnd pins the box to the end edge; leftover space grows the start side.
	BiasEnd
	// BiasEqual splits leftover space equally between the two sides.
	// Used for center alignment and auto-margin distribution.
	BiasEqual
)

// ItemPosition mirrors Blink's ItemPosition (cssom/style_self_alignment_data.h)
// values used by align-self and justify-self. We include only the variants
// that affect OOF inset resolution.
type ItemPosition int

const (
	ItemPositionNormal ItemPosition = iota // abspos default; behaves like start on block axis
	ItemPositionAuto                       // falls back to parent's align-items / justify-items
	ItemPositionStart
	ItemPositionEnd
	ItemPositionCenter
	ItemPositionSelfStart
	ItemPositionSelfEnd
	ItemPositionFlexStart
	ItemPositionFlexEnd
	ItemPositionStretch
	ItemPositionBaseline
	ItemPositionLastBaseline
	ItemPositionLeft
	ItemPositionRight
	// TODO(anchor-positioning): ItemPositionAnchorCenter
)

// AlignmentData carries a single axis's resolved align-self/justify-self.
// Mirrors Blink's StyleSelfAlignmentData (position + overflow-alignment bit).
type AlignmentData struct {
	Position ItemPosition
	IsSafe   bool // overflow-alignment: safe (vs default)
}

// LogicalAlignment carries the two per-axis alignment values that drive the
// bias picked by GetAlignmentInsetBias. Mirrors Blink's LogicalAlignment.
type LogicalAlignment struct {
	InlineAlignment AlignmentData
	BlockAlignment  AlignmentData
}

// LogicalOofInsets holds the resolved CSS insets (top/right/bottom/left
// converted to logical coordinates) with a "has value" flag per side.
// Mirrors Blink's LogicalOofInsets.
//
// Louis14 already has LogicalInsets (writing_mode_converter.go) with the
// same shape. We keep LogicalOofInsets as a distinct name to stay aligned
// with Blink's API surface; at call sites we convert with AsOofInsets().
type LogicalOofInsets struct {
	InlineStart, InlineEnd float64
	BlockStart, BlockEnd   float64
	HasInlineStart, HasInlineEnd bool
	HasBlockStart, HasBlockEnd   bool
}

// AsOofInsets reshapes a LogicalInsets into a LogicalOofInsets. The fields
// are identical; this is a narrow type-converting helper.
func (li LogicalInsets) AsOofInsets() LogicalOofInsets {
	return LogicalOofInsets{
		InlineStart: li.InlineStart, InlineEnd: li.InlineEnd,
		BlockStart: li.BlockStart, BlockEnd: li.BlockEnd,
		HasInlineStart: li.HasInlineStart, HasInlineEnd: li.HasInlineEnd,
		HasBlockStart: li.HasBlockStart, HasBlockEnd: li.HasBlockEnd,
	}
}

// InsetModifiedContainingBlock is the CB with specified insets subtracted.
// Mirrors Blink's InsetModifiedContainingBlock struct (absolute_utils.h).
//
// Fields are named 1:1 with Blink's struct. HasAuto{Inline,Block}Inset records
// whether either inset on the axis was auto (for downstream margin handling).
type InsetModifiedContainingBlock struct {
	// AvailableSize is the original containing block size in the CB's
	// writing mode (the IMCB's "available_size" field in Blink).
	AvailableSize LogicalSize

	// Resolved inset offsets, in the CB's logical coordinates. Auto insets
	// resolve to zero here, but HasAutoInlineInset / HasAutoBlockInset
	// still record that the original inset was auto.
	InlineStart, InlineEnd float64
	BlockStart, BlockEnd   float64

	HasAutoInlineInset bool
	HasAutoBlockInset  bool

	InlineInsetBias InsetBias
	BlockInsetBias  InsetBias

	// Safe and default biases track overflow-alignment semantics. They are
	// populated by GetAlignmentInsetBias; ComputeInsets may use them to
	// resolve overflow.
	InlineSafeInsetBias, BlockSafeInsetBias       InsetBias
	HasInlineSafeInsetBias, HasBlockSafeInsetBias bool

	InlineDefaultInsetBias, BlockDefaultInsetBias       InsetBias
	HasInlineDefaultInsetBias, HasBlockDefaultInsetBias bool

	// InlineHasDefaultAlignmentOverflow and BlockHasDefaultAlignmentOverflow
	// track the "default alignment is non-normal and box overflows" flag
	// used by Blink's ComputeInsets. Not yet read by us; preserved for
	// parity so a future caller can wire it in.
	InlineHasDefaultAlignmentOverflow bool
	BlockHasDefaultAlignmentOverflow  bool
}

// InlineSize returns the IMCB's inline-axis content size.
func (imcb InsetModifiedContainingBlock) InlineSize() float64 {
	return imcb.AvailableSize.InlineSize - imcb.InlineStart - imcb.InlineEnd
}

// BlockSize returns the IMCB's block-axis content size.
func (imcb InsetModifiedContainingBlock) BlockSize() float64 {
	return imcb.AvailableSize.BlockSize - imcb.BlockStart - imcb.BlockEnd
}

// Size returns both axes of the IMCB as a LogicalSize.
func (imcb InsetModifiedContainingBlock) Size() LogicalSize {
	return LogicalSize{InlineSize: imcb.InlineSize(), BlockSize: imcb.BlockSize()}
}

// LogicalOofDimensions is the resolved output of a ComputeOofXxxDimensions
// call: the final position (via `Inset`), size, and margins for one axis or
// both. Mirrors Blink's LogicalOofDimensions.
//
// `Inset` is the logical inset from the CB's edge to the margin-box edge of
// the OOF child. `Size` is the child's border-box size. `Margins` are the
// resolved logical margins (including any auto-margin redistribution).
type LogicalOofDimensions struct {
	Inset   LogicalEdges
	Size    LogicalSize
	Margins LogicalEdges
}

// GetAlignmentInsetBias maps a single-axis AlignmentData to the primary
// InsetBias plus optional "safe" and "default" biases.
//
// Mirrors Blink's GetAlignmentInsetBias() (absolute_utils.cc:51).
//
// containerWDM/selfWDM: both writing directions are taken so that the
// kSelfStart/kSelfEnd positions resolve against the element's own direction
// while kStart/kEnd resolve against the container's.
//
// isJustifyAxis: true for the inline-like axis (justify-*), false for block
// (align-*). Affects how kLeft/kRight map (Blink handles these per axis).
//
// Returns:
//   bias                           — primary InsetBias for this axis.
//   hasDefaultAlignmentOverflow    — true if align is non-normal; set on
//                                    InsetModifiedContainingBlock for later
//                                    use by ComputeInsets.
//   defaultInsetBias, hasDefault   — fallback bias for non-normal alignment.
//   safeInsetBias, hasSafe         — fallback bias when align has `safe`.
func GetAlignmentInsetBias(
	a AlignmentData,
	containerWDM WritingDirectionMode,
	selfWDM WritingDirectionMode,
	isJustifyAxis bool,
) (
	bias InsetBias,
	hasDefaultAlignmentOverflow bool,
	defaultInsetBias InsetBias, hasDefault bool,
	safeInsetBias InsetBias, hasSafe bool,
) {
	// For OOF, the "default" inset bias is the start edge of the containing
	// block. When align is non-normal (specifically center/end), Blink sets
	// hasDefaultAlignmentOverflow so that ComputeInsets can snap back to
	// the start edge when the content overflows.
	switch a.Position {
	case ItemPositionNormal, ItemPositionAuto, ItemPositionStretch, ItemPositionStart,
		ItemPositionFlexStart, ItemPositionBaseline:
		bias = BiasStart
	case ItemPositionEnd, ItemPositionFlexEnd, ItemPositionLastBaseline:
		bias = BiasEnd
		hasDefaultAlignmentOverflow = true
		defaultInsetBias, hasDefault = BiasStart, true
	case ItemPositionCenter:
		bias = BiasEqual
		hasDefaultAlignmentOverflow = true
		defaultInsetBias, hasDefault = BiasStart, true
	case ItemPositionSelfStart:
		if axesOppose(containerWDM, selfWDM, isJustifyAxis) {
			bias = BiasEnd
		} else {
			bias = BiasStart
		}
	case ItemPositionSelfEnd:
		if axesOppose(containerWDM, selfWDM, isJustifyAxis) {
			bias = BiasStart
		} else {
			bias = BiasEnd
		}
		hasDefaultAlignmentOverflow = true
		defaultInsetBias, hasDefault = BiasStart, true
	case ItemPositionLeft:
		if isJustifyAxis && containerWDM.IsRTL() {
			bias = BiasEnd
		} else {
			bias = BiasStart
		}
	case ItemPositionRight:
		if isJustifyAxis && containerWDM.IsRTL() {
			bias = BiasStart
		} else {
			bias = BiasEnd
		}
		hasDefaultAlignmentOverflow = true
		defaultInsetBias, hasDefault = BiasStart, true
	default:
		bias = BiasStart
	}

	if a.IsSafe {
		safeInsetBias, hasSafe = BiasStart, true
	}
	return
}

// axesOppose returns true if the child's inline axis (isJustifyAxis=true) or
// block axis points in the opposite direction from the container's. Used to
// flip kSelfStart/kSelfEnd.
func axesOppose(container, self WritingDirectionMode, isJustifyAxis bool) bool {
	if isJustifyAxis {
		// Inline axis comparison: depends on writing mode & direction combination.
		if container.IsHorizontal() == self.IsHorizontal() {
			return container.Dir != self.Dir
		}
		// Orthogonal: self-start/self-end use the self's axis direction;
		// the container can't align on an orthogonal axis in a direct sense,
		// so treat as non-opposing. This matches Blink's fallback.
		return false
	}
	// Block axis: VRL/SRL flip block progression relative to VLR/SLR/HTB.
	containerFlipped := container.IsFlippedBlocks()
	selfFlipped := self.IsFlippedBlocks()
	return containerFlipped != selfFlipped
}

// ComputeUnclampedIMCBInOneAxis computes the per-axis IMCB start/end offsets
// and output inset biases. Mirrors Blink's ComputeUnclampedIMCBInOneAxis
// (absolute_utils.cc:128).
//
// Inputs:
//   availableSize           — the CB's size on this axis.
//   insetStart/hasStart,
//   insetEnd/hasEnd         — resolved insets; auto if !has*.
//   isStaticAlignmentParallel — whether the align-self axis is this axis.
//                              Affects which safe-bias fallback is chosen.
//   staticPositionOffset    — the static-position offset on this axis.
//   staticPositionInsetBias — derived from StaticPositionEdge (Start/Center/End
//                              → BiasStart/BiasEqual/BiasEnd).
//   alignmentInsetBias,
//   defaultInsetBias,
//   safeInsetBias/hasSafe,
//   altSafeInsetBias/hasAlt — outputs of GetAlignmentInsetBias on the two axes.
//
// Outputs:
//   imcbStart, imcbEnd    — resolved IMCB offsets (auto → 0).
//   biasOut               — the effective InsetBias used for Resize later.
//   safeOut/hasSafeOut,
//   defaultOut/hasDefOut  — passthrough biases for ComputeInsets.
func ComputeUnclampedIMCBInOneAxis(
	availableSize float64,
	insetStart float64, hasStart bool,
	insetEnd float64, hasEnd bool,
	isStaticAlignmentParallel bool,
	staticPositionOffset float64,
	staticPositionInsetBias InsetBias,
	alignmentInsetBias InsetBias,
	defaultInsetBias InsetBias,
	safeInsetBias InsetBias, hasSafe bool,
	altSafeInsetBias InsetBias, hasAlt bool,
) (
	imcbStart, imcbEnd float64,
	biasOut InsetBias,
	safeOut InsetBias, hasSafeOut bool,
	defaultOut InsetBias, hasDefaultOut bool,
) {
	// Branch A — both insets auto. IMCB is defined by the static-position
	// rectangle; the static-position's inset bias (from StaticPositionEdge)
	// becomes the output bias.
	if !hasStart && !hasEnd {
		switch staticPositionInsetBias {
		case BiasStart:
			// |*------------>|
			imcbStart = staticPositionOffset
			imcbEnd = 0
		case BiasEqual:
			// |<----*---->   |  — center-clipping so the box can't escape.
			half := staticPositionOffset
			if availableSize-staticPositionOffset < half {
				half = availableSize - staticPositionOffset
			}
			if half < 0 {
				half = 0
			}
			imcbStart = staticPositionOffset - half
			imcbEnd = availableSize - staticPositionOffset - half
			// Mirrors Blink: when the static edge is kCenter and the box
			// overflows the center-clipped IMCB, snap to the start edge
			// instead of splitting negative free space equally. The caller
			// consults hasDefaultAlignmentOverflow on the IMCB struct to
			// arm this fallback; we surface the default bias here.
			defaultOut, hasDefaultOut = BiasStart, true
		case BiasEnd:
			// |<------------*|
			imcbStart = 0
			imcbEnd = availableSize - staticPositionOffset
		}
		biasOut = staticPositionInsetBias
		// Safe bias is chosen from the parallel/perpendicular pair per Blink.
		if isStaticAlignmentParallel {
			safeOut, hasSafeOut = safeInsetBias, hasSafe
		} else {
			safeOut, hasSafeOut = altSafeInsetBias, hasAlt
		}
		return
	}

	// Branches B & C — at least one inset is specified. Resolve auto insets
	// to zero; the IMCB is [imcbStart, available-imcbEnd].
	if hasStart {
		imcbStart = insetStart
	}
	if hasEnd {
		imcbEnd = insetEnd
	}

	// Branch B — exactly one auto. The specified side is the "strong" edge;
	// the auto side is weaker. Bias points to the auto side's opposite
	// (i.e. to the specified side).
	if !hasStart || !hasEnd {
		if hasStart {
			biasOut = BiasStart
		} else {
			biasOut = BiasEnd
		}
		// Safe bias: Blink also forwards it here.
		safeOut, hasSafeOut = safeInsetBias, hasSafe
		return
	}

	// Branch C — both insets specified. The IMCB is fully determined; bias
	// comes from alignment (align-self/justify-self). Overflow handling is
	// deferred to ComputeInsets.
	biasOut = alignmentInsetBias
	safeOut, hasSafeOut = safeInsetBias, hasSafe
	defaultOut, hasDefaultOut = defaultInsetBias, true
	return
}

// ResizeIMCBInOneAxis redistributes `amount` of leftover space between the
// two insets of one axis per the given bias. Mirrors Blink's
// ResizeIMCBInOneAxis (absolute_utils.cc:108).
//
// `amount` can be negative (box larger than IMCB) or positive (box smaller).
// - BiasStart: start-pinned; add `amount` to the end side.
// - BiasEnd:   end-pinned;   add `amount` to the start side.
// - BiasEqual: split evenly (true centering / auto-margin).
func ResizeIMCBInOneAxis(bias InsetBias, amount float64, insetStart, insetEnd *float64) {
	switch bias {
	case BiasStart:
		*insetEnd += amount
	case BiasEnd:
		*insetStart += amount
	case BiasEqual:
		*insetStart += amount / 2
		*insetEnd += amount / 2
	}
}

// ComputeUnclampedIMCB computes the IMCB for both axes. Mirrors Blink's
// ComputeUnclampedIMCB (absolute_utils.cc:196).
//
// alignSelfDirectionIsInline: mirrors Blink's
// LogicalStaticPosition::align_self_direction == kInline. Determines which
// axis is "parallel" to the static-alignment axis (affects safe-bias choice).
//
// static position: `staticOffset{Inline,Block}` with the StaticPositionEdge
// values already mapped to InsetBias via BiasFromStaticEdge.
//
// Returns an IMCB ready to be clamped (the caller may negative-clamp if
// the sum of insets exceeds availableSize; Blink does that inside
// ComputeInsetModifiedContainingBlock, which we add when wiring the caller).
func ComputeUnclampedIMCB(
	availableSize LogicalSize,
	alignment LogicalAlignment,
	insets LogicalOofInsets,
	staticOffsetInline, staticOffsetBlock float64,
	staticInlineBias, staticBlockBias InsetBias,
	alignSelfDirectionIsInline bool,
	containerWDM, selfWDM WritingDirectionMode,
) InsetModifiedContainingBlock {
	// Resolve the alignment biases for each axis.
	inlineAlignBias, inlineHasOverflow, inlineDef, inlineHasDef, inlineSafe, inlineHasSafe :=
		GetAlignmentInsetBias(alignment.InlineAlignment, containerWDM, selfWDM, true)
	blockAlignBias, blockHasOverflow, blockDef, blockHasDef, blockSafe, blockHasSafe :=
		GetAlignmentInsetBias(alignment.BlockAlignment, containerWDM, selfWDM, false)

	// Static-position center (both-auto branch) also arms overflow fallback —
	// ComputeUnclampedIMCBInOneAxis surfaces a default bias in that case but
	// the overflow-armed flag lives on the IMCB struct and is OR'd in here.
	inlineStaticCenter := !insets.HasInlineStart && !insets.HasInlineEnd &&
		staticInlineBias == BiasEqual
	blockStaticCenter := !insets.HasBlockStart && !insets.HasBlockEnd &&
		staticBlockBias == BiasEqual

	imcb := InsetModifiedContainingBlock{
		AvailableSize:                     availableSize,
		HasAutoInlineInset:                !insets.HasInlineStart || !insets.HasInlineEnd,
		HasAutoBlockInset:                 !insets.HasBlockStart || !insets.HasBlockEnd,
		InlineHasDefaultAlignmentOverflow: inlineHasOverflow || inlineStaticCenter,
		BlockHasDefaultAlignmentOverflow:  blockHasOverflow || blockStaticCenter,
	}

	imcb.InlineStart, imcb.InlineEnd,
		imcb.InlineInsetBias,
		imcb.InlineSafeInsetBias, imcb.HasInlineSafeInsetBias,
		imcb.InlineDefaultInsetBias, imcb.HasInlineDefaultInsetBias =
		ComputeUnclampedIMCBInOneAxis(
			availableSize.InlineSize,
			insets.InlineStart, insets.HasInlineStart,
			insets.InlineEnd, insets.HasInlineEnd,
			alignSelfDirectionIsInline,
			staticOffsetInline, staticInlineBias,
			inlineAlignBias,
			inlineDef,
			inlineSafe, inlineHasSafe,
			blockSafe, blockHasSafe,
		)
	_ = inlineHasDef // carried in the return tuple above

	imcb.BlockStart, imcb.BlockEnd,
		imcb.BlockInsetBias,
		imcb.BlockSafeInsetBias, imcb.HasBlockSafeInsetBias,
		imcb.BlockDefaultInsetBias, imcb.HasBlockDefaultInsetBias =
		ComputeUnclampedIMCBInOneAxis(
			availableSize.BlockSize,
			insets.BlockStart, insets.HasBlockStart,
			insets.BlockEnd, insets.HasBlockEnd,
			!alignSelfDirectionIsInline,
			staticOffsetBlock, staticBlockBias,
			blockAlignBias,
			blockDef,
			blockSafe, blockHasSafe,
			inlineSafe, inlineHasSafe,
		)
	_ = blockHasDef

	return imcb
}

// BiasFromStaticEdge maps a StaticPositionEdge (our analog of Blink's
// InlineEdge/BlockEdge) to an InsetBias suitable for the both-auto branch
// of ComputeUnclampedIMCBInOneAxis.
func BiasFromStaticEdge(e StaticPositionEdge) InsetBias {
	switch e {
	case StaticEdgeCenter:
		return BiasEqual
	case StaticEdgeEnd:
		return BiasEnd
	}
	return BiasStart
}

// ComputeMargins resolves auto margins on one axis given the IMCB size and
// used content-box size. Mirrors Blink's ComputeMargins (absolute_utils.cc:256).
//
// Returns (marginStart, marginEnd, appliedAutoMargins). When appliedAutoMargins
// is true, the caller must NOT also call ResizeIMCBInOneAxis — the margins
// already absorbed the leftover. When false, auto margins resolved to zero
// (one of: hasAutoInset is true; margin was not auto; margin box exceeds IMCB).
//
// Parameters:
//   imcbSize            — the IMCB's size on this axis.
//   marginStartLen,
//   marginEndLen        — used margin values (already resolved from % etc.).
//   marginStartIsAuto,
//   marginEndIsAuto     — the original margin specifications.
//   boxSize             — the child's border-box size on this axis.
//   hasAutoInset        — whether either inset on this axis was auto.
//   isStartDominant     — for the overconstrained case: which side wins.
//                         CSS 2.1 §10.3.7 picks the direction-start side in
//                         LTR for inline axis; block axis is always start.
func ComputeMargins(
	imcbSize float64,
	marginStartLen, marginEndLen float64,
	marginStartIsAuto, marginEndIsAuto bool,
	boxSize float64,
	hasAutoInset bool,
	isStartDominant bool,
) (marginStart, marginEnd float64, appliedAutoMargins bool) {
	// If either inset is auto, we can't resolve auto margins against the
	// IMCB (the IMCB isn't defined by both insets). Auto margins → 0.
	// Mirrors Blink's has_auto_inset gate in ComputeMargins.
	if hasAutoInset {
		if marginStartIsAuto {
			marginStart = 0
		} else {
			marginStart = marginStartLen
		}
		if marginEndIsAuto {
			marginEnd = 0
		} else {
			marginEnd = marginEndLen
		}
		return
	}

	freeSpace := imcbSize - boxSize
	switch {
	case marginStartIsAuto && marginEndIsAuto:
		// Both auto: split leftover equally; if negative, the dominant side
		// takes zero and the opposite absorbs the overflow.
		if freeSpace < 0 {
			if isStartDominant {
				marginStart = 0
				marginEnd = freeSpace
			} else {
				marginStart = freeSpace
				marginEnd = 0
			}
		} else {
			marginStart = freeSpace / 2
			marginEnd = freeSpace / 2
		}
		appliedAutoMargins = true
	case marginStartIsAuto:
		marginStart = freeSpace - marginEndLen
		marginEnd = marginEndLen
		appliedAutoMargins = true
	case marginEndIsAuto:
		marginStart = marginStartLen
		marginEnd = freeSpace - marginStartLen
		appliedAutoMargins = true
	default:
		marginStart = marginStartLen
		marginEnd = marginEndLen
	}
	return
}

// ComputeInsets resolves the final inset values on one axis given the IMCB,
// margins, and used size. Mirrors Blink's ComputeInsets (absolute_utils.cc:326).
//
// If appliedAutoMargins is true (ComputeMargins already redistributed
// leftover space via auto margins), the insets are just the IMCB offsets.
// Otherwise, leftover space = available - imcb_start - imcb_end -
// margin_start - margin_end - box_size, and it is distributed via
// ResizeIMCBInOneAxis using the inset bias.
//
// Returns the final inset offsets from the CB's start edge to the
// margin-box start/end edges (inclusive of margins).
//
// TODO(anchor-positioning): Blink takes an optional `anchor_center_offset`
// to bias toward the anchor center. Skipped in this port.
func ComputeInsets(
	availableSize float64,
	imcbStart, imcbEnd float64,
	insetBias InsetBias,
	hasDefaultAlignmentOverflow bool,
	defaultInsetBias InsetBias, hasDefault bool,
	safeInsetBias InsetBias, hasSafe bool,
	marginStart, marginEnd float64,
	boxSize float64,
	appliedAutoMargins bool,
) (insetStart, insetEnd float64) {
	insetStart = imcbStart
	insetEnd = imcbEnd

	if appliedAutoMargins {
		// Margins already absorbed leftover space; nothing to redistribute.
		return
	}

	marginBoxSize := marginStart + marginEnd + boxSize
	freeSpace := availableSize - imcbStart - imcbEnd - marginBoxSize

	// Pick effective bias: if default-overflow is flagged AND there's
	// negative free space AND we have a default bias, snap back to it.
	// Mirrors Blink's "fallback to default when the alignment overflows"
	// rule from ComputeInsets.
	bias := insetBias
	if hasDefaultAlignmentOverflow && freeSpace < 0 && hasDefault {
		bias = defaultInsetBias
	}
	// If `safe` overflow-alignment is set and free space is negative,
	// snap to the safe bias instead (start edge, by convention).
	if hasSafe && freeSpace < 0 {
		bias = safeInsetBias
	}

	ResizeIMCBInOneAxis(bias, freeSpace, &insetStart, &insetEnd)
	return
}
