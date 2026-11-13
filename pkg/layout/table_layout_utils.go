package layout

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
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
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A calc() whose pixel part is
// 0 is its percentage, as Blink reads GetPixelsAndPercent for a calc() that
// is not an expression (min()/max()/clamp() are not read).
func cellPercentageInlineSize(style *css.Style, tableWD WritingDirectionMode) (float64, bool) {
	if style == nil {
		return 0, false
	}
	prop := logicalInlineSizeProperty(tableWD)
	percent, ok := style.GetPercentage(prop)
	if !ok {
		percent, ok = calcPercentWithZeroPixels(style, prop)
	}
	if !ok {
		return 0, false
	}
	if maxPercent, ok := style.GetPercentage("max-" + prop); ok {
		percent = min(percent, maxPercent)
	}
	return percent, true
}

// calcPercentWithZeroPixels returns the percentage of prop's calc(), when
// it is a linear calc() whose pixel part is 0: resolved against a 0 base it
// is its pixels, against a 100 base its pixels plus its percent.
func calcPercentWithZeroPixels(style *css.Style, prop string) (float64, bool) {
	v, _ := style.Get(prop)
	if !strings.HasPrefix(strings.TrimSpace(v), "calc(") || !css.IsMathFunctionWithPercent(v) {
		return 0, false
	}
	pixels, ok := style.ResolveLengthPercentage(prop, 0)
	if !ok || pixels != 0 {
		return 0, false
	}
	return style.ResolveLengthPercentage(prop, 100)
}

// Width-distribution guesses (css-tables-3 #width-distribution-algorithm).
const (
	tableMinGuess = iota
	tablePercentageGuess
	tableSpecifiedGuess
	tableMaxGuess
	tableAboveMax
)

