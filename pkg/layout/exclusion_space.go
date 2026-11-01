package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
	"math"
)

// ExclusionSide indicates which logical side of the inline axis a float
// occupies. This mirrors Blink's EFloat enum (kLeft = inline-start,
// kRight = inline-end in the exclusion space's coordinate system).
type ExclusionSide int

const (
	// ExclusionInlineStart means the float is on the inline-start side.
	ExclusionInlineStart ExclusionSide = iota
	// ExclusionInlineEnd means the float is on the inline-end side.
	ExclusionInlineEnd
)

// IsInlineStartFloat converts a physical CSS float side to a logical
// inline-start/inline-end determination based on the writing direction.
//
// Per CSS Writing Modes §6.2:
//   - float:left  → line-left.  LTR: line-left = inline-start. RTL: line-left = inline-end.
//   - float:right → line-right. LTR: line-right = inline-end.  RTL: line-right = inline-start.
//
// Note: In vertical writing modes, "left" still means line-left (physical top),
// which is inline-start in LTR vertical modes and inline-end in RTL vertical modes.
// The physical-to-line-left/right mapping is handled by the CSS spec: float:left
// always goes to line-left regardless of writing mode.
func IsInlineStartFloat(physicalSide css.FloatType, wdm WritingDirectionMode) bool {
	if wdm.Dir == DirectionLTR {
		return physicalSide == css.FloatLeft
	}
	return physicalSide == css.FloatRight
}

// PhysicalFloatToExclusionSide converts a physical CSS float value to the
// logical ExclusionSide using the given writing direction mode.
func PhysicalFloatToExclusionSide(physicalSide css.FloatType, wdm WritingDirectionMode) ExclusionSide {
	if IsInlineStartFloat(physicalSide, wdm) {
		return ExclusionInlineStart
	}
	return ExclusionInlineEnd
}

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
	// Side indicates whether this is an inline-start or inline-end float.
	Side ExclusionSide
	// IsInitialLetter: the exclusion of an initial-letter box, which later
	// floats on its side clear (Blink ExclusionArea::kInitialLetterBox,
	// exclusion_space.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
	IsInitialLetter bool
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

	// hasBreakBeforeFloat records, per side, a float that needs to start
	// in the next fragmentainer (pushed there whole); hasBreakInsideFloat
	// one added here that resumes in the next fragmentainer. Indexed by
	// ExclusionSide. Mirrors Blink's has_break_before_{left,right}_float_
	// and has_break_inside_{left,right}_float_ (exclusion_space.h:440-450
	// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), kept per logical side
	// as louis14's exclusions are.
	hasBreakBeforeFloat [2]bool
	hasBreakInsideFloat [2]bool
}

// clone returns a copy of es (an empty space for nil) whose exclusions can
// be appended to without aliasing es.
func (es *ExclusionSpace) clone(extraCapacity int) *ExclusionSpace {
	newES := &ExclusionSpace{}
	if es != nil {
		newES.exclusions = make([]Exclusion, len(es.exclusions), len(es.exclusions)+extraCapacity)
		copy(newES.exclusions, es.exclusions)
		newES.hasBreakBeforeFloat = es.hasBreakBeforeFloat
		newES.hasBreakInsideFloat = es.hasBreakInsideFloat
	}
	return newES
}

// Add returns a new ExclusionSpace with the given exclusion appended.
func (es *ExclusionSpace) Add(e Exclusion) *ExclusionSpace {
	newES := es.clone(1)
	newES.exclusions = append(newES.exclusions, e)
	return newES
}

