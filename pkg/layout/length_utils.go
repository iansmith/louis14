package layout

import (
	"math"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry/layoutunit"
)

// CalculateChildAvailableSize returns the content-box size a box offers its
// children as available size: its border-box size shrunk by border,
// scrollbar and padding (an Indefinite block-size stays Indefinite). An
// anonymous space, or an anonymous block with an indefinite block-size,
// passes its own available block-size through. Mirrors Blink's
// CalculateChildAvailableSize (length_utils.cc:1773-1788 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func CalculateChildAvailableSize(space ConstraintSpace, node *LayoutInputNode, geom FragmentGeometry) LogicalSize {
	size := LogicalSize{
		InlineSize: max(0, geom.BorderBoxSize.InlineSize-geom.InlineBorderPadding()),
		BlockSize:  Indefinite,
	}
	if geom.BorderBoxSize.BlockSize != Indefinite {
		size.BlockSize = max(0, geom.BorderBoxSize.BlockSize-geom.BlockBorderPadding())
	}
	size.BlockSize = anonymousChildAvailableBlockSize(space, node, size.BlockSize)
	return size
}

// anonymousChildAvailableBlockSize is CalculateChildAvailableSize's
// anonymous step: an anonymous space, or an anonymous block whose child
// available block-size is indefinite, passes its own available block-size
// through (length_utils.cc:1781-1785 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func anonymousChildAvailableBlockSize(space ConstraintSpace, node *LayoutInputNode, blockSize float64) float64 {
	if space.IsAnonymous || (node != nil && node.IsAnonymous() && blockSize == Indefinite) {
		return space.AvailableSize.BlockSize.Float64()
	}
	return blockSize
}

// ColumnInlineProgression is the inline offset from one column of a
// multicol container with style to the next: the used column inline-size
// plus the column gap, both resolved against availableSize (the multicol's
// content inline-size). Mirrors Blink's ColumnInlineProgression
// (length_utils.cc:1400-1404 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ColumnInlineProgression(style *css.Style, availableSize float64) float64 {
	gap := style.GetColumnGapMulticolWithBase(availableSize)
	_, columnInlineSize := resolveColumnCount(availableSize, style.GetColumnCount(), style.GetColumnWidth(), gap)
	return columnInlineSize + gap
}

// ReplacedSizeMode selects which lengths ComputeReplacedSize ignores.
// Mirrors Blink's ReplacedSizeMode (length_utils.h:384-388 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
type ReplacedSizeMode int

const (
	ReplacedSizeModeNormal ReplacedSizeMode = iota
	// ReplacedSizeModeIgnoreInlineLengths is used for the min/max-content
	// size.
	ReplacedSizeModeIgnoreInlineLengths
	// ReplacedSizeModeIgnoreBlockLengths is used for the "intrinsic"
	// block-size.
	ReplacedSizeModeIgnoreBlockLengths
)

// replacedLengthKind classifies a sizing property value the way Blink's
// ResolveInlineLengthInternal / ResolveBlockLengthInternal switch on
// Length::GetType (length_utils.cc:29-286 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The cascade has already
// unprefixed the keywords and turned `stretch` into 100%
// (normalizeVendorPrefixedValue), so stretch is reached only as the
// auto_length.
type replacedLengthKind int

const (
	replacedLengthAuto replacedLengthKind = iota
	replacedLengthNone
	replacedLengthSpecified // fixed, percent or calc
	replacedLengthMinContent
	replacedLengthMaxContent
	replacedLengthFitContent
	replacedLengthStretch
)

func classifyReplacedLength(val string) replacedLengthKind {
	switch strings.TrimSpace(val) {
	case "", "auto":
		return replacedLengthAuto
	case "none":
		return replacedLengthNone
	case "min-content":
		return replacedLengthMinContent
	case "max-content":
		return replacedLengthMaxContent
	case "fit-content":
		return replacedLengthFitContent
	}
	if css.IsFitContentFunction(val) {
		// fit-content(<length-percentage>) clamps by the min/max-content
		// sizes, which are equal for a replaced box: it resolves like
		// fit-content.
		return replacedLengthFitContent
	}
	return replacedLengthSpecified
}