// DistributeInlineSizeToComputedInlineSizeAuto distributes targetInlineSize
// to the columns of columnSpan, returning each column's inline-size, by the
// css-tables-3 width distribution algorithm: the min, percentage, specified
// and max guesses, then growth above max. treatTargetSizeAsConstrained lets
// a target above every max grow fixed columns. Mirrors Blink's
// DistributeInlineSizeToComputedInlineSizeAuto
// (table/table_layout_utils.cc:463-775 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no mergeable
// columns.
func DistributeInlineSizeToComputedInlineSizeAuto(targetInlineSize float64, columnSpan []TableTypesColumn, treatTargetSizeAsConstrained bool) []float64 {
	var percentColumnsCount, fixedColumnsCount, autoColumnsCount int
	var guessSizes, guessSizeTotalIncreases [tableAboveMax]float64
	totalPercent, totalAutoMaxInlineSize, totalFixedMaxInlineSize := 0.0, 0.0, 0.0

	for _, column := range columnSpan {
		switch {
		case column.HasPercent:
			percentColumnsCount++
			totalPercent += column.Percent
			percentInlineSize := column.ResolvePercentInlineSize(targetInlineSize)
			guessSizes[tableMinGuess] += column.MinInlineSize
			guessSizes[tablePercentageGuess] += percentInlineSize
			guessSizes[tableSpecifiedGuess] += percentInlineSize
			guessSizes[tableMaxGuess] += percentInlineSize
			guessSizeTotalIncreases[tablePercentageGuess] += percentInlineSize - column.MinInlineSize
		case column.IsConstrained: // Fixed column
			fixedColumnsCount++
			totalFixedMaxInlineSize += column.MaxInlineSize
			guessSizes[tableMinGuess] += column.MinInlineSize
			guessSizes[tablePercentageGuess] += column.MinInlineSize
			guessSizes[tableSpecifiedGuess] += column.MaxInlineSize
			guessSizes[tableMaxGuess] += column.MaxInlineSize
			guessSizeTotalIncreases[tableSpecifiedGuess] += column.MaxInlineSize - column.MinInlineSize
		default: // Auto column
			autoColumnsCount++
			totalAutoMaxInlineSize += column.MaxInlineSize
			guessSizes[tableMinGuess] += column.MinInlineSize
			guessSizes[tablePercentageGuess] += column.MinInlineSize
			guessSizes[tableSpecifiedGuess] += column.MinInlineSize
			guessSizes[tableMaxGuess] += column.MaxInlineSize
			guessSizeTotalIncreases[tableMaxGuess] += column.MaxInlineSize - column.MinInlineSize
		}
	}

	computedSizes := make([]float64, len(columnSpan))
	// Distribution never makes a column narrower than its min inline-size.
	targetInlineSize = max(targetInlineSize, guessSizes[tableMinGuess])

	startingGuess := tableAboveMax
	for i := tableMinGuess; i != tableAboveMax; i++ {
		if guessSizes[i] >= targetInlineSize {
			startingGuess = i
			break
		}
	}

	// grow gives the columns grow selects their share of distributable,
	// in proportion to weight(column) (evenly when the weights sum to 0),
	// added to base(column); the rounding remainder goes to the last of
	// them. Other columns get other(column).
	grow := func(distributable float64, selects func(TableTypesColumn) bool, weight, base, other func(TableTypesColumn) float64, totalWeight float64, count int) {
		remainingDeficit := distributable
		last := -1
		for i, column := range columnSpan {
			if !selects(column) {
				computedSizes[i] = other(column)
				continue
			}
			last = i
			var delta float64
			if totalWeight > 0 {
				delta = distributable * weight(column) / totalWeight
			} else {
				delta = distributable / float64(count)
			}
			remainingDeficit -= delta
			computedSizes[i] = base(column) + delta
		}
		// Blink's remainder is LayoutUnit rounding; here it is float noise.
		if remainingDeficit != 0 && last >= 0 {
			computedSizes[last] += remainingDeficit
		}
	}
	minSize := func(c TableTypesColumn) float64 { return c.MinInlineSize }
	maxSize := func(c TableTypesColumn) float64 { return c.MaxInlineSize }
	percentSize := func(c TableTypesColumn) float64 { return c.ResolvePercentInlineSize(targetInlineSize) }
	isPercent := func(c TableTypesColumn) bool { return c.HasPercent }
	isFixed := func(c TableTypesColumn) bool { return !c.HasPercent && c.IsConstrained }
	isAuto := func(c TableTypesColumn) bool { return !c.HasPercent && !c.IsConstrained }

	switch startingGuess {
	case tableMinGuess:
		// All columns are their min inline-size.
		for i, column := range columnSpan {
			computedSizes[i] = column.MinInlineSize
		}
	case tablePercentageGuess:
		// Percent columns grow in proportion to their percentage size
		// above their min; the others get their min.
		grow(targetInlineSize-guessSizes[tableMinGuess], isPercent,
			func(c TableTypesColumn) float64 { return percentSize(c) - c.MinInlineSize }, minSize, minSize,
			guessSizeTotalIncreases[tablePercentageGuess], percentColumnsCount)
	case tableSpecifiedGuess:
		// Fixed columns grow; auto columns get their min, percent columns
		// their percentage size.
		grow(targetInlineSize-guessSizes[tablePercentageGuess], isFixed,
			func(c TableTypesColumn) float64 { return c.MaxInlineSize - c.MinInlineSize }, minSize,
			func(c TableTypesColumn) float64 {
				if c.HasPercent {
					return percentSize(c)
				}
				return c.MinInlineSize
			},
			guessSizeTotalIncreases[tableSpecifiedGuess], fixedColumnsCount)
	case tableMaxGuess:
		// Auto columns grow; fixed columns get their max, percent columns
		// their percentage size. An exact match gives every auto column
		// its max, avoiding rounding that could wrap a line.
		isExactMatch := targetInlineSize == guessSizes[tableMaxGuess]
		growsAuto := func(c TableTypesColumn) bool { return isAuto(c) && !isExactMatch }
		grow(targetInlineSize-guessSizes[tableSpecifiedGuess], growsAuto,
			func(c TableTypesColumn) float64 { return c.MaxInlineSize - c.MinInlineSize }, minSize,
			func(c TableTypesColumn) float64 {
				if c.HasPercent {
					return percentSize(c)
				}
				return c.MaxInlineSize
			},
			guessSizeTotalIncreases[tableMaxGuess], autoColumnsCount)
	case tableAboveMax:
		distributable := targetInlineSize - guessSizes[tableMaxGuess]
		percentOrMax := func(c TableTypesColumn) float64 {
			if c.HasPercent {
				return percentSize(c)
			}
			return c.MaxInlineSize
		}
		switch {
		case autoColumnsCount > 0:
			// Grow the auto columns.
			grow(distributable, isAuto, maxSize, maxSize, percentOrMax, totalAutoMaxInlineSize, autoColumnsCount)
		case fixedColumnsCount > 0 && treatTargetSizeAsConstrained:
			// Grow the fixed columns.
			grow(distributable, isFixed, maxSize, maxSize, percentOrMax, totalFixedMaxInlineSize, fixedColumnsCount)
		case percentColumnsCount > 0:
			// Only percent columns remain: each grows past its percentage
			// size in proportion to its percent; the rest stay 0.
			grow(distributable, isPercent, func(c TableTypesColumn) float64 { return c.Percent }, percentSize,
				func(TableTypesColumn) float64 { return 0 }, totalPercent, percentColumnsCount)
		}
	}
	return computedSizes
}

