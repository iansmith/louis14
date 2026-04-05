package layout

import "louis14/pkg/css"

// Exclusion represents a float's occupied region in the formatting context,
// expressed in logical coordinates relative to the BFC origin.
//
// Ported from Blink's ExclusionSpace / ShelfLayout concepts.
type Exclusion struct {
	// InlineOffset is the start of the float along the inline axis.
	InlineOffset float64
	// BlockOffset is the start of the float along the block axis.
	BlockOffset float64
	// InlineSize is the float's margin-box inline-size.
	InlineSize float64
	// BlockSize is the float's margin-box block-size.
	BlockSize float64
	// Side indicates whether this is a start-side or end-side float.
	Side css.FloatType
}

// BlockEnd returns the block-end position of this exclusion.
func (e Exclusion) BlockEnd() float64 {
	return e.BlockOffset + e.BlockSize
}

// ExclusionSpace tracks float exclusions within a block formatting context.
// All coordinates are logical, relative to the BFC origin.
//
// The space is treated as immutable — Add returns a new ExclusionSpace.
type ExclusionSpace struct {
	exclusions []Exclusion
}

// Add returns a new ExclusionSpace with the given exclusion appended.
func (es *ExclusionSpace) Add(e Exclusion) *ExclusionSpace {
	newES := &ExclusionSpace{}
	if es != nil {
		newES.exclusions = make([]Exclusion, len(es.exclusions), len(es.exclusions)+1)
		copy(newES.exclusions, es.exclusions)
	}
	newES.exclusions = append(newES.exclusions, e)
	return newES
}

// FindAvailableInlineSize returns the inline offsets consumed by floats
// at the given block position over the given block extent.
//
// containerInlineSize is the total inline size of the containing block,
// needed to correctly compute end-side float consumption.
//
// Returns (startOffset, endOffset): the amount of inline space consumed
// from the inline-start and inline-end edges respectively.
//
// Content should be placed starting at startOffset and ending at
// (containerInlineSize - endOffset).
func (es *ExclusionSpace) FindAvailableInlineSize(blockOffset, blockExtent, containerInlineSize float64) (startOffset, endOffset float64) {
	if es == nil {
		return 0, 0
	}

	for _, e := range es.exclusions {
		// Check if this exclusion overlaps the given block range.
		// When blockExtent is 0, check if blockOffset falls within the exclusion.
		if blockExtent > 0 {
			if blockOffset >= e.BlockEnd() || blockOffset+blockExtent <= e.BlockOffset {
				continue
			}
		} else {
			if blockOffset < e.BlockOffset || blockOffset >= e.BlockEnd() {
				continue
			}
		}

		if e.Side == css.FloatLeft {
			// Start-side float: accumulate inline offset from start.
			endEdge := e.InlineOffset + e.InlineSize
			if endEdge > startOffset {
				startOffset = endEdge
			}
		} else if e.Side == css.FloatRight {
			// End-side float: the consumed space from the inline-end edge
			// is (containerInlineSize - e.InlineOffset). This correctly
			// accumulates when multiple right-floats stack inward.
			consumed := containerInlineSize - e.InlineOffset
			if consumed > endOffset {
				endOffset = consumed
			}
		}
	}

	return startOffset, endOffset
}

// ClearanceOffset returns the block position needed to clear past floats
// of the specified type. Returns the maximum block-end of all matching
// floats, or currentBlockOffset if no clearing is needed.
func (es *ExclusionSpace) ClearanceOffset(clearType css.ClearType, currentBlockOffset float64) float64 {
	if es == nil || clearType == css.ClearNone {
		return currentBlockOffset
	}

	maxBlockEnd := currentBlockOffset
	for _, e := range es.exclusions {
		shouldClear := false
		switch clearType {
		case css.ClearLeft:
			shouldClear = e.Side == css.FloatLeft
		case css.ClearRight:
			shouldClear = e.Side == css.FloatRight
		case css.ClearBoth:
			shouldClear = true
		}

		if shouldClear && e.BlockEnd() > maxBlockEnd {
			maxBlockEnd = e.BlockEnd()
		}
	}

	return maxBlockEnd
}

// FindFloatPosition finds the block offset where a float of the given size
// can be placed. Floats are placed as high as possible (CSS 2.1 §9.5.1 Rule 6),
// dropping down only when there isn't enough inline space.
func (es *ExclusionSpace) FindFloatPosition(side css.FloatType, floatInlineSize, floatBlockSize float64, availableInlineSize, startBlockOffset float64) float64 {
	if es == nil || availableInlineSize <= 0 {
		return startBlockOffset
	}

	currentBlock := startBlockOffset

	for attempt := 0; attempt < 100; attempt++ {
		startOff, endOff := es.FindAvailableInlineSize(currentBlock, floatBlockSize, availableInlineSize)
		remaining := availableInlineSize - startOff - endOff

		if floatInlineSize <= remaining {
			return currentBlock
		}

		// Find the next block position where an exclusion ends.
		nextBlock := currentBlock + 1
		foundNext := false
		for _, e := range es.exclusions {
			if e.BlockEnd() > currentBlock {
				if !foundNext || e.BlockEnd() < nextBlock {
					nextBlock = e.BlockEnd()
					foundNext = true
				}
			}
		}

		currentBlock = nextBlock

		if currentBlock > startBlockOffset+10000 {
			return startBlockOffset
		}
	}

	return currentBlock
}

// IsEmpty returns true if there are no exclusions.
func (es *ExclusionSpace) IsEmpty() bool {
	return es == nil || len(es.exclusions) == 0
}
