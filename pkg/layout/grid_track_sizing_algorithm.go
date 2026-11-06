package layout

import (
	"math"

	"louis14/pkg/css"
)

// Port of Blink's grid track sizing algorithm, for the column axis:
// GridTrackSizingAlgorithm (core/layout/grid/grid_track_sizing_algorithm.cc),
// GridSet and GridSizingTrackCollection (core/layout/grid/
// grid_track_collection.cc), all @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
// louis14 has no repeat() sets, so each set holds one track (Blink's
// track_count is always 1 here).

// gridIndefinite is Blink's kIndefiniteSize for a growth limit, a
// fit-content limit or a free space: infinite.
var gridIndefinite = math.Inf(1)

// gridSizingConstraint mirrors Blink's SizingConstraint: tracks are sized
// for layout against the available size, or for the container's
// min-content or max-content contribution.
type gridSizingConstraint int

const (
	gridSizingLayout gridSizingConstraint = iota
	gridSizingMinContent
	gridSizingMaxContent
)

// gridBreadthKind is the kind of a track sizing function (Blink's Length
// types as GridTrackSize uses them).
type gridBreadthKind int

const (
	gridBreadthFixed gridBreadthKind = iota
	gridBreadthAuto
	gridBreadthMinContent
	gridBreadthMaxContent
	gridBreadthFlex
)

// gridBreadth is one track sizing function: a length, a keyword, or a flex
// factor.
type gridBreadth struct {
	kind   gridBreadthKind
	length float64 // gridBreadthFixed
	flex   float64 // gridBreadthFlex
}

// gridTrackSize mirrors Blink's GridTrackSize after GridSet normalizes it:
// a min and a max track sizing function, or fit-content(fitContent) when
// isFitContent (min auto, max max-content up to the argument).
type gridTrackSize struct {
	min, max     gridBreadth
	isFitContent bool
	fitContent   float64
}

func (s gridTrackSize) hasFixedMin() bool     { return s.min.kind == gridBreadthFixed }
func (s gridTrackSize) hasFixedMax() bool     { return !s.isFitContent && s.max.kind == gridBreadthFixed }
func (s gridTrackSize) hasAutoMin() bool      { return s.min.kind == gridBreadthAuto }
func (s gridTrackSize) hasIntrinsicMin() bool { return !s.hasFixedMin() }
func (s gridTrackSize) hasMinOrMaxContentMin() bool {
	return s.min.kind == gridBreadthMinContent || s.min.kind == gridBreadthMaxContent
}
func (s gridTrackSize) hasMaxContentMin() bool { return s.min.kind == gridBreadthMaxContent }
func (s gridTrackSize) hasFlexMax() bool       { return !s.isFitContent && s.max.kind == gridBreadthFlex }
func (s gridTrackSize) hasAutoMax() bool       { return !s.isFitContent && s.max.kind == gridBreadthAuto }
func (s gridTrackSize) hasIntrinsicMax() bool  { return !s.hasFixedMax() && !s.hasFlexMax() }
func (s gridTrackSize) hasMaxContentOrAutoMax() bool {
	return s.isFitContent || s.max.kind == gridBreadthMaxContent || s.max.kind == gridBreadthAuto
}