// RowBaselineTabulator gathers a row's baseline from its cells: the
// largest ascent and descent of the baseline-aligned cells with a baseline,
// or, when there are none, the smallest block-end border and padding of the
// cells, the baseline then being the row's content end. Mirrors Blink's
// RowBaselineTabulator (table/table_layout_utils.cc:1680-1733 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), less the
// percentage-descendant tracking louis14 has no use for.
type RowBaselineTabulator struct {
	maxCellAscent, maxCellDescent float64
	hasMaxCellAscent              bool
	fallbackCellDescent           float64
	hasFallbackCellDescent        bool
}

// ProcessCell adds a cell's measured fragment: its block size, its first
// baseline (hasBaseline), the block-end border and padding, whether it has
// in-flow content (Blink's HasDescendantsForTablePart), its content
// alignment, and whether it spans rows (which then adds no descent).
func (t *RowBaselineTabulator) ProcessCell(blockSize, baseline float64, hasBaseline bool, blockEndBorderPadding float64, hasDescendants bool, align BlockContentAlignment, isRowspanned bool) {
	if align == BlockContentAlignmentBaseline && hasDescendants && hasBaseline {
		if !t.hasMaxCellAscent || baseline > t.maxCellAscent {
			t.maxCellAscent = baseline
		}
		// A row-spanning cell adds no descent (it stays 0 if it is the
		// first).
		if !isRowspanned && (!t.hasMaxCellAscent || blockSize-baseline > t.maxCellDescent) {
			t.maxCellDescent = blockSize - baseline
		}
		t.hasMaxCellAscent = true
	}
	// css-tables-3 #row-layout: "If there is no such line box or
	// table-row, the baseline is the bottom of content edge of the cell box."
	if !t.hasMaxCellAscent {
		if !t.hasFallbackCellDescent || blockEndBorderPadding < t.fallbackCellDescent {
			t.fallbackCellDescent = blockEndBorderPadding
		}
		t.hasFallbackCellDescent = true
	}
}

// ComputeRowBlockSize grows maxCellBlockSize to fit the baseline-aligned
// cells' largest ascent plus largest descent.
func (t *RowBaselineTabulator) ComputeRowBlockSize(maxCellBlockSize float64) float64 {
	if t.hasMaxCellAscent {
		return max(maxCellBlockSize, t.maxCellAscent+t.maxCellDescent)
	}
	return maxCellBlockSize
}

// ComputeBaseline returns the row's baseline for a row rowBlockSize tall;
// an empty row's is its top.
func (t *RowBaselineTabulator) ComputeBaseline(rowBlockSize float64) float64 {
	if t.hasMaxCellAscent {
		return t.maxCellAscent
	}
	if t.hasFallbackCellDescent {
		return max(0, rowBlockSize-t.fallbackCellDescent)
	}
	return 0
}

// SynchronizeAssignableTableInlineSizeAndColumns distributes the
// assignable table inline size to the columns, by the fixed or the auto
// layout algorithm; the column sizes then sum to it. Mirrors Blink's
// SynchronizeAssignableTableInlineSizeAndColumns (table/
// table_layout_utils.cc:1849-1864 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func SynchronizeAssignableTableInlineSizeAndColumns(assignableTableInlineSize float64, isFixedLayout bool, columnConstraints *TableTypesColumns) []float64 {
	if len(columnConstraints.Data) == 0 {
		return nil
	}
	if isFixedLayout {
		return SynchronizeAssignableTableInlineSizeAndColumnsFixed(assignableTableInlineSize, columnConstraints)
	}
	return DistributeInlineSizeToComputedInlineSizeAuto(assignableTableInlineSize, columnConstraints.Data, true)
}