// replacedLengthLayoutMax is Blink's LayoutUnit::Max(): an unbounded max.
var replacedLengthLayoutMax = math.Inf(1)

// ClampSizeToMinAndMax clamps size to [MinContent, MaxContent], the minimum
// winning. Mirrors Blink's MinMaxSizes::ClampSizeToMinAndMax
// (min_max_sizes.h @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (mm MinMaxSizes) ClampSizeToMinAndMax(size float64) float64 {
	return max(mm.MinContent, min(size, mm.MaxContent))
}

// isIndefiniteInlineSize reports whether an available inline-size is
// Blink's kIndefiniteSize: negative, or the saturated LayoutUnit a
// louis14 max-content measurement width (1e9) is stored as.
func isIndefiniteInlineSize(v float64) bool {
	return v < 0 || v >= layoutunit.MaxValue.Float64()
}

type replacedLengthType int

const (
	replacedLengthTypeMin replacedLengthType = iota
	replacedLengthTypeMain
	replacedLengthTypeMax
)

// replacedLengthResolver resolves the sizing properties of a replaced box,
// all border-box, in the box's writing mode. Mirrors the
// ResolveMain/Min/MaxInlineLength and ResolveMain/Min/MaxBlockLength
// helpers (length_utils.h:111-250 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) over louis14's content-box
// Resolve*Size helpers.
//
// Stretch subtracts the box's margins from the available size, as Blink
// does: the parents of a replaced box pass a margin-inclusive available
// inline-size (the block container's child space, the line's atomic
// inline, a grid area), although louis14's block container passes its
// other children a margin-free one.
type replacedLengthResolver struct {
	style   *css.Style
	wdm     WritingDirectionMode
	space   ConstraintSpace
	geom    FragmentGeometry
	bp      LogicalEdges
	margins LogicalEdges
}

func newReplacedLengthResolver(style *css.Style, space ConstraintSpace) replacedLengthResolver {
	wdm := space.WritingDirection
	geom := replacedFragmentGeometry(style, wdm, space)
	return replacedLengthResolver{
		style:   style,
		wdm:     wdm,
		space:   space,
		geom:    geom,
		bp:      geom.BorderBoxPadding(),
		margins: ResolveMargins(style, wdm, space.PercentageResolutionInlineSize),
	}
}

func (r replacedLengthResolver) inlineProperty(kind string) string {
	if r.wdm.IsVertical() {
		return kind + "height"
	}
	return kind + "width"
}

func (r replacedLengthResolver) blockProperty() string {
	if r.wdm.IsVertical() {
		return "width"
	}
	return "height"
}

// kind is the classified value of prop, an auto value taking autoKind.
func (r replacedLengthResolver) kind(prop string, autoKind replacedLengthKind) replacedLengthKind {
	v, _ := r.style.Get(prop)
	if kind := classifyReplacedLength(v); kind != replacedLengthAuto {
		return kind
	}
	return autoKind
}

// stretchInline is ResolveInlineLengthInternal's kStretch case
// (length_utils.cc:46-65 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r replacedLengthResolver) stretchInline() float64 {
	available := r.space.AvailableSize.InlineSize.Float64()
	if isIndefiniteInlineSize(available) {
		return Indefinite
	}
	return max(r.bp.InlineSum(), available-r.margins.InlineSum())
}

