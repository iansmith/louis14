package layout

import (
	"strings"

	"louis14/pkg/css"
)

// ComputeGridInlineMinMax returns the table grid's min/max inline-sizes,
// border-box with undistributableSpace added: the sum of the column
// constraints, the max raised by the column percentages. Mirrors Blink's
// ComputeGridInlineMinMax (table/table_layout_utils.cc:1743-1824 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeGridInlineMinMax(node *LayoutInputNode, columnConstraints *TableTypesColumns, undistributableSpace float64, isFixedLayout, isLayoutPass bool) MinMaxSizes {
	var minMax MinMaxSizes
	// Minimum total size estimate based on a column's max inline-size and
	// percent.
	percentMaxSizeEstimate := 0.0
	// Sum of the max inline-sizes of the non-percentage columns.
	nonPercentMaxSizeSum := 0.0
	percentSum := 0.0
	for _, column := range columnConstraints.Data {
		// In fixed layout, a constrained column's minimum is its maximum.
		if isFixedLayout && column.IsFixed() {
			minMax.MinContent += column.MaxInlineSize
		} else {
			minMax.MinContent += column.MinInlineSize
		}
		if column.HasPercent && column.Percent > 0 {
			if column.MaxInlineSize > 0 {
				estimate := 100 / column.Percent * (column.MaxInlineSize - column.PercentBorderPadding)
				percentMaxSizeEstimate = max(percentMaxSizeEstimate, estimate)
			}
		} else {
			nonPercentMaxSizeSum += column.MaxInlineSize
		}
		minMax.MaxContent += column.MaxInlineSize
		if column.HasPercent {
			percentSum += column.Percent
		}
	}
	percentSum = min(percentSum, 100)

	// The table's max inline-size follows from the total column
	// percentage and the non-percentage columns' max inline-sizes.
	if percentSum > 0 && AllowColumnPercentages(node, isLayoutPass) {
		sizeFromPercentAndFixed := 0.0
		if nonPercentMaxSizeSum != 0 {
			if percentSum == 100 {
				sizeFromPercentAndFixed = kTableMaxInlineSize
			} else {
				sizeFromPercentAndFixed = 100 / (100 - percentSum) * nonPercentMaxSizeSum
			}
		}
		minMax.MaxContent = max(minMax.MaxContent, sizeFromPercentAndFixed, percentMaxSizeEstimate)
	}

	minMax.MaxContent = max(minMax.MinContent, minMax.MaxContent)
	minMax.MinContent += undistributableSpace
	minMax.MaxContent += undistributableSpace
	return minMax
}

// AllowColumnPercentages reports whether the table's column percentages
// count toward its inline-size: never under width: max-content, always in
// the layout pass, and while sizing intrinsically only outside table
// cells, flexboxes and grids. Mirrors Blink's
// TableNode::AllowColumnPercentages (table/table_node.cc:63-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), walking the layout input
// tree's parents where Blink walks containing blocks.
func AllowColumnPercentages(table *LayoutInputNode, isLayoutPass bool) bool {
	if v, _ := table.Style().Get(logicalInlineSizeProperty(NewWritingDirectionMode(table.Style()))); strings.TrimSpace(v) == "max-content" {
		return false
	}
	if isLayoutPass {
		return true
	}
	for block := table.Parent(); block != nil; block = block.Parent() {
		style := block.Style()
		if style == nil {
			continue
		}
		switch style.GetDisplay() {
		case css.DisplayTableCell, css.DisplayFlex, css.DisplayInlineFlex, css.DisplayGrid, css.DisplayInlineGrid:
			return false
		}
	}
	return true
}

// cellPercentageInlineSize returns a cell's percentage inline-size in the
// table's writing mode, capped by a percentage max-inline-size: the
// percentage part of Blink's InlineSizesFromStyle
// (table/table_layout_algorithm_types.cc:62-74 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). calc() percentages are not
// read.
func cellPercentageInlineSize(style *css.Style, tableWD WritingDirectionMode) (float64, bool) {
	if style == nil {
		return 0, false
	}
	prop := logicalInlineSizeProperty(tableWD)
	percent, ok := style.GetPercentage(prop)
	if !ok {
		return 0, false
	}
	if maxPercent, ok := style.GetPercentage("max-" + prop); ok {
		percent = min(percent, maxPercent)
	}
	return percent, true
}