// SynchronizeAssignableTableInlineSizeAndColumnsFixed distributes a
// fixed-layout table's inline size: fixed columns get their width, scaled
// up to fill the space the percentage columns leave when there are no auto
// columns and down when they overflow the table; percentage columns get
// their share of the table, scaled the same way; auto columns share the
// rest equally, and zero-width constrained columns only when every column
// is one; the last column sized takes the rounding error. Mirrors Blink's
// SynchronizeAssignableTableInlineSizeAndColumnsFixed (table/
// table_layout_utils.cc:776-923 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func SynchronizeAssignableTableInlineSizeAndColumnsFixed(targetInlineSize float64, columnConstraints *TableTypesColumns) []float64 {
	lu := func(v float64) float64 { return layoutunit.FromFloat64Trunc(v).Float64() }
	// Columns of width 0 are treated as auto by all browsers.
	treatAsFixed := func(c TableTypesColumn) bool { return c.IsFixed() && c.MaxInlineSize != 0 }
	isZeroInlineSizeConstrained := func(c TableTypesColumn) bool { return c.IsConstrained && c.MaxInlineSize == 0 }

	data := columnConstraints.Data
	var percentCount, autoCount, fixedCount, zeroCount int
	var totalPercent, totalFixed, assigned float64
	for _, c := range data {
		switch {
		case c.HasPercent:
			percentCount++
			totalPercent += lu(c.ResolvePercentInlineSize(targetInlineSize))
		case treatAsFixed(c):
			fixedCount++
			totalFixed += c.MaxInlineSize
		case isZeroInlineSizeConstrained(c):
			zeroCount++
		default:
			autoCount++
		}
	}
	sizes := make([]float64, len(data))
	last := -1

	// Fixed columns grow if there are no auto columns, into the space the
	// percentage columns leave.
	if fixedCount > 0 {
		scale, scaleAvailable := 1.0, true
		targetFixed := max(0, targetInlineSize-totalPercent)
		scaleUp := totalFixed < targetFixed && autoCount == 0
		scaleDown := totalFixed > targetInlineSize
		if scaleUp || scaleDown {
			if totalFixed != 0 {
				scale = targetFixed / totalFixed
			} else {
				scaleAvailable = false
			}
		}
		for i, c := range data {
			if !treatAsFixed(c) {
				continue
			}
			last = i
			if scaleAvailable {
				sizes[i] = lu(scale * c.MaxInlineSize)
			} else {
				sizes[i] = lu(targetInlineSize / float64(fixedCount))
			}
			assigned += sizes[i]
		}
	}
	if assigned >= targetInlineSize {
		return sizes
	}
	// Percentage columns only grow if there are no auto columns.
	if percentCount > 0 {
		scale, scaleAvailable := 1.0, true
		remaining := targetInlineSize - assigned
		scaleUp := totalPercent < remaining && autoCount == 0
		scaleDown := totalPercent > remaining
		if scaleUp || scaleDown {
			if totalPercent != 0 {
				scale = remaining / totalPercent
			} else {
				scaleAvailable = false
			}
		}
		for i, c := range data {
			if !c.HasPercent {
				continue
			}
			last = i
			if scaleAvailable {
				sizes[i] = lu(scale * c.ResolvePercentInlineSize(targetInlineSize))
			} else {
				sizes[i] = lu(remaining / float64(percentCount))
			}
			assigned += sizes[i]
		}
	}
	// Auto columns, and zero-width columns when every column is one.
	distributing := targetInlineSize - assigned
	distributeZero := zeroCount == len(data)
	for i, c := range data {
		if c.HasPercent || treatAsFixed(c) {
			continue
		}
		if isZeroInlineSizeConstrained(c) && !distributeZero {
			continue
		}
		last = i
		n := autoCount
		if distributeZero {
			n = zeroCount
		}
		sizes[i] = lu(distributing / float64(n))
		assigned += sizes[i]
	}
	if last >= 0 {
		sizes[last] += targetInlineSize - assigned
	}
	return sizes
}