// resolveInline is ResolveInlineLengthInternal (length_utils.cc:29-160 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for a replaced box: sizes is
// its MinMaxSizesFunc, autoKind the auto_length.
func (r replacedLengthResolver) resolveInline(prop string, lengthType replacedLengthType, autoKind replacedLengthKind, sizes func() float64) float64 {
	switch r.kind(prop, autoKind) {
	case replacedLengthStretch:
		return r.stretchInline()
	case replacedLengthSpecified:
		if propertyHasPercent(r.style, prop) && r.space.PercentageResolutionSize.InlineSize.Float64() < 0 {
			if lengthType != replacedLengthTypeMin {
				return Indefinite
			}
			return r.bp.InlineSum()
		}
		var content layoutunit.LayoutUnit
		ok := true
		switch lengthType {
		case replacedLengthTypeMin:
			content = ResolveMinInlineSize(r.style, r.wdm, r.space, r.geom)
		case replacedLengthTypeMain:
			content, ok = ResolveInlineSize(r.style, r.wdm, r.space, r.geom)
		case replacedLengthTypeMax:
			content, ok = ResolveMaxInlineSize(r.style, r.wdm, r.space, r.geom)
		}
		if !ok {
			return Indefinite
		}
		return content.Float64() + r.bp.InlineSum()
	case replacedLengthMinContent, replacedLengthMaxContent:
		return sizes()
	case replacedLengthFitContent:
		if lengthType == replacedLengthTypeMain && isIndefiniteInlineSize(r.space.AvailableSize.InlineSize.Float64()) {
			return Indefinite
		}
		// min-content and max-content are both the replaced size, so
		// shrink-to-fit returns it whatever the available size.
		return sizes()
	case replacedLengthAuto:
		if lengthType == replacedLengthTypeMin {
			return r.bp.InlineSum()
		}
	}
	return Indefinite
}

// resolveBlock is ResolveBlockLengthInternal (length_utils.cc:162-286 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) for a replaced box: blockSize
// is its BlockSizeFunc, autoKind the auto_length.
func (r replacedLengthResolver) resolveBlock(prop string, lengthType replacedLengthType, autoKind replacedLengthKind, blockSize func() float64) float64 {
	switch r.kind(prop, autoKind) {
	case replacedLengthStretch:
		available := r.space.AvailableSize.BlockSize.Float64()
		if available < 0 {
			if lengthType == replacedLengthTypeMain {
				return blockSize()
			}
			return Indefinite
		}
		return max(r.bp.BlockSum(), available-r.margins.BlockSum())
	case replacedLengthSpecified:
		var content layoutunit.LayoutUnit
		ok := true
		switch lengthType {
		case replacedLengthTypeMin:
			if propertyHasPercent(r.style, prop) && r.space.IsBlockSizeIndefinite() {
				return r.bp.BlockSum()
			}
			content = ResolveMinBlockSize(r.style, r.wdm, r.space, r.geom)
		case replacedLengthTypeMain:
			if content, ok = ResolveBlockSize(r.style, r.wdm, r.space, r.geom); !ok {
				// A percentage against an indefinite size behaves as
				// the content size.
				return blockSize()
			}
		case replacedLengthTypeMax:
			content, ok = ResolveMaxBlockSize(r.style, r.wdm, r.space, r.geom)
		}
		if !ok {
			return Indefinite
		}
		return content.Float64() + r.bp.BlockSum()
	case replacedLengthMinContent, replacedLengthMaxContent, replacedLengthFitContent:
		return blockSize()
	case replacedLengthAuto:
		if lengthType == replacedLengthTypeMin {
			return r.bp.BlockSum()
		}
	}
	return Indefinite
}

// minMaxInline is the border-box min/max inline-sizes, the max
// encompassing the min (ResolveMinInlineLength / ResolveMaxInlineLength,
// length_utils.h:111-141 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r replacedLengthResolver) minMaxInline(sizes func() float64) MinMaxSizes {
	mm := MinMaxSizes{
		MinContent: r.resolveInline(r.inlineProperty("min-"), replacedLengthTypeMin, replacedLengthAuto, sizes),
		MaxContent: r.resolveInline(r.inlineProperty("max-"), replacedLengthTypeMax, replacedLengthAuto, sizes),
	}
	return r.encompass(mm, r.bp.InlineSum())
}

