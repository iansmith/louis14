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

// gridContributionType mirrors Blink's GridItemContributionType: which
// contribution an item gives in a step, and which size it grows.
type gridContributionType int

const (
	gridForIntrinsicMinimums gridContributionType = iota
	gridForContentBasedMinimums
	gridForMaxContentMinimums
	gridForIntrinsicMaximums
	gridForMaxContentMaximums
	gridForFreeSpace
)

// gridSizingItem is an in-flow item as the column sizing sees it: the
// tracks [start, end) it spans and its min-content and max-content inline
// contributions, margins included.
type gridSizingItem struct {
	start, end             int
	minContent, maxContent float64
	// isScrollContainer: the item's automatic minimum size is 0.
	isScrollContainer bool
}

func (it *gridSizingItem) span() int { return it.end - it.start }

// gridTrackSizingAlgorithm mirrors Blink's GridTrackSizingAlgorithm for one
// axis.
type gridTrackSizingAlgorithm struct {
	sets       []gridSet
	gutter     float64
	constraint gridSizingConstraint
	available  float64 // gridIndefinite when indefinite
}

func (a *gridTrackSizingAlgorithm) spansAny(it *gridSizingItem, pred func(gridTrackSize) bool) bool {
	for i := it.start; i < it.end; i++ {
		if pred(a.sets[i].trackSize) {
			return true
		}
	}
	return false
}

func (a *gridTrackSizingAlgorithm) spansIntrinsic(it *gridSizingItem) bool {
	return a.spansAny(it, func(s gridTrackSize) bool { return s.hasIntrinsicMin() || s.hasIntrinsicMax() })
}

func (a *gridTrackSizingAlgorithm) spansFlexible(it *gridSizingItem) bool {
	return a.spansAny(it, gridTrackSize.hasFlexMax)
}

// contribution mirrors GridLayoutAlgorithm::ContributionSizeForGridItem
// (grid_layout_algorithm.cc:577-815). An intrinsic minimum is the item's
// automatic minimum: its min-content contribution when it spans an auto
// minimum and no flexible track with a span over 1, and is not a scroll
// container; otherwise 0 (CalculateIntrinsicMinimumContribution, with no
// explicit min-size or fixed-maximum clamp in louis14 yet).
func (a *gridTrackSizingAlgorithm) contribution(t gridContributionType, it *gridSizingItem) float64 {
	switch t {
	case gridForContentBasedMinimums, gridForIntrinsicMaximums:
		return it.minContent
	case gridForIntrinsicMinimums:
		special := !a.spansAny(it, gridTrackSize.hasAutoMin) || (a.spansFlexible(it) && it.span() > 1)
		if special || it.isScrollContainer {
			return 0
		}
		return it.minContent
	}
	return it.maxContent
}

// affectedSize mirrors AffectedSizeForContribution (:360-375).
func affectedSize(s *gridSet, t gridContributionType) float64 {
	if isDistributionForGrowthLimits(t) {
		return s.definiteGrowthLimit()
	}
	return s.baseSize
}

// isContributionAppliedToSet mirrors IsContributionAppliedToSet (:410-428).
func isContributionAppliedToSet(s *gridSet, t gridContributionType) bool {
	switch t {
	case gridForIntrinsicMinimums:
		return s.trackSize.hasIntrinsicMin()
	case gridForContentBasedMinimums:
		return s.trackSize.hasMinOrMaxContentMin()
	case gridForMaxContentMinimums:
		return s.trackSize.hasMaxContentMin()
	case gridForIntrinsicMaximums:
		return s.trackSize.hasIntrinsicMax()
	case gridForMaxContentMaximums:
		return s.trackSize.hasMaxContentOrAutoMax()
	}
	return true
}

// shouldUsedSizeGrowBeyondLimit mirrors ShouldUsedSizeGrowBeyondLimit
// (:434-447).
func shouldUsedSizeGrowBeyondLimit(s *gridSet, t gridContributionType) bool {
	switch t {
	case gridForIntrinsicMinimums, gridForContentBasedMinimums:
		return s.trackSize.hasIntrinsicMax()
	case gridForMaxContentMinimums:
		return s.trackSize.hasMaxContentOrAutoMax()
	}
	return false
}

// isDistributionForGrowthLimits mirrors IsDistributionForGrowthLimits
// (:449-460).
func isDistributionForGrowthLimits(t gridContributionType) bool {
	return t == gridForIntrinsicMaximums || t == gridForMaxContentMaximums
}

