package layout

import "louis14/pkg/css"

// CalculateChildAvailableSize returns the content-box size a box offers its
// children as available size: its border-box size shrunk by border,
// scrollbar and padding (an Indefinite block-size stays Indefinite). An
// anonymous space, or an anonymous block with an indefinite block-size,
// passes its own available block-size through. Mirrors Blink's
// CalculateChildAvailableSize (length_utils.cc:1773-1788 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func CalculateChildAvailableSize(space ConstraintSpace, node *LayoutInputNode, geom FragmentGeometry) LogicalSize {
	size := LogicalSize{
		InlineSize: max(0, geom.BorderBoxSize.InlineSize-geom.InlineBorderPadding()),
		BlockSize:  Indefinite,
	}
	if geom.BorderBoxSize.BlockSize != Indefinite {
		size.BlockSize = max(0, geom.BorderBoxSize.BlockSize-geom.BlockBorderPadding())
	}
	size.BlockSize = anonymousChildAvailableBlockSize(space, node, size.BlockSize)
	return size
}

// anonymousChildAvailableBlockSize is CalculateChildAvailableSize's
// anonymous step: an anonymous space, or an anonymous block whose child
// available block-size is indefinite, passes its own available block-size
// through (length_utils.cc:1781-1785 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func anonymousChildAvailableBlockSize(space ConstraintSpace, node *LayoutInputNode, blockSize float64) float64 {
	if space.IsAnonymous || (node != nil && node.IsAnonymous() && blockSize == Indefinite) {
		return space.AvailableSize.BlockSize.Float64()
	}
	return blockSize
}

// ColumnInlineProgression is the inline offset from one column of a
// multicol container with style to the next: the used column inline-size
// plus the column gap, both resolved against availableSize (the multicol's
// content inline-size). Mirrors Blink's ColumnInlineProgression
// (length_utils.cc:1400-1404 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ColumnInlineProgression(style *css.Style, availableSize float64) float64 {
	gap := style.GetColumnGapMulticolWithBase(availableSize)
	_, columnInlineSize := resolveColumnCount(availableSize, style.GetColumnCount(), style.GetColumnWidth(), gap)
	return columnInlineSize + gap
}