// minMaxBlock is the border-box min/max block-sizes, the max encompassing
// the min (ResolveMinBlockLength / ResolveMaxBlockLength,
// length_utils.h:175-220 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (r replacedLengthResolver) minMaxBlock(blockSize func() float64) MinMaxSizes {
	mm := MinMaxSizes{
		MinContent: r.resolveBlock(minBlockProperty(r.wdm), replacedLengthTypeMin, replacedLengthAuto, blockSize),
		MaxContent: r.resolveBlock(maxBlockProperty(r.wdm), replacedLengthTypeMax, replacedLengthAuto, blockSize),
	}
	return r.encompass(mm, r.bp.BlockSum())
}

// encompass maps an indefinite min to bpSum and an indefinite max to
// unbounded, and makes the max encompass the min.
func (r replacedLengthResolver) encompass(mm MinMaxSizes, bpSum float64) MinMaxSizes {
	if mm.MinContent == Indefinite {
		mm.MinContent = bpSum
	}
	if mm.MaxContent == Indefinite {
		mm.MaxContent = replacedLengthLayoutMax
	}
	mm.MaxContent = max(mm.MinContent, mm.MaxContent)
	return mm
}

// InlineSizeFromAspectRatio is the border-box inline-size blockSize (also
// border-box) transfers through aspectRatio. Mirrors Blink's
// InlineSizeFromAspectRatio (length_utils.cc:288-299 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func InlineSizeFromAspectRatio(bp LogicalEdges, aspectRatio LogicalSize, borderBox bool, blockSize float64) float64 {
	if borderBox {
		return max(bp.InlineSum(), blockSize*aspectRatio.InlineSize/aspectRatio.BlockSize)
	}
	return (blockSize-bp.BlockSum())*aspectRatio.InlineSize/aspectRatio.BlockSize + bp.InlineSum()
}

// BlockSizeFromAspectRatio is the border-box block-size inlineSize (also
// border-box) transfers through aspectRatio. Mirrors Blink's
// BlockSizeFromAspectRatio (length_utils.cc:301-312 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func BlockSizeFromAspectRatio(bp LogicalEdges, aspectRatio LogicalSize, borderBox bool, inlineSize float64) float64 {
	if borderBox {
		return max(bp.BlockSum(), inlineSize*aspectRatio.BlockSize/aspectRatio.InlineSize)
	}
	return (inlineSize-bp.InlineSum())*aspectRatio.BlockSize/aspectRatio.InlineSize + bp.BlockSum()
}

// ComputeTransferredMinMaxInlineSizes transfers the block min/max sizes
// through aspectRatio. Mirrors Blink's ComputeTransferredMinMaxInlineSizes
// (length_utils.cc:695-715 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeTransferredMinMaxInlineSizes(aspectRatio LogicalSize, blockMinMax MinMaxSizes, bp LogicalEdges, borderBox bool) MinMaxSizes {
	transferred := MinMaxSizes{MinContent: 0, MaxContent: replacedLengthLayoutMax}
	if blockMinMax.MinContent > 0 {
		transferred.MinContent = InlineSizeFromAspectRatio(bp, aspectRatio, borderBox, blockMinMax.MinContent)
	}
	if blockMinMax.MaxContent != replacedLengthLayoutMax {
		transferred.MaxContent = InlineSizeFromAspectRatio(bp, aspectRatio, borderBox, blockMinMax.MaxContent)
	}
	transferred.MaxContent = max(transferred.MaxContent, transferred.MinContent)
	return transferred
}