// growthPotential mirrors GrowthPotentialForSet (:467-516).
// ignoreInfinitelyGrowable is InfinitelyGrowableBehavior::kIgnore.
func growthPotential(s *gridSet, t gridContributionType, ignoreInfinitelyGrowable bool) float64 {
	switch t {
	case gridForIntrinsicMinimums, gridForContentBasedMinimums, gridForMaxContentMinimums:
		if s.growthLimit == gridIndefinite {
			return gridIndefinite
		}
		return s.growthLimit - (s.baseSize + s.itemIncurredIncrease)
	case gridForIntrinsicMaximums, gridForMaxContentMaximums:
		if !ignoreInfinitelyGrowable && s.growthLimit != gridIndefinite && !s.isInfinitelyGrowable {
			return 0
		}
		if s.fitContentLimit != gridIndefinite {
			return max(0, s.fitContentLimit-s.definiteGrowthLimit()-s.itemIncurredIncrease)
		}
		return gridIndefinite
	}
	return s.growthLimit - s.baseSize
}

// distributeExtraSpaceToSets mirrors DistributeExtraSpaceToSets (:527-761):
// equally when flexFactorSum is 0, else by flex factor. Each set's share
// lands in its itemIncurredIncrease.
func distributeExtraSpaceToSets(extra, flexFactorSum float64, t gridContributionType, toGrow, beyondLimit []*gridSet) {
	if extra == gridIndefinite {
		for _, s := range toGrow {
			s.itemIncurredIncrease = growthPotential(s, t, false)
		}
		return
	}
	equal := flexFactorSum == 0
	growable := 0
	for _, s := range toGrow {
		s.itemIncurredIncrease = 0
		if growthPotential(s, t, false) != 0 {
			growable++
		}
	}
	shareRatioSum := flexFactorSum
	if equal {
		shareRatioSum = float64(growable)
		if growable > 0 || isDistributionForGrowthLimits(t) {
			// Least growth potential first, infinite last.
			sortStable(toGrow, func(l, r *gridSet) bool {
				gl, gr := growthPotential(l, t, true), growthPotential(r, t, true)
				if gl == gridIndefinite || gr == gridIndefinite {
					return gl != gridIndefinite
				}
				return gl < gr
			})
		}
	}
	share := func(s *gridSet, potential float64) float64 {
		if potential == 0 {
			return 0
		}
		ratio := 1.0
		if !equal {
			ratio = s.flexFactor()
		}
		var v float64
		if ratio >= shareRatioSum {
			ratio, v = shareRatioSum, extra
			growable = 0
		} else {
			v = extra * ratio / shareRatioSum
			growable--
		}
		if potential != gridIndefinite {
			v = min(v, potential)
		}
		shareRatioSum -= ratio
		extra -= v
		return v
	}
	for _, s := range toGrow {
		if growable == 0 {
			break
		}
		s.itemIncurredIncrease = share(s, growthPotential(s, t, false))
	}
	if beyondLimit == nil || extra <= 0 {
		return
	}
	beyond := func(s *gridSet) float64 {
		if !isDistributionForGrowthLimits(t) {
			return gridIndefinite
		}
		return growthPotential(s, t, true)
	}
	growable = 0
	for _, s := range beyondLimit {
		if beyond(s) != 0 {
			growable++
		}
	}
	shareRatioSum = float64(growable)
	for _, s := range beyondLimit {
		if growable == 0 {
			break
		}
		s.itemIncurredIncrease += share(s, beyond(s))
	}
}

// sortStable is an insertion sort by less, stable as the small set lists
// here need.
func sortStable(sets []*gridSet, less func(l, r *gridSet) bool) {
	for i := 1; i < len(sets); i++ {
		for j := i; j > 0 && less(sets[j], sets[j-1]); j-- {
			sets[j], sets[j-1] = sets[j-1], sets[j]
		}
	}
}

// growAffectedSizeByPlannedIncrease mirrors
// GrowAffectedSizeByPlannedIncrease (:377-405).
func growAffectedSizeByPlannedIncrease(s *gridSet, t gridContributionType) {
	s.isInfinitelyGrowable = false
	if s.plannedIncrease == gridIndefinite {
		return
	}
	switch t {
	case gridForIntrinsicMinimums, gridForContentBasedMinimums, gridForMaxContentMinimums:
		s.increaseBaseSize(s.baseSize + s.plannedIncrease)
	case gridForIntrinsicMaximums:
		s.isInfinitelyGrowable = s.growthLimit == gridIndefinite
		s.growthLimit = s.definiteGrowthLimit() + s.plannedIncrease
	case gridForMaxContentMaximums:
		s.growthLimit = s.definiteGrowthLimit() + s.plannedIncrease
	}
}

