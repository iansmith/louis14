package layout

import (
	xbidi "golang.org/x/text/unicode/bidi"
)

// ResolveBidiLevels computes the resolved bidi embedding level for each
// InlineItem in items data, based on the paragraph's base direction and
// the Unicode Bidirectional Algorithm (UAX#9).
//
// After this call, each item's BidiLevel field is 0 (LTR) or 1 (RTL),
// derived from the UAX#9 run structure. Non-text items (open/close tags,
// atomic inlines) inherit the level of the character at their text offset.
//
// This mirrors Blink's BidiParagraph pass in InlineNode::PrepareLayout.
func ResolveBidiLevels(itemsData *InlineItemsData, baseDir Direction) {
	if len(itemsData.TextContent) == 0 {
		return
	}

	defaultDir := xbidi.LeftToRight
	if baseDir == DirectionRTL {
		defaultDir = xbidi.RightToLeft
	}

	// Run the Unicode Bidirectional Algorithm on the full paragraph text.
	var para xbidi.Paragraph
	if _, err := para.SetString(itemsData.TextContent, xbidi.DefaultDirection(defaultDir)); err != nil {
		return
	}
	ordering, err := para.Order()
	if err != nil {
		return
	}

	// Build a per-rune level array from the bidi ordering.
	// bidi.Ordering gives runs with direction LeftToRight (level 0) or
	// RightToLeft (level 1). For our reordering purposes these two levels
	// are sufficient — we don't need deep nesting levels.
	textRunes := []rune(itemsData.TextContent)
	levels := make([]int, len(textRunes))
	for i := 0; i < ordering.NumRuns(); i++ {
		run := ordering.Run(i)
		start, end := run.Pos()
		lvl := 0
		if run.Direction() == xbidi.RightToLeft {
			lvl = 1
		}
		for j := start; j <= end && j < len(levels); j++ {
			levels[j] = lvl
		}
	}

	// Map byte offset → rune index in TextContent.
	// range over a string yields the byte offset of each rune's first byte.
	// InlineItem offsets are always rune-aligned (set by WriteRune calls in
	// CollectInlines), so every item.StartOffset is a valid rune-start byte.
	runeAtByte := make([]int, len(itemsData.TextContent)+1)
	ri := 0
	for bi := range itemsData.TextContent {
		runeAtByte[bi] = ri
		ri++
	}
	runeAtByte[len(itemsData.TextContent)] = ri // sentinel: one past the last rune

	// Assign bidi levels to each InlineItem.
	for _, item := range itemsData.Items {
		offset := item.StartOffset
		if offset >= len(itemsData.TextContent) {
			// Item at or past end of text (e.g. close tag at end of paragraph).
			// Inherit the level of the last character.
			if len(levels) > 0 {
				item.BidiLevel = levels[len(levels)-1]
			}
			continue
		}
		runeIdx := runeAtByte[offset]
		if runeIdx < len(levels) {
			item.BidiLevel = levels[runeIdx]
		}
	}
}

// ReorderLineVisual reorders a line's InlineItemResults from logical order
// to visual order using the UAX#9 L2 algorithm.
//
// L2: From the highest level found in the text to the lowest odd level on
// each line, reverse any contiguous sequence of characters that are at that
// level or higher.
//
// After this call, line.Results is in visual (left-to-right) order and
// createLineBox can place items sequentially with increasing inline offset.
//
// Mirrors Blink's InlineLayoutAlgorithm::BidiReorder().
func ReorderLineVisual(results []InlineItemResult) {
	if len(results) == 0 {
		return
	}

	// Find the highest and lowest-odd bidi levels on this line.
	maxLevel := 0
	minOdd := 256
	for _, r := range results {
		lvl := r.Item.BidiLevel
		if lvl > maxLevel {
			maxLevel = lvl
		}
		if lvl%2 == 1 && lvl < minOdd {
			minOdd = lvl
		}
	}

	// All items are level 0 (LTR) — nothing to reorder.
	if maxLevel == 0 || minOdd == 256 {
		return
	}

	// UAX#9 L2: descend from highest level to lowest odd level.
	// At each level, find all contiguous runs at >= that level and reverse them.
	for lvl := maxLevel; lvl >= minOdd; lvl-- {
		for i := 0; i < len(results); {
			if results[i].Item.BidiLevel >= lvl {
				// Find the end of this contiguous high-level run.
				j := i + 1
				for j < len(results) && results[j].Item.BidiLevel >= lvl {
					j++
				}
				// Reverse results[i:j] in place.
				for a, b := i, j-1; a < b; a, b = a+1, b-1 {
					results[a], results[b] = results[b], results[a]
				}
				i = j
			} else {
				i++
			}
		}
	}
}