// logicalAspectRatio converts a physical width:height ratio to the logical
// inline:block ratio of wdm.
func logicalAspectRatio(width, height float64, wdm WritingDirectionMode) LogicalSize {
	if wdm.IsVertical() {
		return LogicalSize{InlineSize: height, BlockSize: width}
	}
	return LogicalSize{InlineSize: width, BlockSize: height}
}

// isEmptyAspectRatio is LogicalSize::IsEmpty for an aspect ratio.
func isEmptyAspectRatio(r LogicalSize) bool {
	return r.InlineSize <= 0 || r.BlockSize <= 0
}

// styleAspectRatioIsAutoAndRatio reports whether `aspect-ratio` is
// `auto && <ratio>` (EAspectRatioType::kAutoAndRatio) rather than a bare
// <ratio> (kRatio).
func styleAspectRatioIsAutoAndRatio(style *css.Style) bool {
	v, _ := style.Get("aspect-ratio")
	for _, f := range strings.Fields(v) {
		if f == "auto" {
			return true
		}
	}
	return false
}

// boxSizingForAspectRatio reports whether the aspect-ratio of style
// applies to the border box: only a bare `aspect-ratio: <ratio>` uses
// box-sizing. Mirrors ComputedStyle::BoxSizingForAspectRatio
// (computed_style.h:2623-2628 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func boxSizingForAspectRatio(style *css.Style) bool {
	if !style.GetAspectRatio().IsSet || styleAspectRatioIsAutoAndRatio(style) {
		return false
	}
	return style.GetBoxSizing() == "border-box"
}

// replacedAspectRatio is the logical aspect ratio of a replaced box with
// natural sizing info: a bare `aspect-ratio: <ratio>`, else its natural
// ratio when no size containment applies, else the ratio of
// `auto && <ratio>`. Mirrors Blink's BlockNode::GetReplacedAspectRatio
// (block_node.cc:1329-1354 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func replacedAspectRatio(style *css.Style, info IntrinsicSizingInfo, wdm WritingDirectionMode) LogicalSize {
	ar := style.GetAspectRatio()
	if ar.IsSet && !styleAspectRatioIsAutoAndRatio(style) {
		return logicalAspectRatio(ar.Width, ar.Height, wdm)
	}
	if !style.ShouldApplySizeContainment() && !style.ShouldApplyInlineSizeContainment() &&
		info.HasAspectRatio && info.AspectRatio > 0 {
		return logicalAspectRatio(info.AspectRatio, 1, wdm)
	}
	if ar.IsSet {
		return logicalAspectRatio(ar.Width, ar.Height, wdm)
	}
	return LogicalSize{}
}

// replacedIntrinsicSize is the content-box natural size of a replaced box
// with natural sizing info, each axis reported only when it has one; size
// containment makes a contained axis 0. Mirrors Blink's
// LayoutInputNode::IntrinsicSize and GetOverrideIntrinsicSize
// (layout_input_node.cc:153-183, :236-253 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); contain-intrinsic-size is not
// modelled. louis14's IntrinsicSizingInfo reports an unknown axis as 0.
func replacedIntrinsicSize(style *css.Style, info IntrinsicSizingInfo, wdm WritingDirectionMode) (inline float64, hasInline bool, block float64, hasBlock bool) {
	if style.ShouldApplySizeContainment() {
		return 0, true, 0, true
	}
	w, hasW := info.IntrinsicWidth, info.IntrinsicWidth > 0
	h, hasH := info.IntrinsicHeight, info.IntrinsicHeight > 0
	if wdm.IsVertical() {
		w, hasW, h, hasH = h, hasH, w, hasW
	}
	if style.ShouldApplyInlineSizeContainment() {
		w, hasW = 0, true
	}
	return w, hasW, h, hasH
}

// replacedDefaultNaturalSize is the 300x150 default natural size, logical.
// Mirrors Blink's ComputeDefaultNaturalSize (length_utils.cc:946-951 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); zoom is not modelled.
func replacedDefaultNaturalSize(wdm WritingDirectionMode) LogicalSize {
	return logicalAspectRatio(300, 150, wdm)
}