// InitialLetterClearanceOffset returns the block-end of the initial-letter
// boxes on the side of a float of physicalSide (-Inf when there are none,
// Blink's LayoutUnit::Min()): a float clears them. Mirrors Blink's ExclusionSpace::
// InitialLetterClearanceOffset(EFloat) (exclusion_space.h:128-133, kept by
// Add, exclusion_space.cc:268-275 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) InitialLetterClearanceOffset(physicalSide css.FloatType, wdm WritingDirectionMode) float64 {
	offset := math.Inf(-1)
	if es == nil {
		return offset
	}
	side := PhysicalFloatToExclusionSide(physicalSide, wdm)
	for _, e := range es.exclusions {
		if e.IsInitialLetter && e.Side == side {
			offset = max(offset, e.BlockEnd())
		}
	}
	return offset
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

		if e.Side == ExclusionInlineStart {
			// Start-side float: accumulate inline offset from start.
			endEdge := e.InlineOffset + e.InlineSize
			if endEdge > startOffset {
				startOffset = endEdge
			}
		} else if e.Side == ExclusionInlineEnd {
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
//
// The wdm parameter is needed because CSS clear:left/right are physical
// values, but exclusions store logical sides (inline-start/inline-end).
// We convert the physical clear side to logical using the same mapping
// as float placement.
func (es *ExclusionSpace) ClearanceOffset(clearType css.ClearType, currentBlockOffset layoutunit.LayoutUnit, wdm WritingDirectionMode) layoutunit.LayoutUnit {
	if es == nil || clearType == css.ClearNone {
		return currentBlockOffset
	}

	maxBlockEnd := currentBlockOffset.Float64()
	for _, e := range es.exclusions {
		shouldClear := false
		switch clearType {
		case css.ClearLeft:
			// clear:left clears floats on the line-left side.
			// float:left also goes to line-left. In LTR, line-left = inline-start.
			// In RTL, line-left = inline-end.
			if IsInlineStartFloat(css.FloatLeft, wdm) {
				shouldClear = e.Side == ExclusionInlineStart
			} else {
				shouldClear = e.Side == ExclusionInlineEnd
			}
		case css.ClearRight:
			// clear:right clears floats on the line-right side.
			if IsInlineStartFloat(css.FloatRight, wdm) {
				shouldClear = e.Side == ExclusionInlineStart
			} else {
				shouldClear = e.Side == ExclusionInlineEnd
			}
		case css.ClearBoth:
			shouldClear = true
		}

		if shouldClear && e.BlockEnd() > maxBlockEnd {
			maxBlockEnd = e.BlockEnd()
		}
	}

	return layoutunit.FromFloat64Round(maxBlockEnd)
}

// FindFloatPosition finds the block offset where a float of the given size
// can be placed. Floats are placed as high as possible (CSS 2.1 §9.5.1 Rule 6),
// dropping down only when there isn't enough inline space.
func (es *ExclusionSpace) FindFloatPosition(side css.FloatType, floatInlineSize, floatBlockSize float64, availableInlineSize, startBlockOffset float64) float64 {
	return es.FindFloatPositionLocal(side, floatInlineSize, floatBlockSize, availableInlineSize, startBlockOffset, 0, availableInlineSize)
}

// FindLocalFloatOffsets queries the exclusion space with BFC-absolute
// coordinates and returns the result as offsets relative to the content box
// at [bfcInlineOrigin, bfcInlineOrigin+contentInlineSize] in BFC space.
//
// This is the BFC-to-local conversion companion to FindAvailableInlineSize.
// For BFC roots (bfcInlineOrigin=0, bfcContainerInlineSize=contentInlineSize)
// it returns the same values as a direct FindAvailableInlineSize call.
func (es *ExclusionSpace) FindLocalFloatOffsets(blockOffset, blockExtent, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize float64) (localStart, localEnd float64) {
	bfcStart, bfcEnd := es.FindAvailableInlineSize(blockOffset, blockExtent, bfcContainerInlineSize)

	localStart = bfcStart - bfcInlineOrigin
	if localStart < 0 {
		localStart = 0
	}

	rightFloatLeftEdge := bfcContainerInlineSize - bfcEnd
	contentRightEdge := bfcInlineOrigin + contentInlineSize
	localEnd = contentRightEdge - rightFloatLeftEdge
	if localEnd < 0 {
		localEnd = 0
	}

	return
}

// FindFloatPositionLocal finds the block offset where a float of the given
// size can be placed, accounting for the containing block's BFC offset.
// contentInlineSize is the containing block's content width. The exclusion
// space is queried in BFC-absolute coordinates and the available-space check
// is clamped to the content area at [bfcInlineOrigin, bfcInlineOrigin+contentInlineSize].
func (es *ExclusionSpace) FindFloatPositionLocal(side css.FloatType, floatInlineSize, floatBlockSize, contentInlineSize, startBlockOffset, bfcInlineOrigin, bfcContainerInlineSize float64) float64 {
	if es == nil || contentInlineSize <= 0 {
		return startBlockOffset
	}

	currentBlock := startBlockOffset

	for attempt := 0; attempt < 100; attempt++ {
		localStart, localEnd := es.FindLocalFloatOffsets(currentBlock, floatBlockSize, bfcInlineOrigin, contentInlineSize, bfcContainerInlineSize)
		remaining := contentInlineSize - localStart - localEnd

		if floatInlineSize <= remaining {
			return currentBlock
		}

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

		if !foundNext {
			return currentBlock
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

// OffsetBlockPosition returns a new ExclusionSpace with all exclusion block
// offsets shifted by -delta. This translates the exclusion space from BFC-origin
// coordinates to a child's local coordinate system, where the child starts at
// `delta` in the parent's block direction.
//
// For example, if a float occupies BFC block [0, 20) and a child element starts
// at BFC block 20, calling OffsetBlockPosition(20) shifts the float to [-20, 0)
// in the child's coordinates, so it no longer affects the child's content.
func (es *ExclusionSpace) OffsetBlockPosition(delta float64) *ExclusionSpace {
	if es == nil || delta == 0 {
		return es
	}
	newES := es.clone(0)
	for i := range newES.exclusions {
		newES.exclusions[i].BlockOffset -= delta
	}
	return newES
}

// SetHasBreakBeforeFloat returns a copy recording that a float on side
// needs to start in the next fragmentainer: no later float may be placed in
// this one (a float cannot start above a preceding float), and clearance
// past it is clearance past the fragmentainer. Mirrors Blink's
// ExclusionSpace::SetHasBreakBeforeFloat (exclusion_space.h:135-146 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) SetHasBreakBeforeFloat(side ExclusionSide) *ExclusionSpace {
	newES := es.clone(0)
	newES.hasBreakBeforeFloat[side] = true
	return newES
}

// SetHasBreakInsideFloat returns a copy recording that a float on side broke
// inside and takes up layout space in the next fragmentainer too, so
// content that clears it cannot fit here. Mirrors Blink's
// ExclusionSpace::SetHasBreakInsideFloat (exclusion_space.h:148-159 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) SetHasBreakInsideFloat(side ExclusionSide) *ExclusionSpace {
	newES := es.clone(0)
	newES.hasBreakInsideFloat[side] = true
	return newES
}

// NeedsClearancePastFragmentainer reports whether clearType clears past a
// float that broke before or inside, i.e. past the fragmentainer end.
// clear:left/right are physical; they map to the side float:left/right
// takes in wdm. Mirrors Blink's
// ExclusionSpace::NeedsClearancePastFragmentainer (exclusion_space.h:
// 161-181 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) NeedsClearancePastFragmentainer(clearType css.ClearType, wdm WritingDirectionMode) bool {
	if es == nil {
		return false
	}
	brokeOn := func(physical css.FloatType) bool {
		side := PhysicalFloatToExclusionSide(physical, wdm)
		return es.hasBreakInsideFloat[side] || es.hasBreakBeforeFloat[side]
	}
	switch clearType {
	case css.ClearLeft:
		return brokeOn(css.FloatLeft)
	case css.ClearRight:
		return brokeOn(css.FloatRight)
	case css.ClearBoth:
		return brokeOn(css.FloatLeft) || brokeOn(css.FloatRight)
	}
	return false
}

// NeedsBreakBeforeFloat reports whether a float with clearType must break
// before: floats cannot start above any preceding float, so once a float
// has been pushed to the next fragmentainer every later one follows; so
// does one that clears a float continuing there. Mirrors Blink's
// ExclusionSpace::NeedsBreakBeforeFloat (exclusion_space.h:183-189 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) NeedsBreakBeforeFloat(clearType css.ClearType, wdm WritingDirectionMode) bool {
	if es == nil {
		return false
	}
	if es.hasBreakBeforeFloat[ExclusionInlineStart] || es.hasBreakBeforeFloat[ExclusionInlineEnd] {
		return true
	}
	return es.NeedsClearancePastFragmentainer(clearType, wdm)
}

// HasFragmentainerBreak reports whether any float broke before or inside.
// Mirrors Blink's ExclusionSpace::HasFragmentainerBreak
// (exclusion_space.h:191-193 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (es *ExclusionSpace) HasFragmentainerBreak() bool {
	return es != nil && (es.hasBreakBeforeFloat != [2]bool{} || es.hasBreakInsideFloat != [2]bool{})
}
