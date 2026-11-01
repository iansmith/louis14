package layout

// BoxInlineSize is the inline-size of the box's first fragment. Mirrors
// BoxInlineSize (layout_box_utils.cc:317-325 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func BoxInlineSize(box *LayoutInputNode) float64 {
	return NewLogicalFragment(NewWritingDirectionMode(box.Style()), box.layoutResults[0].Fragment).InlineSize()
}

// BoxTotalBlockSize is the stitched block-size of all the box's fragments:
// the last fragment with a non-zero block-size, plus what the fragments
// before it consumed. Mirrors BoxTotalBlockSize (layout_box_utils.cc:
// 327-352 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func BoxTotalBlockSize(box *LayoutInputNode) float64 {
	wdm := NewWritingDirectionMode(box.Style())
	results := box.layoutResults
	numFragments := len(results)
	total := 0.0
	for numFragments > 0 {
		if blockSize := NewLogicalFragment(wdm, results[numFragments-1].Fragment).BlockSize(); blockSize > 0 {
			total += blockSize
			break
		}
		numFragments--
	}
	if numFragments > 1 {
		total += results[numFragments-2].BreakToken.ConsumedBlockSize.Float64()
	}
	return total
}