// computeNormalizedNaturalSize returns the border-box natural size,
// reflecting a single natural size through the aspect ratio and coercing
// the natural block-size to the ratio; ok is false when there is only a
// ratio. Mirrors Blink's ComputeNormalizedNaturalSize (length_utils.cc:
// 964-1008 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func computeNormalizedNaturalSize(style *css.Style, info IntrinsicSizingInfo, wdm WritingDirectionMode, bp LogicalEdges, borderBox bool, aspectRatio LogicalSize) (LogicalSize, bool) {
	inline, hasInline, block, hasBlock := replacedIntrinsicSize(style, info, wdm)
	noRatio := isEmptyAspectRatio(aspectRatio)
	if hasInline {
		inline += bp.InlineSum()
	} else if noRatio {
		inline, hasInline = replacedDefaultNaturalSize(wdm).InlineSize+bp.InlineSum(), true
	}
	if hasBlock {
		block += bp.BlockSum()
	} else if noRatio {
		block, hasBlock = replacedDefaultNaturalSize(wdm).BlockSize+bp.BlockSum(), true
	}
	if !hasInline && hasBlock {
		inline, hasInline = InlineSizeFromAspectRatio(bp, aspectRatio, borderBox, block), true
	}
	if hasInline && (!hasBlock || !noRatio) {
		block, hasBlock = BlockSizeFromAspectRatio(bp, aspectRatio, borderBox, inline), true
	}
	if hasInline && hasBlock {
		return LogicalSize{InlineSize: inline, BlockSize: block}, true
	}
	return LogicalSize{}, false
}

// replacedFragmentGeometry is the border, padding and (absent) scrollbar
// of a replaced box: a replaced box is never a scroll container
// (isScrollContainerBox), so it reserves no scrollbar (ComputeScrollbars,
// length_utils.cc:1543-1560 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func replacedFragmentGeometry(style *css.Style, wdm WritingDirectionMode, space ConstraintSpace) FragmentGeometry {
	geom := ComputeFragmentGeometry(style, wdm, space.PercentageResolutionInlineSize)
	geom.Scrollbar = LogicalEdges{}
	return geom
}

// replacedSizer holds what ComputeReplacedSizeInternal reads whatever its
// mode: the resolver, the aspect ratio and the normalized natural size.
type replacedSizer struct {
	r              replacedLengthResolver
	borderBox      bool
	aspectRatio    LogicalSize
	noRatio        bool
	naturalSize    LogicalSize
	hasNaturalSize bool
}

// ComputeReplacedSize returns the border-box size of a replaced box laid
// out in space. Mirrors Blink's ComputeReplacedSize (length_utils.cc:
// 1232-1275 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b); louis14 lays out
// no <svg> document element through it, so only ComputeReplacedSizeInternal
// is ported. The border and padding are the box's own
// (replacedFragmentGeometry).
func ComputeReplacedSize(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace, mode ReplacedSizeMode) LogicalSize {
	style := node.Style()
	r := newReplacedLengthResolver(style, space)
	info := GetIntrinsicSizingInfo(ctx, node)
	s := replacedSizer{r: r, borderBox: boxSizingForAspectRatio(style)}
	s.aspectRatio = replacedAspectRatio(style, info, r.wdm)
	s.noRatio = isEmptyAspectRatio(s.aspectRatio)
	s.naturalSize, s.hasNaturalSize = computeNormalizedNaturalSize(style, info, r.wdm, r.bp, s.borderBox, s.aspectRatio)
	return s.computeReplacedSizeInternal(mode)
}

// lazyOnce memoizes f: ComputeReplacedSizeInternal's size functions may be
// read by the min, max and main lengths alike.
func lazyOnce(f func() float64) func() float64 {
	var v float64
	done := false
	return func() float64 {
		if !done {
			v, done = f(), true
		}
		return v
	}
}