// newGridTrackSize maps a louis14 track definition to Blink's normalized
// GridTrackSize (GridSet::GridSet, grid_track_collection.cc:389-421): a
// percentage resolves against a definite available size and is auto
// otherwise, and a flexible minimum is auto. louis14 parses a minmax()
// minimum only as a length (LOU-760), so it stays fixed here.
func newGridTrackSize(t css.GridTrack, available float64) gridTrackSize {
	fixed := func(l float64) gridBreadth { return gridBreadth{kind: gridBreadthFixed, length: l} }
	auto := gridBreadth{kind: gridBreadthAuto}
	switch {
	case t.IsFitContent:
		return gridTrackSize{min: auto, isFitContent: true, fitContent: t.FitContentMax}
	case t.IsMinMax:
		s := gridTrackSize{min: fixed(t.MinSize)}
		switch {
		case t.MaxFr > 0:
			s.max = gridBreadth{kind: gridBreadthFlex, flex: t.MaxFr}
		case t.MaxAuto:
			s.max = auto
		case t.MaxSize > 0:
			s.max = fixed(t.MaxSize)
		default:
			s.max = fixed(t.MinSize)
		}
		return s
	case t.Fr > 0:
		return gridTrackSize{min: auto, max: gridBreadth{kind: gridBreadthFlex, flex: t.Fr}}
	case t.MinContent:
		b := gridBreadth{kind: gridBreadthMinContent}
		return gridTrackSize{min: b, max: b}
	case t.MaxContent:
		b := gridBreadth{kind: gridBreadthMaxContent}
		return gridTrackSize{min: b, max: b}
	case t.Percent > 0:
		if available >= 0 && available != gridIndefinite {
			return gridTrackSize{min: fixed(available * t.Percent / 100), max: fixed(available * t.Percent / 100)}
		}
		return gridTrackSize{min: auto, max: auto}
	case t.Auto:
		return gridTrackSize{min: auto, max: auto}
	}
	return gridTrackSize{min: fixed(t.Size), max: fixed(t.Size)}
}

// gridSet mirrors Blink's GridSet for one track: its normalized sizing
// functions, base size and growth limit (gridIndefinite while infinite),
// and the scratch state of the distribution steps.
type gridSet struct {
	trackSize            gridTrackSize
	baseSize             float64
	growthLimit          float64
	fitContentLimit      float64 // gridIndefinite unless fit-content
	plannedIncrease      float64 // gridIndefinite: accommodated no item
	itemIncurredIncrease float64
	isInfinitelyGrowable bool
}

// newGridSets builds and initializes the sets for tracks:
// GridSizingTrackCollection::InitializeSets (grid_track_collection.cc:
// 1207-1252). A fixed maximum is the initial growth limit and an intrinsic
// or flexible one is infinite; a fixed minimum is the initial base size and
// an intrinsic one is zero; the growth limit is never below the base size.
func newGridSets(tracks []css.GridTrack, available float64) []gridSet {
	sets := make([]gridSet, len(tracks))
	for i, t := range tracks {
		s := gridSet{trackSize: newGridTrackSize(t, available), fitContentLimit: gridIndefinite, growthLimit: gridIndefinite}
		if s.trackSize.isFitContent {
			s.fitContentLimit = s.trackSize.fitContent
		}
		if s.trackSize.hasFixedMax() {
			s.growthLimit = s.trackSize.max.length
		}
		if s.trackSize.hasFixedMin() {
			s.baseSize = s.trackSize.min.length
		}
		s.ensureGrowthLimitIsNotLessThanBaseSize()
		sets[i] = s
	}
	return sets
}

// definiteGrowthLimit is the growth limit, or the base size while the
// growth limit is infinite (DefiniteGrowthLimit, :351-355).
func (s *gridSet) definiteGrowthLimit() float64 {
	if s.growthLimit == gridIndefinite {
		return s.baseSize
	}
	return s.growthLimit
}

// ensureGrowthLimitIsNotLessThanBaseSize mirrors
// GridSet::EnsureGrowthLimitIsNotLessThanBaseSize (:463-466).
func (s *gridSet) ensureGrowthLimitIsNotLessThanBaseSize() {
	if s.growthLimit != gridIndefinite && s.growthLimit < s.baseSize {
		s.growthLimit = s.baseSize
	}
}

// increaseBaseSize mirrors GridSet::IncreaseBaseSize (:446-452).
func (s *gridSet) increaseBaseSize(v float64) {
	s.baseSize = v
	s.ensureGrowthLimitIsNotLessThanBaseSize()
}

// flexFactor mirrors GridSet::FlexFactor (:424-427).
func (s *gridSet) flexFactor() float64 { return s.trackSize.max.flex }
