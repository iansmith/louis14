package layout

import "math"

// gridTrackBaselines holds one axis's shared baselines, per track: the
// major group's (aligned to the track's start) and the minor group's
// (aligned to its end), unset as -Inf. Mirrors the major/minor baselines of
// Blink's GridLayoutData (SetMajorBaseline / SetMinorBaseline /
// ResetBaselines; core/layout/grid/grid_data.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), keyed by track since every
// louis14 set is one track.
type gridTrackBaselines struct {
	major, minor []float64
	// centralBaseline: the grid's font baseline is central, not
	// alphabetic (ComputedStyle::GetFontBaseline), for synthesized baselines.
	centralBaseline bool
}

func newGridTrackBaselines(trackCount int, centralBaseline bool) *gridTrackBaselines {
	b := &gridTrackBaselines{major: make([]float64, trackCount), minor: make([]float64, trackCount), centralBaseline: centralBaseline}
	for i := range b.major {
		b.major[i], b.minor[i] = math.Inf(-1), math.Inf(-1)
	}
	return b
}

// trackBaseline is the shared baseline an item aligns to: its start-most
// track's major baseline, or its end-most track's minor one ("a box
// spanning multiple shared alignment contexts participates in its
// start-most/end-most one"). Mirrors GetTrackBaseline
// (grid/grid_layout_utils.cc:26-39 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *gridTrackBaselines) trackBaseline(item *gridItem, forColumns bool) float64 {
	start, end := item.span(forColumns)
	if item.axis(forColumns).baselineGroup == BaselineGroupMajor {
		return b.major[start]
	}
	return b.minor[end-1]
}

// setTrackBaseline raises the item's shared baseline to baseline. Mirrors
// SetTrackBaseline (grid_layout_utils.cc:50-64) with the max that
// GridLayoutData::SetMajorBaseline / SetMinorBaseline keep.
func (b *gridTrackBaselines) setTrackBaseline(item *gridItem, forColumns bool, baseline float64) {
	start, end := item.span(forColumns)
	if item.axis(forColumns).baselineGroup == BaselineGroupMajor {
		b.major[start] = math.Max(b.major[start], baseline)
	} else {
		b.minor[end-1] = math.Max(b.minor[end-1], baseline)
	}
}

// gridBaselineFragment is a grid item's fragment read in its baseline
// writing direction for one axis: Blink's LogicalBoxFragment(
// BaselineWritingDirection(direction), fragment) (logical_box_fragment.h:
// 33-78 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type gridBaselineFragment struct {
	item *gridItem
	wd   WritingDirectionMode
}

func (item *gridItem) baselineFragment(forColumns bool) gridBaselineFragment {
	return gridBaselineFragment{item: item, wd: WritingDirectionMode{WM: item.axis(forColumns).baselineWritingMode, Dir: item.wdm.Dir}}
}

// blockSize is the fragment's size along the baseline writing direction's
// block axis.
func (f gridBaselineFragment) blockSize() float64 {
	return NewLogicalFragment(f.wd, f.item.result.Fragment).BlockSize()
}

// baseline is the fragment's first (or last) baseline, or not ok when it
// has none in this writing mode: a fragment has baselines only in its own
// writing mode, and a scroll container's are clamped to its block size.
// Mirrors LogicalBoxFragment::FirstBaseline / LastBaseline. A louis14
// LayoutResult with a first baseline has a last one; a zero LastBaseline
// means the first.
func (f gridBaselineFragment) baseline(forColumns, isLast bool) (float64, bool) {
	r := f.item.result
	if !f.item.axis(forColumns).hasOwnBaselines || !r.HasBaseline {
		return 0, false
	}
	b := r.Baseline
	if isLast && r.LastBaseline > 0 {
		b = r.LastBaseline
	}
	if f.item.node != nil && isScrollContainerBox(f.item.style, f.item.node.DOMNode) {
		b = math.Max(0, math.Min(b, f.blockSize()))
	}
	return b, true
}

// logicalBaseline is the item's baseline measured from its block-start
// edge, or for a last baseline from its block-end edge, synthesized from the
// border box when it has none. Mirrors GetLogicalBaseline
// (grid_layout_utils.cc:41-48) with FirstBaselineOrSynthesize /
// LastBaselineOrSynthesize and SynthesizedBaseline (logical_box_fragment.h:
// 33-78 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (f gridBaselineFragment) logicalBaseline(forColumns, centralBaseline, isLast bool) float64 {
	b, ok := f.baseline(forColumns, isLast)
	if !ok {
		switch {
		case centralBaseline:
			b = f.blockSize() / 2
		case f.wd.IsFlippedLines():
			b = 0
		default:
			b = f.blockSize()
		}
	}
	if isLast {
		return f.blockSize() - b
	}
	return b
}

// computeGridItemBaselines is one axis's shared baselines from the items'
// final layout: each baseline-aligned item's logical baseline plus its
// start margin (end margin for a last baseline) in its baseline writing
// direction. Mirrors the final pass of ComputeGridItemBaselines
// (grid_layout_algorithm.cc:951-1037) with GetExtraMarginForBaseline
// (:540-559) and StoreItemBaseline (grid_layout_utils.cc:66-84 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). nil when no item is
// baseline-aligned in the axis (GridLayoutData::HasBaselines). The
// alignment fallback (SetAlignmentFallback) is LOU-978 PR D.
func computeGridItemBaselines(items []*gridItem, forColumns bool, trackCount int, centralBaseline bool) *gridTrackBaselines {
	var b *gridTrackBaselines
	for _, item := range items {
		a := item.axis(forColumns)
		if !a.isBaselineAligned() || item.result == nil {
			continue
		}
		if b == nil {
			b = newGridTrackBaselines(trackCount, centralBaseline)
		}
		f := item.baselineFragment(forColumns)
		isLast := a.alignment == AxisEdgeLastBaseline
		margins := ToLogicalEdges(ToPhysicalEdges(item.margins, item.wdm), f.wd)
		extraMargin := margins.BlockStart
		if isLast {
			extraMargin = margins.BlockEnd
		}
		b.setTrackBaseline(item, forColumns, extraMargin+f.logicalBaseline(forColumns, centralBaseline, isLast))
	}
	return b
}

// baselineOffset is a baseline-aligned item's offset in its grid area of
// areaSize: its shared baseline less its own, from the area's start for
// the major group, or placed back from the area's end for the minor group;
// 0 for an item that is not baseline-aligned. Mirrors ComputeBaselineOffset
// (grid_layout_utils.cc:86-113 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (b *gridTrackBaselines) baselineOffset(item *gridItem, forColumns bool, areaSize, itemSize float64) float64 {
	a := item.axis(forColumns)
	if b == nil || !a.isBaselineAligned() {
		return 0
	}
	delta := b.trackBaseline(item, forColumns) -
		item.baselineFragment(forColumns).logicalBaseline(forColumns, b.centralBaseline, a.alignment == AxisEdgeLastBaseline)
	if a.baselineGroup == BaselineGroupMajor {
		return delta
	}
	return areaSize - delta - itemSize
}