// computeReplacedSizeInternal mirrors Blink's ComputeReplacedSizeInternal
// (length_utils.cc:1012-1229 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// Quirks-mode min/max block percentages (:1050-1059) are not modelled.
func (s replacedSizer) computeReplacedSizeInternal(mode ReplacedSizeMode) LogicalSize {
	r, bp, space := s.r, s.r.bp, s.r.space

	blockSizeFunc := lazyOnce(func() float64 {
		if s.noRatio {
			return s.naturalSize.BlockSize
		}
		if mode == ReplacedSizeModeNormal {
			return s.computeReplacedSizeInternal(ReplacedSizeModeIgnoreBlockLengths).BlockSize
		}
		if s.hasNaturalSize {
			return s.naturalSize.BlockSize
		}
		return Indefinite
	})

	blockMinMax := MinMaxSizes{MinContent: 0, MaxContent: replacedLengthLayoutMax}
	replacedBlock, hasReplacedBlock := 0.0, false
	if mode != ReplacedSizeModeIgnoreBlockLengths {
		blockMinMax = r.minMaxBlock(blockSizeFunc)
		if space.IsFixedBlockSize && !space.IsFixedBlockSizeIndefinite {
			replacedBlock, hasReplacedBlock = space.AvailableSize.BlockSize.Float64(), true
		} else {
			autoBlock := replacedLengthFitContent
			if space.BlockAutoBehavior != AutoSizeBehaviorFitContent {
				autoBlock = replacedLengthStretch
			}
			if blockSize := r.resolveBlock(r.blockProperty(), replacedLengthTypeMain, autoBlock, blockSizeFunc); blockSize != Indefinite {
				replacedBlock, hasReplacedBlock = blockMinMax.ClampSizeToMinAndMax(blockSize), true
			}
		}
	}

	transferredMinMax := MinMaxSizes{MinContent: 0, MaxContent: replacedLengthLayoutMax}
	if !s.noRatio {
		transferredMinMax = ComputeTransferredMinMaxInlineSizes(s.aspectRatio, blockMinMax, bp, s.borderBox)
	}

	minMaxSizesFunc := lazyOnce(func() float64 {
		switch {
		case s.noRatio:
			return s.naturalSize.InlineSize
		case hasReplacedBlock:
			return InlineSizeFromAspectRatio(bp, s.aspectRatio, s.borderBox, replacedBlock)
		case s.hasNaturalSize:
			if mode == ReplacedSizeModeNormal {
				return s.computeReplacedSizeInternal(ReplacedSizeModeIgnoreInlineLengths).InlineSize
			}
			return s.naturalSize.InlineSize
		}
		return Indefinite
	})

	var inlineMinMax MinMaxSizes
	replacedInline, hasReplacedInline := 0.0, false
	if mode == ReplacedSizeModeIgnoreInlineLengths {
		inlineMinMax = transferredMinMax
	} else {
		inlineMinMax = r.minMaxInline(minMaxSizesFunc)
		if space.IsFixedInlineSize {
			replacedInline, hasReplacedInline = space.AvailableSize.InlineSize.Float64(), true
		} else {
			autoInline := replacedLengthFitContent
			if space.InlineAutoBehavior != AutoSizeBehaviorFitContent {
				autoInline = replacedLengthStretch
			}
			if inlineSize := r.resolveInline(r.inlineProperty(""), replacedLengthTypeMain, autoInline, minMaxSizesFunc); inlineSize != Indefinite {
				replacedInline, hasReplacedInline = inlineMinMax.ClampSizeToMinAndMax(inlineSize), true
			}
		}

		// Transfer the block min/max sizes if the main size did not
		// resolve (css-sizing-4 aspect-ratio size transfers).
		if !hasReplacedInline {
			inlineMinMax.MinContent = max(inlineMinMax.MinContent, min(transferredMinMax.MinContent, inlineMinMax.MaxContent))
			inlineMinMax.MaxContent = min(inlineMinMax.MaxContent, transferredMinMax.MaxContent)
		}
	}

	if hasReplacedInline && hasReplacedBlock {
		return LogicalSize{InlineSize: replacedInline, BlockSize: replacedBlock}
	}

	stretchFit := func() float64 {
		if !isIndefiniteInlineSize(space.AvailableSize.InlineSize.Float64()) {
			return r.stretchInline()
		}
		// All browsers use the default natural size for a percentage.
		if propertyHasPercent(r.style, r.inlineProperty("")) {
			return replacedDefaultNaturalSize(r.wdm).InlineSize + bp.InlineSum()
		}
		return bp.InlineSum()
	}

	// Only an aspect-ratio and no sizes: stretch.
	if !s.hasNaturalSize && !hasReplacedInline && !hasReplacedBlock {
		replacedInline, hasReplacedInline = inlineMinMax.ClampSizeToMinAndMax(stretchFit()), true
	}

	if hasReplacedInline {
		if s.noRatio {
			replacedBlock = s.naturalSize.BlockSize
		} else {
			replacedBlock = BlockSizeFromAspectRatio(bp, s.aspectRatio, s.borderBox, replacedInline)
		}
		return LogicalSize{InlineSize: replacedInline, BlockSize: blockMinMax.ClampSizeToMinAndMax(replacedBlock)}
	}

	if hasReplacedBlock {
		if s.noRatio {
			replacedInline = s.naturalSize.InlineSize
		} else {
			replacedInline = InlineSizeFromAspectRatio(bp, s.aspectRatio, s.borderBox, replacedBlock)
		}
		return LogicalSize{InlineSize: inlineMinMax.ClampSizeToMinAndMax(replacedInline), BlockSize: replacedBlock}
	}

	return LogicalSize{
		InlineSize: inlineMinMax.ClampSizeToMinAndMax(s.naturalSize.InlineSize),
		BlockSize:  blockMinMax.ClampSizeToMinAndMax(s.naturalSize.BlockSize),
	}
}

