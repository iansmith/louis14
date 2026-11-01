package layout

import (
	"strings"

	"louis14/pkg/css"
)

// BlockContentAlignment is the merged result of align-content for a block
// container. Mirrors Blink's BlockContentAlignment
// (block_layout_algorithm_utils.h:37-44 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type BlockContentAlignment int

const (
	BlockContentAlignmentStart BlockContentAlignment = iota
	BlockContentAlignmentBaseline
	BlockContentAlignmentSafeCenter
	BlockContentAlignmentUnsafeCenter
	BlockContentAlignmentSafeEnd
	BlockContentAlignmentUnsafeEnd
)

// ComputeContentAlignmentForBlock resolves align-content on a block
// container. Mirrors ComputeContentAlignmentForBlock and the non-table-cell
// half of ComputeContentAlignment (block_layout_algorithm_utils.cc:17-122,
// :168-178 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a
// <content-distribution> uses its fallback alignment, last baseline is safe
// end, and an unspecified overflow position behaves as safe. The page-margin
// box (table-cell-like) case is not modelled.
func ComputeContentAlignmentForBlock(style *css.Style) BlockContentAlignment {
	if !isBlockContainerStyle(style) {
		return BlockContentAlignmentStart
	}
	raw, ok := style.Get("align-content")
	if !ok {
		return BlockContentAlignmentStart
	}
	raw = strings.TrimSpace(raw)
	safe := true // OverflowAlignment::kDefault behaves as safe (:60-65).
	switch {
	case strings.HasPrefix(raw, "unsafe "):
		safe = false
		raw = strings.TrimSpace(raw[len("unsafe "):])
	case strings.HasPrefix(raw, "safe "):
		raw = strings.TrimSpace(raw[len("safe "):])
	}
	switch raw {
	case "space-around", "space-evenly":
		return BlockContentAlignmentSafeCenter
	case "last baseline":
		return BlockContentAlignmentSafeEnd
	case "center":
		if safe {
			return BlockContentAlignmentSafeCenter
		}
		return BlockContentAlignmentUnsafeCenter
	case "end", "flex-end":
		if safe {
			return BlockContentAlignmentSafeEnd
		}
		return BlockContentAlignmentUnsafeEnd
	case "baseline", "first baseline":
		return BlockContentAlignmentBaseline
	}
	return BlockContentAlignmentStart
}

// AlignBlockContent shifts a block container's content in the block axis by
// its align-content (or the internal button centering), using the free
// space between the fragment's block-size and contentBlockSize, the content's
// unconstrained block-size (both content-box here: louis14 child offsets are
// content-edge relative). Only the first fragment is aligned. Mirrors Blink's
// AlignBlockContent (block_layout_algorithm_utils.cc:186-232 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), called from
// BlockLayoutAlgorithm::FinishLayout (block_layout_algorithm.cc:1503-1505).
// fragmentContentBlockSize is the fragment's content-box block-size and
// includeBlockEnd whether the fragment carries its block-end edge
// (ShouldIncludeBlockEndBorderPadding).
func AlignBlockContent(style *css.Style, node *LayoutInputNode, breakToken *BlockBreakToken, fragmentContentBlockSize, contentBlockSize float64, includeBlockEnd bool, builder *BoxFragmentBuilder) {
	if breakToken.IsBreakInside() {
		return
	}
	freeSpace := fragmentContentBlockSize - contentBlockSize
	if style.AlignContentBlockCenter() {
		// Buttons have safe alignment.
		if node.IsButtonOrInputButton() {
			freeSpace = max(0, freeSpace)
		}
		builder.MoveChildrenInBlockDirection(freeSpace / 2)
		return
	}
	if _, ok := style.Properties["align-content"]; !ok || !includeBlockEnd {
		return
	}
	alignment := ComputeContentAlignmentForBlock(style)
	if alignment == BlockContentAlignmentSafeCenter || alignment == BlockContentAlignmentSafeEnd {
		freeSpace = max(0, freeSpace)
	}
	switch alignment {
	case BlockContentAlignmentSafeCenter, BlockContentAlignmentUnsafeCenter:
		builder.MoveChildrenInBlockDirection(freeSpace / 2)
	case BlockContentAlignmentSafeEnd, BlockContentAlignmentUnsafeEnd:
		builder.MoveChildrenInBlockDirection(freeSpace)
	}
}