// increaseTrackSizesToAccommodateGridItems mirrors
// IncreaseTrackSizesToAccommodateGridItems (:798-909).
func (a *gridTrackSizingAlgorithm) increaseTrackSizesToAccommodateGridItems(group []*gridSizingItem, t gridContributionType, spanningFlex bool) {
	for i := range a.sets {
		a.sets[i].plannedIncrease = gridIndefinite
	}
	for _, it := range group {
		var toGrow, beyondLimit []*gridSet
		flexFactorSum := 0.0
		spanned := a.gutter * float64(it.span()-1)
		for i := it.start; i < it.end; i++ {
			s := &a.sets[i]
			spanned += affectedSize(s, t)
			if spanningFlex && !s.trackSize.hasFlexMax() {
				continue
			}
			if isContributionAppliedToSet(s, t) {
				if s.plannedIncrease == gridIndefinite {
					s.plannedIncrease = 0
				}
				if spanningFlex {
					flexFactorSum += s.flexFactor()
				}
				toGrow = append(toGrow, s)
				if shouldUsedSizeGrowBeyondLimit(s, t) {
					beyondLimit = append(beyondLimit, s)
				}
			}
		}
		if len(toGrow) == 0 {
			continue
		}
		extra := max(0, a.contribution(t, it)-spanned)
		if extra == 0 {
			continue
		}
		if !spanningFlex || flexFactorSum == 0 {
			if len(beyondLimit) == 0 {
				beyondLimit = toGrow
			}
			distributeExtraSpaceToSets(extra, 0, t, toGrow, beyondLimit)
		} else {
			distributeExtraSpaceToSets(extra, flexFactorSum, t, toGrow, nil)
		}
		for _, s := range toGrow {
			s.plannedIncrease = max(s.itemIncurredIncrease, s.plannedIncrease)
		}
	}
	for i := range a.sets {
		growAffectedSizeByPlannedIncrease(&a.sets[i], t)
	}
}

// resolveIntrinsicTrackSizes mirrors ResolveIntrinsicTrackSizes
// (:911-1022): items spanning an intrinsic track, those not spanning a
// flexible track first, grouped by span size, then all items spanning a
// flexible track together.
func (a *gridTrackSizingAlgorithm) resolveIntrinsicTrackSizes(items []*gridSizingItem) {
	var ordered []*gridSizingItem
	for _, it := range items {
		if a.spansIntrinsic(it) {
			ordered = append(ordered, it)
		}
	}
	flexLess := func(i, j int) bool {
		fi, fj := a.spansFlexible(ordered[i]), a.spansFlexible(ordered[j])
		if fi || fj {
			return !fi && fj
		}
		return ordered[i].span() < ordered[j].span()
	}
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && flexLess(j, j-1); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	begin := 0
	for begin < len(ordered) && !a.spansFlexible(ordered[begin]) {
		end := begin + 1
		for end < len(ordered) && !a.spansFlexible(ordered[end]) && ordered[end].span() == ordered[begin].span() {
			end++
		}
		group := ordered[begin:end]
		for _, t := range []gridContributionType{gridForIntrinsicMinimums, gridForContentBasedMinimums,
			gridForMaxContentMinimums, gridForIntrinsicMaximums, gridForMaxContentMaximums} {
			a.increaseTrackSizesToAccommodateGridItems(group, t, false)
		}
		begin = end
	}
	if begin < len(ordered) {
		for _, t := range []gridContributionType{gridForIntrinsicMinimums, gridForContentBasedMinimums, gridForMaxContentMinimums} {
			a.increaseTrackSizesToAccommodateGridItems(ordered[begin:], t, true)
		}
	}
}

// totalTrackSize is the sum of the base sizes and the gutters between
// them (GridSizingTrackCollection::TotalTrackSize).
func (a *gridTrackSizingAlgorithm) totalTrackSize() float64 {
	total := 0.0
	for i := range a.sets {
		total += a.sets[i].baseSize
	}
	if n := len(a.sets); n > 1 {
		total += a.gutter * float64(n-1)
	}
	return total
}

// determineFreeSpace mirrors DetermineFreeSpace (:1303-1329): what is left
// of a definite available size when sizing for layout, infinite under a
// max-content constraint, and zero under a min-content constraint.
func (a *gridTrackSizingAlgorithm) determineFreeSpace() float64 {
	switch a.constraint {
	case gridSizingMaxContent:
		return gridIndefinite
	case gridSizingMinContent:
		return 0
	}
	if a.available == gridIndefinite {
		return gridIndefinite
	}
	return max(0, a.available-a.totalTrackSize())
}

// setIndefiniteGrowthLimitsToBaseSize mirrors
// GridSizingTrackCollection::SetIndefiniteGrowthLimitsToBaseSize
// (grid_track_collection.cc:1073-1078).
func (a *gridTrackSizingAlgorithm) setIndefiniteGrowthLimitsToBaseSize() {
	for i := range a.sets {
		if a.sets[i].growthLimit == gridIndefinite {
			a.sets[i].growthLimit = a.sets[i].baseSize
		}
	}
}

// maximizeTracks mirrors MaximizeTracks (:1024-1052): positive free space is
// distributed equally to the base sizes, each freezing at its growth limit.
func (a *gridTrackSizingAlgorithm) maximizeTracks() {
	free := a.determineFreeSpace()
	if free == 0 {
		return
	}
	toGrow := make([]*gridSet, len(a.sets))
	for i := range a.sets {
		toGrow[i] = &a.sets[i]
	}
	distributeExtraSpaceToSets(free, 0, gridForFreeSpace, toGrow, nil)
	for _, s := range toGrow {
		s.increaseBaseSize(s.baseSize + s.itemIncurredIncrease)
	}
}