// ComputeMinAndMaxContentContributionForReplaced is the content-box
// min/max-content contribution of a replaced box: its replaced size, the
// min-content contribution of a percentage (max-)inline-size being its
// min-inline-size. An intrinsic pass has an indefinite available and
// percentage-resolution inline-size, and sizes an auto inline-size
// fit-content (a block container never stretches a replaced child); the
// space is made so here because louis14's intrinsic spaces do not carry
// either. Mirrors Blink's ComputeMinAndMaxContentContributionForReplaced
// (length_utils.cc:314-355 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func ComputeMinAndMaxContentContributionForReplaced(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) MinMaxSizes {
	space.AvailableSize.InlineSize = layoutunit.IndefiniteSize
	space.PercentageResolutionSize.InlineSize = layoutunit.IndefiniteSize
	space.InlineAutoBehavior = AutoSizeBehaviorFitContent
	style := node.Style()
	r := newReplacedLengthResolver(style, space)
	size := ComputeReplacedSize(ctx, node, space, ReplacedSizeModeNormal).InlineSize
	result := MinMaxSizes{MinContent: size, MaxContent: size}
	if propertyHasPercent(style, r.inlineProperty("")) || propertyHasPercent(style, r.inlineProperty("max-")) {
		result.MinContent = r.resolveInline(r.inlineProperty("min-"), replacedLengthTypeMin, replacedLengthAuto,
			func() float64 { return r.bp.InlineSum() })
		if result.MinContent == Indefinite {
			result.MinContent = r.bp.InlineSum()
		}
	}
	return MinMaxSizes{
		MinContent: max(0, result.MinContent-r.bp.InlineSum()),
		MaxContent: max(0, result.MaxContent-r.bp.InlineSum()),
	}
}
