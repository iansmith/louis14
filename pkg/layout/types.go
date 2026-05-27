package layout

import (
	"strconv"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// Box represents a CSS box in the fragment tree.
// This is the output of layout — an immutable positioned fragment
// with physical coordinates for painting.
type Box struct {
	Node   *html.Node
	Style  *css.Style
	X, Y   float64
	Width  float64
	Height float64

	Margin  css.BoxEdge
	Padding css.BoxEdge
	Border  css.BoxEdge

	Children []*Box
	Parent   *Box

	Position css.PositionType
	ZIndex   int

	// Content for replaced elements and pseudo-elements.
	ImagePath     string
	PseudoContent string

	// MarkerStyle is the computed ::marker pseudo-element style, if any.
	// Set during layout tree building for display:list-item elements.
	MarkerStyle   *css.Style
	MarkerContent string // Resolved ::marker content text (may include counter values)

	// Text holds the rendered text content for inline text boxes.
	Text string

	// IsVerticalText is true when text should be rendered vertically
	// (writing-mode is vertical-rl, vertical-lr, sideways-rl, or sideways-lr).
	IsVerticalText bool

	// IsSidewaysLR is true for sideways-lr writing mode, where inline direction
	// is bottom-to-top (characters drawn from bottom to top within the fragment).
	IsSidewaysLR bool

	// IsSidewaysRL is true for sideways-rl writing mode, where the entire text
	// line is rotated 90° CW (like horizontal text read by tilting head right).
	IsSidewaysRL bool

	// IsWritingModeVerticalLR is true when the CSS writing-mode property is
	// vertical-lr AND text-orientation is mixed (so IsSidewaysRL is also set).
	// This flag distinguishes vertical-lr from vertical-rl in the sideways
	// rotation path, where both write modes set IsSidewaysRL=true for mixed
	// text-orientation. Decoration painters use this to place the underline on
	// the correct physical side (right for vertical-lr, left for vertical-rl).
	IsWritingModeVerticalLR bool

	// Inline fragment tracking.
	IsFirstFragment bool
	IsLastFragment  bool

	// Line boxes for inline formatting contexts.
	LineBoxes []*LineBox

	// LayoutNode is the LayoutInputNode that produced this box.
	// Nil for anonymous boxes, text fragments, and line boxes.
	// Provides the Box→LayoutInputNode direction of the bidirectional link
	// used by BuildPaintTree to walk children in DOM tree order.
	LayoutNode *LayoutInputNode

	// DOMIndex is a pre-order index in the DOM tree, used to ensure correct
	// paint ordering when out-of-flow children propagate to ancestor boxes.
	DOMIndex int

	// ClipContentToBorderBox forces the paint layer to clip children to
	// this box's border-box regardless of the computed `overflow` style.
	// Carried from the producing PhysicalFragment (see layout_result.go).
	// Used by CSS Tables 3 §5.4.1 rowspan-over-collapsed-row clipping.
	ClipContentToBorderBox bool

	// RenderedColumnCount is the number of column fragments actually placed
	// inside a multicol container. Used at paint time so column-rule painting
	// honors the spec rule that rules are only drawn between columns that both
	// have content (CSS Multicol L1 §5: column-rule). Zero for non-multicol.
	RenderedColumnCount int

	// GapGeometry carries the column-rule / row-rule geometry for multicol
	// containers, forwarded from PhysicalFragment.GapGeometry at paint time.
	// Nil for non-multicol fragments or when no gap rules apply.
	GapGeometry *GapGeometry

	// IsColumnBox marks this Box as a multicol column fragmentainer
	// (forwarded from PhysicalFragment.BoxType == BoxTypeColumn).
	// Mirrors Blink PhysicalFragment::IsColumnBox. Read by the
	// column-rule painter (P20.4) to derive rule extents from the
	// actual placed column fragments rather than from the multicol
	// container's full content area, and reserved for future paint-
	// time fragmentainer behaviour. Default false on all other boxes.
	IsColumnBox bool

	// IsMulticolContainer marks this Box as a multicol container
	// (forwarded from PhysicalFragment.IsMulticolContainer). Mirrors
	// the structural condition that makes Blink's
	// LayoutBox::HasNonVisibleOverflow() return true for multicol —
	// the fragmentation context establishes an overflow clip even
	// with computed `overflow: visible`. Consumed by the paint layer
	// (P20.5) to apply a padding-box overflow clip mirroring Blink's
	// default OverflowClipRect for non-scroll containers.
	IsMulticolContainer bool

	// AppliedTextDecorations is an optional per-fragment override of the
	// style-carried decoration vector (LOU-149 Phase 4). Set only for text
	// fragments that participate in a multi-fragment decorating box; the
	// paint layer reads this in preference to Style.GetAppliedTextDecorations
	// when non-nil. Forwarded from PhysicalFragment.AppliedTextDecorations.
	AppliedTextDecorations []css.AppliedTextDecoration

	// CollapsedBorderOutwardExtension carries the per-physical-side width
	// (px) of the collapsed-border "outside half" that the live cell must
	// paint as an additive outward strip — when its neighbor on that side
	// is missing from the grid (cell removed, table outer edge with no
	// element border to share). Indexed [top, right, bottom, left].
	//
	// Spec: CSS 2.1 §17.6.3 / CSS Tables 3 §4.2. Blink reference:
	// TablePainter::PaintCollapsedBorders @ table_painters.cc:356-362
	// (Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Forwarded
	// from PhysicalFragment.CollapsedBorderOutwardExtension; consumed by
	// paint_layer.go and pkg/render/render.go's
	// paintCollapsedTableCellBorder.
	//
	// Zero on all sides for cells whose every neighbor exists (the common
	// case) and for border-collapse:separate cells.
	CollapsedBorderOutwardExtension [4]float64
}

// CreatesStackingContext returns true if this box establishes a new
// stacking context per CSS 2.1 Appendix E / CSS Compositing §2.
//
// Stacking contexts are created by:
// - Positioned elements with an explicit integer z-index (not auto)
// - Elements with opacity < 1
func (b *Box) CreatesStackingContext() bool {
	if b.Style == nil {
		return false
	}
	// Positioned + explicit z-index.
	if b.Position != css.PositionStatic && b.Style.HasExplicitZIndex() {
		return true
	}
	// CSS Flexbox §4.3: flex items with explicit z-index create stacking contexts
	// even when position is static.
	if b.Style.HasExplicitZIndex() && b.Parent != nil && b.Parent.Style != nil {
		parentDisplay := b.Parent.Style.GetDisplay()
		if parentDisplay == css.DisplayFlex || parentDisplay == css.DisplayInlineFlex {
			return true
		}
	}
	// opacity < 1.
	if opacity, ok := b.Style.Get("opacity"); ok {
		if o, err := strconv.ParseFloat(opacity, 64); err == nil && o < 1.0 {
			return true
		}
	}
	// CSS Transforms: elements with a transform create a stacking context.
	// Per CSS Transforms Level 2 §3, individual transform properties
	// (translate, rotate, scale) also create a stacking context.
	//
	// Gated on IsTransformableBox per CSS Transforms Level 1 §3
	// ("transformable element"): non-replaced inline-level boxes
	// (`display: inline | ruby | ruby-text`) silently no-op on transform
	// at paint time, so they MUST NOT create a stacking context either
	// — otherwise the element gets spuriously hoisted into a z-list and
	// reorders paint even though its transform is ignored.
	//
	// Mirrors the `!object.IsBox()` short-circuit of Blink's
	// NeedsTransform at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f —
	// paint_property_tree_builder.cc:1310 (function spans :1299-:1319).
	// Non-atomic inline LayoutObjects (LayoutInline) are not boxes;
	// transforms never reach paint-property-tree creation for them.
	// (NeedsTransform has other branches — backface-visibility:hidden,
	// transform animations, preserve-3d — that louis14's stacking-
	// context decision doesn't yet model; separate gap.) An earlier
	// version of louis14 cited computed_style.cc:1319
	// HasPropertyThatCreatesStackingContext as the Blink gate; that was
	// incorrect at the pinned SHA — that function returns true for
	// kTransform/kTranslate/kRotate/kScale unconditionally and is not a
	// transformability check.
	if IsTransformableBox(b.Style, b.Node) {
		if transforms := b.Style.GetTransforms(); len(transforms) > 0 {
			return true
		}
		if _, _, _, _, ok := b.Style.GetIndividualTranslate(); ok {
			return true
		}
		if _, ok := b.Style.GetIndividualRotate(); ok {
			return true
		}
		if _, _, ok := b.Style.GetIndividualScale(); ok {
			return true
		}
	}
	// CSS Filters: elements with a filter create a stacking context.
	if filters := b.Style.GetFilter(); len(filters) > 0 {
		return true
	}
	// CSS backdrop-filter: elements with a backdrop-filter create a stacking context.
	if filters := b.Style.GetBackdropFilter(); len(filters) > 0 {
		return true
	}
	// CSS mix-blend-mode: elements with non-normal blend mode create a stacking context.
	if bm := b.Style.GetMixBlendMode(); bm != css.MixBlendModeNormal && bm != "" {
		return true
	}
	// CSS Masking Level 1 §3.1: a computed clip-path other than 'none'
	// creates a stacking context (and, in Blink, a PaintLayer).
	if b.Style.GetClipPath() != nil {
		return true
	}
	// CSS Masking Level 1 §6.1: a computed mask/mask-image other than 'none'
	// creates a stacking context.
	if mi := b.Style.GetMaskImage(); mi != "" && mi != "none" {
		return true
	}
	// CSS Containment: layout and paint containment create a stacking context.
	if b.Style.HasLayoutContainment() || b.Style.HasPaintContainment() {
		return true
	}
	// CSS Compositing Level 1 §2: isolation: isolate creates a stacking context.
	if b.Style.GetIsolation() == "isolate" {
		return true
	}
	// CSS Will Change Level 1 §2.2: will-change of certain properties creates
	// a stacking context (same properties that create one when actually set).
	for _, prop := range b.Style.GetWillChange() {
		switch prop {
		case "transform", "opacity", "filter", "backdrop-filter",
			"clip-path", "mask", "mix-blend-mode", "isolation",
			"perspective", "offset-path":
			return true
		}
	}
	// CSS Flexbox §4.3: A flex item with an explicit z-index creates a
	// stacking context even if position is static.
	if b.IsFlexItem() && b.Style.HasExplicitZIndex() {
		return true
	}
	return false
}

// IsFlexItem returns true if this box is a child of a flex container.
func (b *Box) IsFlexItem() bool {
	if b.Parent == nil || b.Parent.Style == nil {
		return false
	}
	d := b.Parent.Style.GetDisplay()
	return d == css.DisplayFlex || d == css.DisplayInlineFlex
}

// LineBox represents a line in an inline formatting context.
type LineBox struct {
	Y      float64
	Height float64
	Boxes  []*Box
}
