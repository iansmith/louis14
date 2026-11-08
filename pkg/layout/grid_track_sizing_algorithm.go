package layout

import (
	"cmp"
	"math"
	"slices"
	"strings"

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
			// A zero maximum, or one the parser cannot represent:
			// min-content, max-content and percentage maximums all arrive
			// as MaxSize 0 (LOU-760). Either way the growth limit is the
			// minimum (EnsureGrowthLimitIsNotLessThanBaseSize).
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
	// item is the grid item measured, for a pass that re-measures some.
	item                   *gridItem
	start, end             int
	minContent, maxContent float64
	// isScrollContainer: the item's automatic minimum size is 0.
	isScrollContainer bool
	// mainIsAuto: its size in the track axis is auto-like
	// (gridItemIsAutoLike); then minIsAuto: its min size there is auto, and
	// minimumSize: its min size, border box, plus its margins
	// (gridItemMinimum).
	mainIsAuto, minIsAuto bool
	minimumSize           float64
}

func (it *gridSizingItem) span() int { return it.end - it.start }

// gridTrackSizingAlgorithm mirrors Blink's GridTrackSizingAlgorithm for one
// axis.
type gridTrackSizingAlgorithm struct {
	sets       []gridSet
	gutter     float64
	constraint gridSizingConstraint
	available  float64 // gridIndefinite when indefinite
	// stretchAuto: the content distribution lets auto tracks stretch.
	stretchAuto bool
	// forColumns: the tracks are columns; the sizing constraint applies to
	// their free space only.
	forColumns bool
	// minAvailable is the container's definite min size in this axis, 0
	// when it has none: StretchAutoTracks' free space when the available
	// size is indefinite (Blink's min_available_size_).
	minAvailable float64
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
// (grid_layout_algorithm.cc:577-815). An intrinsic minimum, for an
// auto-like size, is the item's resolved min size plus margins when its min
// size is not auto, it is a scroll container, or it spans no auto minimum
// or a flexible track with a span over 1; otherwise its min-content
// contribution (CalculateIntrinsicMinimumContribution, grid_layout_utils.cc:
// 506-600 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Not ported: the
// clamp to a fixed spanned maximum.
func (a *gridTrackSizingAlgorithm) contribution(t gridContributionType, it *gridSizingItem) float64 {
	switch t {
	case gridForContentBasedMinimums, gridForIntrinsicMaximums:
		return it.minContent
	case gridForIntrinsicMinimums:
		if !it.mainIsAuto {
			return it.minContent
		}
		special := !a.spansAny(it, gridTrackSize.hasAutoMin) || (a.spansFlexible(it) && it.span() > 1)
		if !it.minIsAuto || it.isScrollContainer || special {
			return it.minimumSize
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
			// Least growth potential first, infinite last (gridIndefinite
			// is +Inf, so it orders last).
			slices.SortStableFunc(toGrow, func(l, r *gridSet) int {
				return cmp.Compare(growthPotential(l, t, true), growthPotential(r, t, true))
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
	type ordering struct {
		it       *gridSizingItem
		flexible bool
	}
	var ordered []ordering
	for _, it := range items {
		if a.spansIntrinsic(it) {
			ordered = append(ordered, ordering{it, a.spansFlexible(it)})
		}
	}
	// Items not spanning a flexible track first, by span size; then those
	// spanning one, in order.
	slices.SortStableFunc(ordered, func(l, r ordering) int {
		if l.flexible || r.flexible {
			return cmp.Compare(boolInt(l.flexible), boolInt(r.flexible))
		}
		return cmp.Compare(l.it.span(), r.it.span())
	})
	group := func(o []ordering) []*gridSizingItem {
		g := make([]*gridSizingItem, len(o))
		for i := range o {
			g[i] = o[i].it
		}
		return g
	}
	begin := 0
	for begin < len(ordered) && !ordered[begin].flexible {
		end := begin + 1
		for end < len(ordered) && !ordered[end].flexible && ordered[end].it.span() == ordered[begin].it.span() {
			end++
		}
		g := group(ordered[begin:end])
		for _, t := range []gridContributionType{gridForIntrinsicMinimums, gridForContentBasedMinimums,
			gridForMaxContentMinimums, gridForIntrinsicMaximums, gridForMaxContentMaximums} {
			a.increaseTrackSizesToAccommodateGridItems(g, t, false)
		}
		begin = end
	}
	if begin < len(ordered) {
		for _, t := range []gridContributionType{gridForIntrinsicMinimums, gridForContentBasedMinimums, gridForMaxContentMinimums} {
			a.increaseTrackSizesToAccommodateGridItems(group(ordered[begin:]), t, true)
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
	constraint := a.constraint
	if !a.forColumns {
		// Both min-content and max-content block sizes are the size of the
		// content after layout (:1307-1309).
		constraint = gridSizingLayout
	}
	switch constraint {
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

// stretchAutoTracks mirrors StretchAutoTracks (:1054-1107): under a stretch
// (or normal) content distribution, positive definite free space is added
// equally to the tracks with an auto maximum, fit-content excluded.
func (a *gridTrackSizingAlgorithm) stretchAutoTracks() {
	if !a.stretchAuto {
		return
	}
	var toGrow []*gridSet
	for i := range a.sets {
		if a.sets[i].trackSize.hasAutoMax() {
			toGrow = append(toGrow, &a.sets[i])
		}
	}
	// An indefinite free space falls back to the container's definite min
	// size, less the tracks (:1088-1095).
	free := a.determineFreeSpace()
	if free == gridIndefinite {
		free = a.minAvailable - a.totalTrackSize()
	}
	if len(toGrow) == 0 || free <= 0 {
		return
	}
	distributeExtraSpaceToSets(free, 0, gridForFreeSpace, toGrow, toGrow)
	for _, s := range toGrow {
		s.increaseBaseSize(s.baseSize + s.itemIncurredIncrease)
	}
}

// findFrSize is the size of one fr over the sets [start, end) filling
// leftover, treating as inflexible each flexible set whose fr share would
// fall below its base size (FindFrSize in ExpandFlexibleTracks,
// grid_track_sizing_algorithm.cc:1121-1209). A flex factor sum below 1
// counts as 1.
func (a *gridTrackSizingAlgorithm) findFrSize(start, end int, leftover float64) float64 {
	flexSum := 0.0
	var flexible []*gridSet
	for i := start; i < end; i++ {
		if s := &a.sets[i]; s.trackSize.hasFlexMax() && s.flexFactor() != 0 {
			flexSum += s.flexFactor()
			flexible = append(flexible, s)
		} else {
			leftover -= s.baseSize
		}
	}
	leftover -= a.gutter * float64(end-start-1)
	if leftover < 0 || len(flexible) == 0 {
		return 0
	}
	// Largest base size per flex factor first.
	slices.SortStableFunc(flexible, func(l, r *gridSet) int {
		return cmp.Compare(r.baseSize*l.flexFactor(), l.baseSize*r.flexFactor())
	})
	for cur := 0; leftover > 0 && cur < len(flexible); {
		flexSum = max(flexSum, 1)
		next := cur
		for next < len(flexible) && flexible[next].flexFactor()*leftover < flexible[next].baseSize*flexSum {
			next++
		}
		if next == cur {
			return leftover / flexSum
		}
		for _, s := range flexible[cur:next] {
			flexSum -= s.flexFactor()
			leftover -= s.baseSize
		}
		cur = next
	}
	return 0
}

// expandFlexibleTracks mirrors ExpandFlexibleTracks (:1110-1301): with no
// free space, or under a min-content constraint, flexible tracks keep their
// base sizes; with definite free space the fr size fills the available
// size; with indefinite free space it is the largest of each item's
// max-content contribution over the tracks it spans and each flexible
// track's base size per flex factor (a factor below 1 counting as 1). Each
// flexible track then grows to its fr share when that is not smaller.
func (a *gridTrackSizingAlgorithm) expandFlexibleTracks(items []*gridSizingItem) {
	free := a.determineFreeSpace()
	if free == 0 {
		return
	}
	frSize := 0.0
	if free != gridIndefinite {
		frSize = a.findFrSize(0, len(a.sets), a.available)
	} else {
		for _, it := range items {
			if a.spansFlexible(it) {
				frSize = max(frSize, a.findFrSize(it.start, it.end, a.contribution(gridForMaxContentMaximums, it)))
			}
		}
		for i := range a.sets {
			if s := &a.sets[i]; s.trackSize.hasFlexMax() {
				frSize = max(frSize, s.baseSize/max(s.flexFactor(), 1))
			}
		}
	}
	for i := range a.sets {
		if s := &a.sets[i]; s.trackSize.hasFlexMax() {
			if expanded := frSize * s.flexFactor(); expanded >= s.baseSize {
				s.increaseBaseSize(expanded)
			}
		}
	}
}

// computeUsedTrackSizes mirrors ComputeUsedTrackSizes (:305-347).
func (a *gridTrackSizingAlgorithm) computeUsedTrackSizes(items []*gridSizingItem) {
	a.resolveIntrinsicTrackSizes(items)
	a.setIndefiniteGrowthLimitsToBaseSize()
	a.maximizeTracks()
	a.expandFlexibleTracks(items)
	a.stretchAutoTracks()
}

// columnSizingItems measures the items the column sizing of sets reads:
// those spanning an intrinsic track (ResolveIntrinsicTrackSizes) or a
// flexible one (ExpandFlexibleTracks under indefinite free space,
// :1236-1251). Measured once, they serve every constraint sizeColumns runs.
func (gla *GridLayoutAlgorithm) columnSizingItems(sets []gridSet, items []*gridItem) []*gridSizingItem {
	a := &gridTrackSizingAlgorithm{sets: sets}
	wdm := gla.space.WritingDirection
	var sizing []*gridSizingItem
	for _, item := range items {
		it := &gridSizingItem{item: item, start: item.colStart, end: min(item.colEnd, len(sets))}
		if it.start < 0 || it.end <= it.start || (!a.spansIntrinsic(it) && !a.spansFlexible(it)) {
			continue
		}
		var edges float64
		it.minContent, it.maxContent, edges = gridItemInlineContribution(item.node, item.style, wdm, gla.ctx, gla.space)
		it.isScrollContainer = item.style.IsScrollContainer()
		inlineProp := logicalInlineSizeProperty(wdm)
		if it.mainIsAuto = gridItemIsAutoLike(item.style, inlineProp); it.mainIsAuto {
			it.minimumSize, it.minIsAuto = gridItemMinimum(it, item.style, wdm, inlineProp, edges)
		}
		sizing = append(sizing, it)
	}
	return sizing
}

// sizeColumns runs the column track sizing algorithm over measured items,
// sizing sets (from newGridSets for available) in place.
func (gla *GridLayoutAlgorithm) sizeColumns(sets []gridSet, available, gap float64, sizing []*gridSizingItem, constraint gridSizingConstraint) []float64 {
	return sizeTracks(sets, available, 0, gap, sizing, constraint, gla.stretchesAutoTracks("justify-content"), true)
}

// sizeTracks runs the track sizing algorithm over measured items, sizing
// sets (from newGridSets for available) in place, and returns their used
// sizes. minAvailable is the container's definite min size in the axis.
func sizeTracks(sets []gridSet, available, minAvailable, gap float64, sizing []*gridSizingItem, constraint gridSizingConstraint, stretchAuto, forColumns bool) []float64 {
	available = columnAvailable(available)
	a := &gridTrackSizingAlgorithm{
		sets: sets, gutter: gap,
		constraint: constraint, available: available,
		stretchAuto: stretchAuto, minAvailable: minAvailable, forColumns: forColumns,
	}
	a.computeUsedTrackSizes(sizing)
	sizes := make([]float64, len(a.sets))
	for i := range a.sets {
		sizes[i] = a.sets[i].baseSize
	}
	return sizes
}

// columnAvailable is an available size for the track sizing: Indefinite
// as gridIndefinite.
func columnAvailable(available float64) float64 {
	if available < 0 {
		return gridIndefinite
	}
	return available
}

// stretchesAutoTracks reports whether the content distribution property
// (justify-content for columns, align-content for rows) lets auto tracks
// stretch: stretch, or normal (StretchAutoTracks, :1062-1070).
func (gla *GridLayoutAlgorithm) stretchesAutoTracks(prop string) bool {
	v, _ := gla.style.Get(prop)
	return v == "" || v == "normal" || v == "stretch"
}

// layoutRowContribution is how layout measures a parallel item's block
// size for its row contribution: its first-pass layout at its columns'
// size, unless that resolved a percentage block size (block-size, or its
// min or max) against the grid's definite block size; then it is laid out
// again at its columns with its grid area's block size indefinite, as
// Blink measures it (CreateConstraintSpaceForMeasure,
// grid_layout_algorithm.cc:1460-1500 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), storing nothing on the box.
// The re-measures are cached per item for the pass.
func (gla *GridLayoutAlgorithm) layoutRowContribution(colSizes []float64, colGap float64, hasExplicitBlock bool) rowContributionFunc {
	wdm := gla.space.WritingDirection
	blockProp := logicalBlockSizeProperty(wdm)
	remeasured := map[*gridItem]float64{}
	return func(item *gridItem) (float64, LogicalEdges) {
		if !hasExplicitBlock || !gridItemHasPercentageSize(item.style, blockProp) {
			return NewLogicalFragment(wdm, item.result.Fragment).BlockSize(), item.margins
		}
		if b, ok := remeasured[item]; ok {
			return b, item.margins
		}
		b := gla.measureItemAtColumns(item, colSizes, colGap)
		remeasured[item] = b
		return b, item.margins
	}
}

// rowContributionFunc gives a parallel item's block size and margins for
// its row contribution.
type rowContributionFunc func(*gridItem) (float64, LogicalEdges)

// gridItemHasPercentageSize reports whether the size property prop, or its
// min or max, is a percentage (or calc() with one).
func gridItemHasPercentageSize(style *css.Style, prop string) bool {
	for _, p := range []string{prop, "min-" + prop, "max-" + prop} {
		if v, ok := style.Get(p); ok && (strings.HasSuffix(strings.TrimSpace(v), "%") || css.IsMathFunctionWithPercent(v)) {
			return true
		}
	}
	return false
}

// measureItemAtColumns is a parallel item's block size laid out at its
// columns, as the first pass lays it out (layoutItemAtColumns), with its
// grid area's block size indefinite, storing nothing on the box.
func (gla *GridLayoutAlgorithm) measureItemAtColumns(item *gridItem, colSizes []float64, colGap float64) float64 {
	enableSideEffects := gla.ctx.DisableLayoutSideEffects()
	result, _ := gla.layoutItemAtColumns(item, colSizes, colGap, Indefinite)
	enableSideEffects()
	return NewLogicalFragment(gla.space.WritingDirection, result.Fragment).BlockSize()
}

// computeRowSizes sizes a placed grid's rows for layout from their items'
// layout at their columns' size: Blink's ComputeUsedTrackSizes for kForRows
// (grid_layout_algorithm.cc:1164-1189 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). available is the grid's content
// block size or Indefinite; minAvailable its definite min block size.
func (gla *GridLayoutAlgorithm) computeRowSizes(tracks []css.GridTrack, available, minAvailable, gap float64, items []*gridItem, contribution rowContributionFunc) []float64 {
	sets := newGridSets(tracks, columnAvailable(available))
	return sizeTracks(sets, available, minAvailable, gap, gla.rowSizingItems(sets, items, contribution), gridSizingLayout, gla.stretchesAutoTracks("align-content"), false)
}

// rowSizingItems measures the items the row sizing of sets reads, from their
// layout at their columns' size (item.result). A parallel item contributes
// its block size, margins included, for every contribution type; an
// orthogonal one, whose inline axis the rows are, its min-content and
// max-content inline sizes (ContributionSizeForGridItem's MinContentSize /
// MaxContentSize / BlockContributionSize, grid_layout_algorithm.cc:577-815 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (gla *GridLayoutAlgorithm) rowSizingItems(sets []gridSet, items []*gridItem, parallelBlock rowContributionFunc) []*gridSizingItem {
	a := &gridTrackSizingAlgorithm{sets: sets}
	wdm := gla.space.WritingDirection
	blockProp := logicalBlockSizeProperty(wdm)
	var sizing []*gridSizingItem
	for _, item := range items {
		it := &gridSizingItem{item: item, start: item.rowStart, end: min(item.rowEnd, len(sets))}
		if it.start < 0 || it.end <= it.start || (!a.spansIntrinsic(it) && !a.spansFlexible(it)) {
			continue
		}
		var edges float64
		childWDM := NewWritingDirectionMode(item.style)
		if wdm.IsOrthogonalTo(childWDM) {
			geom := ComputeFragmentGeometry(item.style, childWDM)
			margins := ResolveMargins(item.style, childWDM, 0)
			edges = geom.InlineBorderPadding() + margins.InlineSum()
			space := NewConstraintSpaceBuilder(wdm, childWDM, true).
				SetInlineAutoBehavior(AutoSizeBehaviorStretchImplicit).
				SetOrthogonalFallbackInlineSize(orthogonalFallbackSize(childWDM, gla.ctx)).
				SetOrthogonalFallbackBlockSize(gla.space.OrthogonalFallbackBlockSize).
				Build()
			mm := ComputeMinMaxSizes(gla.ctx, item.node, space)
			it.minContent, it.maxContent = mm.MinContent+edges, mm.MaxContent+edges
		} else {
			blockSize, margins := parallelBlock(item)
			edges = ComputeFragmentGeometry(item.style, wdm).BlockBorderPadding() + margins.BlockStart + margins.BlockEnd
			block := blockSize + margins.BlockStart + margins.BlockEnd
			it.minContent, it.maxContent = block, block
		}
		it.isScrollContainer = item.style.IsScrollContainer()
		if it.mainIsAuto = gridItemIsAutoLike(item.style, blockProp); it.mainIsAuto {
			it.minimumSize, it.minIsAuto = gridItemMinimum(it, item.style, wdm, blockProp, edges)
		}
		sizing = append(sizing, it)
	}
	return sizing
}

// gridItemIsAutoLike reports whether an item's size property prop (the
// container's inline or block size property) is auto-like for its minimum
// contribution: auto,
// fit-content, stretch, a percentage, or calc() with a percentage (Blink's
// kAuto, kFitContent, kStretch, kPercent, kCalculated: a calc() without a
// percentage is a fixed length; ConvertLengthSizing,
// style_builder_converter.cc:2130-2156 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The property is the
// container's, so an orthogonal item's other axis is read, as Blink reads
// its LogicalHeight for columns.
func gridItemIsAutoLike(style *css.Style, prop string) bool {
	main, _ := style.Get(prop)
	switch main = strings.TrimSpace(main); main {
	case "", "auto", "fit-content", "-webkit-fit-content", "stretch", "-webkit-fill-available":
		return true
	}
	return strings.HasSuffix(main, "%") || css.IsMathFunctionWithPercent(main)
}

// gridItemMinimum is an auto-like item's minimum contribution when its
// automatic minimum does not apply, and whether its min size is auto. It is
// its min size, border box, plus its margins: edges is its border, padding
// and margins (gridItemInlineContribution). A percentage min size is
// unresolvable against the indefinite grid area, so it is 0; an intrinsic
// keyword is the matching contribution (CalculateIntrinsicMinimumContribution,
// grid_layout_utils.cc:506-600 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func gridItemMinimum(it *gridSizingItem, style *css.Style, containerWDM WritingDirectionMode, sizeProp string, edges float64) (minimumSize float64, minIsAuto bool) {
	v, ok := style.Get("min-" + sizeProp)
	switch v = strings.TrimSpace(v); {
	case !ok || v == "" || v == "auto":
		return edges, true
	case v == "min-content" || v == "-webkit-min-content":
		return it.minContent, false
	case v == "max-content" || v == "-webkit-max-content", v == "fit-content" || v == "-webkit-fit-content":
		// fit-content against an indefinite available size is max-content.
		return it.maxContent, false
	case strings.HasSuffix(v, "%") || css.IsMathFunctionWithPercent(v):
		return edges, false
	}
	geom := ComputeFragmentGeometry(style, containerWDM)
	space := ConstraintSpace{WritingDirection: containerWDM}
	if sizeProp == logicalBlockSizeProperty(containerWDM) {
		return ResolveMinBlockSize(style, containerWDM, space, geom).Float64() + edges, false
	}
	return ResolveMinInlineSize(style, containerWDM, space, geom).Float64() + edges, false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// measureBlockSizeDependentColumns is the additional column pass of a
// grid's intrinsic inline size: when an item's column contribution depends
// on its rows (an orthogonal item, whose block size is its inline axis'
// contribution), the rows are sized from the column sizes, and the columns
// sized again with those items measured against their rows' span. Mirrors
// ComputeTotalColumnSize (grid_layout_algorithm.cc:287-314): after the
// column pass, HasBlockSizeDependentGridItem sizes the rows under the same
// constraint and reruns the columns (MayChangeBlockSizeDependentGridItem-
// Contributions, :1146-1162) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
// Not ported: block-size dependence other than orthogonality (a percentage
// or stretched block size).
func (gla *GridLayoutAlgorithm) measureBlockSizeDependentColumns(tree gridSizingTree, colSets []gridSet, colSizing []*gridSizingItem, colSizes []float64, colGap float64, constraint gridSizingConstraint, measured map[gridItemMeasure]float64) []float64 {
	if !hasBlockSizeDependentSizingItem(gla.space.WritingDirection, colSizing) {
		return colSizes
	}
	// The rows under the same constraint, at the grid's available block
	// size (its definite block size clamped by min/max, or indefinite) and
	// its min available block size (grid_available_size_ /
	// grid_min_available_size_, :287-314).
	wdm := gla.space.WritingDirection
	geom := ComputeFragmentGeometry(gla.style, wdm, gla.space.PercentageResolutionInlineSize)
	rowAvailable := float64(Indefinite)
	if b, ok := ResolveBlockSize(gla.style, wdm, gla.space, geom); ok {
		rowAvailable = gla.clampBlockSizeAround(b.Float64(), b.Float64(), wdm, geom)
	}
	rowMinAvailable := max(0, ResolveMinBlockSize(gla.style, wdm, gla.space, geom).Float64())
	rowGap, _ := gla.gridGaps(Indefinite, rowAvailable)
	rowSets := newGridSets(tree.rowTracks, columnAvailable(rowAvailable))
	rowItems := gla.rowSizingItems(rowSets, tree.items, func(item *gridItem) (float64, LogicalEdges) {
		// Measured once per item and column span across both constraints.
		key := gridItemMeasure{item, gla.spannedSize(colSizes, item.colStart, item.colEnd, colGap)}
		b, ok := measured[key]
		if !ok {
			b = gla.measureItemAtColumns(item, colSizes, colGap)
			measured[key] = b
		}
		return b, ResolveMargins(item.style, wdm, 0)
	})
	rowSizes := sizeTracks(rowSets, rowAvailable, rowMinAvailable, rowGap, rowItems, constraint, gla.stretchesAutoTracks("align-content"), false)
	sizing := gla.remeasureBlockSizeDependentItems(colSizing, rowSizes, rowGap)
	return gla.sizeColumns(slices.Clone(colSets), gridIndefinite, colGap, sizing, constraint)
}

// gridItemMeasure keys a parallel item's block-size measure by the column
// span it was measured at.
type gridItemMeasure struct {
	item   *gridItem
	inline float64
}

// hasBlockSizeDependentSizingItem reports whether an item the column sizing
// measured depends on its rows: an orthogonal item. Gating on the sizing
// items, not all items, keeps a grid with no such item from sizing its rows
// during measurement at all.
func hasBlockSizeDependentSizingItem(wdm WritingDirectionMode, colSizing []*gridSizingItem) bool {
	for _, it := range colSizing {
		if wdm.IsOrthogonalTo(NewWritingDirectionMode(it.item.style)) {
			return true
		}
	}
	return false
}

// remeasureBlockSizeDependentItems is colSizing with each orthogonal
// item's contribution measured against its rows' span (the single layout,
// orthogonalFlowRootContribution, plus margins), or nil when there is none;
// the additional column pass, for measurement and for layout.
func (gla *GridLayoutAlgorithm) remeasureBlockSizeDependentItems(colSizing []*gridSizingItem, rowSizes []float64, rowGap float64) []*gridSizingItem {
	if !hasBlockSizeDependentSizingItem(gla.space.WritingDirection, colSizing) {
		return nil
	}
	wdm := gla.space.WritingDirection
	sizing := make([]*gridSizingItem, 0, len(colSizing))
	for _, it := range colSizing {
		if childWDM := NewWritingDirectionMode(it.item.style); wdm.IsOrthogonalTo(childWDM) {
			rows := gla.spannedSize(rowSizes, it.item.rowStart, min(it.item.rowEnd, len(rowSizes)), rowGap)
			// Against its grid area: its rows' span is its available and
			// percentage block size (CreateConstraintSpaceForMeasure,
			// grid_layout_algorithm.cc:1460-1500).
			c := orthogonalFlowRootContribution(gla.ctx, it.item.node, childWDM, gla.space, rows, rows, gla.space.OrthogonalFallbackBlockSize) +
				ResolveMargins(it.item.style, wdm, 0).InlineSum()
			cp := *it
			cp.minContent, cp.maxContent = c, c
			it = &cp
		}
		sizing = append(sizing, it)
	}
	return sizing
}