// DistributeColspanCellToColumnsFixed spreads a colspan cell's min and max
// inline-sizes, less the inner border spacing, evenly across the columns it
// spans in a fixed-layout table, to those no cell or <col> sized
// (hasSize), the last of them taking the rounding error; a percentage goes
// to the unconstrained columns. Its min counts only when the cell is
// constrained. Mirrors Blink's DistributeColspanCellToColumnsFixed
// (table/table_layout_utils.cc:924-1005 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no mergeable
// columns.
func DistributeColspanCellToColumnsFixed(colspanCell TableTypesColspanCell, inlineBorderSpacing float64, columnConstraints *TableTypesColumns, hasSize []bool) {
	end := min(colspanCell.StartColumn+colspanCell.Span, len(columnConstraints.Data))
	span := end - colspanCell.StartColumn
	if span <= 0 {
		return
	}
	innerSpacing := inlineBorderSpacing * float64(span-1)
	cell := colspanCell.CellInlineConstraint
	minSize := 0.0
	if cell.IsConstrained {
		minSize = max(0, cell.MinInlineSize-innerSpacing)
	}
	maxSize := max(0, cell.MaxInlineSize-innerSpacing)
	newMin := layoutunit.FromFloat64Trunc(minSize / float64(span)).Float64()
	newMax := layoutunit.FromFloat64Trunc(maxSize / float64(span)).Float64()
	roundingMin, roundingMax := minSize, maxSize
	last := -1
	for i := colspanCell.StartColumn; i < end; i++ {
		column := &columnConstraints.Data[i]
		last = i
		roundingMin -= newMin
		roundingMax -= newMax
		if !hasSize[i] {
			column.IsConstrained = column.IsConstrained || cell.IsConstrained
			column.MinInlineSize, column.MaxInlineSize = newMin, newMax
			hasSize[i] = true
		}
		// Percentages only get distributed over auto columns.
		if !column.HasPercent && !column.IsConstrained && cell.HasPercent {
			column.Percent, column.HasPercent = cell.Percent/float64(span), true
		}
	}
	columnConstraints.Data[last].MinInlineSize += roundingMin
	columnConstraints.Data[last].MaxInlineSize += roundingMax
}

// DistributeColspanCellToColumnsAuto spreads a colspan cell's min and max
// inline-sizes, less the inner border spacing, across the columns it spans
// in an auto-layout table, and a percentage beyond the spanned columns'
// own across the non-percentage ones in proportion to their max. Mirrors
// Blink's DistributeColspanCellToColumnsAuto
// (table/table_layout_utils.cc:1007-1121 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 has no mergeable
// columns.
func DistributeColspanCellToColumnsAuto(colspanCell TableTypesColspanCell, inlineBorderSpacing float64, columnConstraints *TableTypesColumns) {
	if len(columnConstraints.Data) == 0 || colspanCell.StartColumn >= len(columnConstraints.Data) {
		return
	}
	effectiveSpan := min(colspanCell.Span, len(columnConstraints.Data)-colspanCell.StartColumn)
	columnSpan := columnConstraints.Data[colspanCell.StartColumn : colspanCell.StartColumn+effectiveSpan]

	// Inline sizes for redistribution exclude the inner border spacing.
	totalInnerBorderSpacing := float64(len(columnSpan)-1) * inlineBorderSpacing
	cell := colspanCell.CellInlineConstraint
	colspanCellMinInlineSize := max(0, cell.MinInlineSize-totalInnerBorderSpacing)
	colspanCellMaxInlineSize := max(0, cell.MaxInlineSize-totalInnerBorderSpacing)

	if cell.HasPercent {
		columnsPercent := 0.0
		percentColumnsCount, nonpercentColumnsCount := 0, 0
		nonpercentColumnsMaxInlineSize := 0.0
		for _, column := range columnSpan {
			if column.HasPercent {
				percentColumnsCount++
				columnsPercent += column.Percent
			} else {
				nonpercentColumnsCount++
				nonpercentColumnsMaxInlineSize += column.MaxInlineSize
			}
		}
		if surplusPercent := cell.Percent - columnsPercent; surplusPercent > 0 && len(columnSpan) > percentColumnsCount {
			// Give the surplus percentage to the non-percentage columns,
			// in proportion to their max inline-size.
			for i := range columnSpan {
				if columnSpan[i].HasPercent {
					continue
				}
				if nonpercentColumnsMaxInlineSize != 0 {
					columnSpan[i].Percent = surplusPercent * columnSpan[i].MaxInlineSize / nonpercentColumnsMaxInlineSize
				} else {
					columnSpan[i].Percent = surplusPercent / float64(nonpercentColumnsCount)
				}
				columnSpan[i].HasPercent = true
			}
		}
	}

	computedSizes := DistributeInlineSizeToComputedInlineSizeAuto(colspanCellMinInlineSize, columnSpan, true)
	for i := range columnSpan {
		columnSpan[i].MinInlineSize = max(columnSpan[i].MinInlineSize, computedSizes[i])
	}
	computedSizes = DistributeInlineSizeToComputedInlineSizeAuto(colspanCellMaxInlineSize, columnSpan, cell.IsConstrained)
	for i := range columnSpan {
		columnSpan[i].MaxInlineSize = max(columnSpan[i].MinInlineSize, columnSpan[i].MaxInlineSize, computedSizes[i])
	}
}
