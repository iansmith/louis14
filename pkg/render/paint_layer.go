package render

import (
	"sort"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
	"mazarin/textshape"
)

// PaintPhase selects the subset of painting work done in one pass over
// a stacking-context subtree. Mirrors Blink's PaintPhase enum
// (kDescendantBlockBackgroundsOnly / kFloat / kForeground).
//
// CSS 2.1 Appendix E splits non-positioned descendant painting into
// three ordered passes: block backgrounds (step 3), floats (step 4),
// inline foreground (step 5). A single DOM-order tree walk cannot
// express this ordering because inline foreground must paint AFTER
// floats even though the inline is structurally inside a sibling.
type PaintPhase int

const (
	// PhaseBackground paints backgrounds, borders, and list markers of
	// non-self-painting descendants (Appendix E step 3).
	PhaseBackground PaintPhase = iota
	// PhaseFloat recurses into non-self-painting descendants looking
	// for floats, each of which is painted with its full phase loop
	// (Appendix E step 4).
	PhaseFloat
	// PhaseForeground paints text, images, and replaced content of
	// non-self-painting descendants (Appendix E step 5).
	PhaseForeground
	// PhaseOutline paints outlines of non-self-painting descendants
	// (Appendix E step 10): outlines paint after all in-flow content of
	// the stacking context so they overlap text and backgrounds they
	// cover (e.g. negative outline-offset). Mirrors Blink's
	// PaintPhase::kSelfOutlineOnly / kDescendantOutlinesOnly
	// (paint_phase.h @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	PhaseOutline
)

// PaintLayer represents a node in the pre-paint tree.
// Built from the layout Box tree before painting, PaintLayers pre-sort
// children by CSS 2.1 Appendix E stacking order and pre-compute
// all paint-relevant properties so draw methods never access Style.
//
// Boxes without Style (line boxes, text runs) do NOT get PaintLayers.
// They remain reachable via their parent PaintLayer's Box.Children
// and are painted inline during the parent's paint.
//
// Mirrors Blink's PaintLayer / PrePaintTreeWalk.
type PaintLayer struct {
	Box      *layout.Box
	Position css.PositionType // Always set (PositionStatic for unstyled)
	ZIndex   int

	// Stacking children (only populated for stacking context roots):
	NegativeZ []*PaintLayer // z < 0, sorted ascending
	AutoZero  []*PaintLayer // z-index:auto positioned + z-index:0 SCs, tree order
	PositiveZ []*PaintLayer // z > 0, sorted ascending

	// Non-positioned children in DOM order (Appendix E steps 3-5):
	FlowChildren []*PaintLayer

	// Float children (Appendix E step 4: floats paint after non-float blocks).
	// Floats are separated from FlowChildren so they paint above block backgrounds.
	FloatChildren []*PaintLayer

	// Overflow clip (pre-computed from Style):
	HasClip  bool
	ClipX    bool       // true if X axis is clipped (overflow-x != visible)
	ClipY    bool       // true if Y axis is clipped (overflow-y != visible)
	ClipRect [4]float64 // x, y, w, h of padding box

	// AncestorOverflowClips: pre-computed chain of ancestor overflow clip
	// rectangles (in absolute coordinates) that should be applied before
	// painting this layer's content. Populated only on layers that escape
	// their parent's overflow clip via z-list escalation (positioned
	// stacking contexts whose nearest ancestor SC is above the overflow
	// clip ancestor). For each entry the renderer must Push a clip rect
	// in addition to (and BEFORE) the layer's own HasClip rect.
	//
	// Blink models clips orthogonally to stacking via the paint property
	// tree, so any descendant inherits the chain of ancestor clip nodes
	// regardless of where it paints in z-order. Louis14's paint model
	// brackets each layer's clip via Push/Pop in paintLayer, so we need
	// to carry the chain explicitly on layers that escape.
	//
	// Each entry is [x, y, w, h] in absolute coordinates of the ancestor's
	// overflow-clip rectangle (padding box by default). Order is innermost
	// → outermost (the renderer Pushes in this order and Pops in reverse).
	AncestorOverflowClips [][4]float64

	// Overflow-clip-margin geometry (CSS Overflow 3 §3.2).
	//
	// The clip rect is the chosen visual-box (border-box,
	// padding-box, or content-box — `ClipMarginVisualBoxInset` is its
	// per-side inset from the border edge, always ≥ 0, in CSS px)
	// outward-expanded by `ClipMarginLength` on every side. The clip
	// radii are computed lazily by overflowClipRadii, mirroring
	// Blink's `AdjustRoundedClipForOverflowClipMargin`
	// (core/paint/paint_property_tree_builder.cc:2869 @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): padding-box radii
	// expanded by the combined reference-box delta + margin using the
	// same coverage-factor corner correction as box-shadow spread.
	//
	// Zero/false when overflow-clip-margin is not applied.
	HasClipMargin            bool
	ClipMarginVisualBoxInset [4]float64
	ClipMarginLength         float64

	// CSS clip: rect() (purely physical, per CSS Writing Modes §7.6):
	HasCSSClip  bool
	CSSClipRect [4]float64 // x, y, w, h of clip region

	// Pre-computed paint properties — no Style access needed during paint.

	// Compositing:
	Visible bool    // false = visibility:hidden, skip subtree
	Opacity float64 // 0.0..1.0; 1.0 = fully opaque

	// Image (for <img> replaced elements):
	ImageSrc       string        // src attribute value; empty if not an img element
	ObjectFit      css.ObjectFit // fill, contain, cover, none, scale-down
	ObjectPosition [2]float64    // x%, y% in range [0,1]; default (0.5, 0.5)
	ImageRendering string        // auto, pixelated, crisp-edges, -webkit-optimize-contrast

	// Background:
	BackgroundColor  css.Color              // A==0 means no background
	BackgroundClip   css.BackgroundClipType // clip for background-color (always set)
	BackgroundLayers *css.FillLayer         // linked list of layers, head = topmost CSS layer

	// Borders: indices 0=Top, 1=Right, 2=Bottom, 3=Left
	BorderColors [4]css.Color
	BorderStyles [4]css.BorderStyle
	BorderRadius css.EllipticalRadii // TopLeft, TopRight, BottomRight, BottomLeft (elliptical)

	// Border image (9-slice): replaces regular border drawing when source is set.
	BorderImageSource    *css.CSSImageValue   // url() value; nil = none (LOU-138 phase 7.3)
	BorderImageSlice     css.BorderImageSlice // 4 slice values + fill flag
	BorderImageWidth     [4]float64           // top, right, bottom, left (px)
	BorderImageWidthAuto [4]bool              // true for sides where 'auto' was specified (resolved at paint time per CSS Backgrounds 3 §6.3)
	BorderImageRepeat    [2]string            // [horizontal, vertical]: stretch/repeat/round/space
	BorderImageOutset    [4]float64           // top, right, bottom, left (px); extends paint area outside border box

	// Box shadows (outset and inset):
	BoxShadows []css.BoxShadow

	// Outline (doesn't affect layout, drawn outside border-box):
	OutlineStyle  string // none, solid, dashed, dotted, double
	OutlineWidth  float64
	OutlineColor  css.Color
	OutlineOffset float64
	// OutlineBox overrides the Box geometry used by drawOutline when non-zero.
	// Set by buildPaintSubtree for the first fragment of a multi-line inline
	// element: the union bounding box of all line fragments for the same
	// html.Node, so the outline ring encloses the full inline element.
	// Subsequent fragments of the same element have their outline suppressed
	// (OutlineStyle set to "none") so only the first fragment draws the ring.
	// Mirrors Blink's OutlinePainter collecting all line boxes for a
	// LayoutInline before calling the outline painter.
	OutlineBox [4]float64 // [x, y, w, h]; used only when OutlineBox[2] > 0

	// Text:
	TextColor  css.Color
	FontSize   float64
	FontBold   bool
	FontItalic bool
	FontMono   bool
	FontAhem   bool
	FontFamily string
	// font-synthesis-* gates (CSS Fonts 4 §6.6). When `Weight` is false the
	// font fallback path must not pick a bold variant of a different
	// physical family to "synthesize" weight. `Style` carries the
	// three-valued css.FontSynthesisStyleValue enum (auto / none /
	// oblique-only) — none forbids cross-family italic fallback, oblique-only
	// forbids italic↔oblique substitution at the matcher (and still allows
	// skew on a true oblique face). See
	// text.FontConfig.FontPathForFamilyWithSynthesis. FontSynthesisSmallCaps
	// gates the synthesize-caps fallback in render.drawTextSmallCaps; native
	// OpenType `smcp`/`c2sc` features are emitted unconditionally below in
	// the font-feature-settings block.
	FontSynthesisWeight     bool
	FontSynthesisStyle      css.FontSynthesisStyleValue
	FontSynthesisSmallCaps  bool
	LetterSpacing           float64
	WordSpacing             float64
	TabSize                 float64 // tab-size value (character count or px)
	TabSizeIsLength         bool    // true = px length, false = character count
	IsVerticalText          bool
	IsSidewaysLR            bool
	IsSidewaysRL            bool
	IsWritingModeVerticalLR bool // vertical-lr + mixed text-orientation (IsSidewaysRL also true)

	// IsUprightVertical is true when the writing mode is vertical-rl or vertical-lr
	// AND text-orientation is not "sideways". This is the louis14 analogue of Blink's
	// `GetFontBaseline() == kCentralBaseline` check used by ResolveUnderlinePosition()
	// at third_party/blink/renderer/core/paint/text_decoration_info.cc:13-39 @ SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	// Per CSS Text Decor 3 §3.5, `text-underline-position: left | right` keywords
	// apply ONLY in upright/mixed vertical writing modes (central-baseline). In
	// sideways writing modes (horizontal typographic mode after rotation), `left`
	// and `right` MUST be ignored and the rotation-direction default applies.
	//
	// Note: louis14 collapses vertical-{rl,lr}+mixed into IsSidewaysRL=true at
	// engine.go:374, so this flag is what distinguishes those cases from true
	// sideways writing modes (writing-mode: sideways-*) and from vertical modes
	// with explicit text-orientation: sideways.
	IsUprightVertical bool

	// Text decoration (underline, overline, line-through):
	TextDecoration          css.TextDecoration
	TextDecorationColor     css.Color // defaults to TextColor (currentColor)
	TextDecorationThickness float64   // defaults to ~1px
	TextDecorationStyle     string    // solid, double, dotted, dashed, wavy
	TextUnderlineOffset     float64   // additional offset for underline (px); 0 = auto/default

	// TextUnderlinePosition is the raw CSS text-underline-position value
	// (auto / under / left / right / from-font, space-separated combinations
	// like "from-font right"). Used by the vertical text-decoration painters
	// to flip the under-side between physical LEFT and RIGHT independently
	// of the writing-mode's natural under-direction. Per CSS Text Decor 4 §3.3,
	// `left` / `right` override the writing-mode-derived under-side; `auto`
	// follows the rotated baseline's under direction (LEFT after CW rotation,
	// RIGHT after CCW). louis14 keeps the raw string here because the parser
	// doesn't yet type the property; consumers do substring-contains checks
	// for "left"/"right"/"under".
	TextUnderlinePosition string

	// TextLangIsCJK is set when this layer's inherited HTML `lang` attribute
	// resolves to a CJK locale (zh, ja, ko, mn). Per CSS Text Decor 3 §3.5
	// CJK convention flips the default underline direction in vertical
	// writing modes — the "auto" underline default moves to the over-side
	// and `flip_underline_and_overline_` swaps the bits so a `text-decoration:
	// underline` paints on the over-side and `text-decoration: overline`
	// paints on the under-side. Computed from html.Node.InheritedLanguage()
	// at PaintLayer construction (DOM tree inheritance, not CSS cascade —
	// `lang` is NOT a cascaded inherited property in louis14). Mirrors how
	// Blink's `ResolveUnderlinePosition` consults ComputedStyle::Locale()
	// derived from Element::ComputeInheritedLanguage() at
	// third_party/blink/renderer/core/paint/text_decoration_info.cc:23-52 @
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	TextLangIsCJK bool

	// AppliedTextDecorations is the accumulated CSS Text Decor 3 vector for
	// this element (mirrors Blink's `AppliedTextDecorationVector` on
	// ComputedStyle at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Each
	// entry was contributed by an ancestor that established a decoration.
	// Phase 1: populated directly from Style; Phase 4 will refine to use
	// per-decorating-box origins. Painters iterate this vector; when non-nil
	// it supersedes the legacy single-decoration fields above.
	AppliedTextDecorations []css.AppliedTextDecoration

	// TextDecorationSkipInk is the resolved text-decoration-skip-ink property
	// (CSS Text Decor 4 §1.1.5). When auto (the default), the decoration line
	// is interrupted where it would otherwise cross over a glyph's extent.
	// Lives on PaintLayer so the sideways (Strategy A) and upright (Strategy B)
	// paint paths can read it without depending on `box.Style` — the sideways
	// path paints into an off-screen buffer with a synthetic virtual box that
	// has no Style attached.
	TextDecorationSkipInk css.TextDecorationSkipInk

	// Text shadows:
	TextShadows []css.TextShadow

	// CSS font-variant-caps (small-caps, all-small-caps, etc.):
	FontVariantCaps string

	// CSS font-feature-settings: OpenType feature tags parsed into tag/value pairs.
	// Populated from CSS like "kern" 1, "liga" 0. Empty when "normal".
	FontFeatures []textshape.FontFeature

	// Text emphasis marks (small marks above/below each character):
	TextEmphasisMark  string    // resolved mark character ("●", "•", etc.); "" = none
	TextEmphasisColor css.Color // defaults to currentColor
	TextEmphasisOver  bool      // true = over (above), false = under (below)

	// CSS text-transform (uppercase, lowercase, capitalize):
	TextTransform css.TextTransform

	// CSS text-overflow (CSS UI 4 §6.2): clip, ellipsis, or a custom
	// <string>. When TextOverflow == css.TextOverflowString the
	// replacement string is in TextOverflowString.
	TextOverflow       css.TextOverflowType
	TextOverflowString string

	// List markers are real laid-out ::marker boxes in the fragment tree
	// (marker-foundation Phases 3-4) — they paint as ordinary box fragments
	// through the normal box/inline paint paths. The PaintLayer no longer
	// carries marker-paint shortcut fields; the paint-time drawListMarker hack
	// has been retired.

	// CSS Transforms:
	Transforms       []css.Transform
	TransformOrigin  [2]float64 // resolved to px: (origin-x, origin-y)
	TransformOriginZ float64    // resolved z-component of transform-origin (default 0)
	HasTransform     bool

	// HasTransformPaint is true when this layer participates in transform
	// paint semantics — either it has an actual `transform` (HasTransform)
	// or it has any `will-change: transform`-class property. Per CSS
	// Transforms 1 §6, both make `background-attachment: fixed` on
	// descendants degrade to `scroll`. Mirrors Blink's
	// HasWillChangeAnyTransformProperty() ∪ HasTransform() check in
	// background_image_geometry.cc @ 4883d11fef.
	HasTransformPaint bool

	// BackfaceHidden is true when the element's computed back-face is
	// currently facing the viewer AND `backface-visibility: hidden` is set.
	// Per CSS Transforms L2 §11 the entire layer (and its subtree) is then
	// skipped at paint time. Computed from the element's own transform
	// composed with any preserve-3d ancestor transforms above the nearest
	// 3D rendering context boundary. Mirrors Blink's
	// PaintLayerPainter::ShouldPaintForBackfaceHidden() at SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	BackfaceHidden bool

	// CSS Filters:
	Filters   []css.FilterFunction
	HasFilter bool

	// CSS Backdrop Filters:
	BackdropFilters   []css.FilterFunction
	HasBackdropFilter bool

	// CSS clip-path:
	ClipPath *css.ClipPath // nil = no clip-path

	// CSS mask-image:
	MaskImage    string // "none", url(...), or gradient value
	HasMaskImage bool

	// CSS mix-blend-mode:
	BlendMode css.MixBlendMode

	// HasBlendingDescendant is true when this stacking-context layer
	// contains a descendant with `mix-blend-mode != normal` somewhere in
	// its subtree (bounded by inner stacking contexts that themselves
	// carry the flag — the flag propagates UP only to the nearest
	// ancestor stacking context). Per CSS Compositing 1 §8, the parent
	// group of a blended element is treated as an isolated group: the
	// renderer paints this layer into an offscreen buffer initialised to
	// transparent black so the descendant's blend can see the parent's
	// painted area as backdrop without leaking the ancestor canvas
	// underneath. Mirrors Blink's PaintLayer::HasNonIsolatedDescendant
	// WithBlendMode tracking at paint_layer.cc @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	HasBlendingDescendant bool

	// IsBackdropRoot is true when this layer's style forms a Backdrop Root
	// per CSS Filter Effects 2 §3.5 (opacity < 1, filter, mask, mask-image,
	// mask-border, clip-path, backdrop-filter, mix-blend-mode, or
	// will-change of any of the above). Backdrop Roots BOUND the backdrop
	// sample that a descendant's `backdrop-filter` consumes — the captured
	// region of the canvas only includes content painted within this layer
	// (and descendants painted before the backdrop-filter element), not
	// content painted further up the ancestor chain.
	//
	// Set by buildPaintSubtree from `style.IsBackdropRoot()`. Notably
	// transform/perspective/z-index DO NOT trigger backdrop-root, even
	// though they create a stacking context — the spec intentionally
	// separates the two predicates.
	//
	// Mirrors Blink's `effect_paint_property_node.cc::DetermineBackdropRoot`
	// at 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	IsBackdropRoot bool

	// HasBackdropFilterDescendant is true when this layer is a Backdrop
	// Root that contains a descendant (within the backdrop-root boundary)
	// with `backdrop-filter != none`. When set, paintLayer routes the layer
	// through paintLayerIsolated so the descendant's backdrop-filter samples
	// the isolated buffer instead of the canvas underneath the backdrop-
	// root. The flag is only meaningful in combination with IsBackdropRoot.
	HasBackdropFilterDescendant bool

	// PaintsCanvasBackground is true for the root element (or body when
	// background propagates). Per CSS 2.1 §14.2, the root element's background
	// paints the entire canvas, not just its own box.
	PaintsCanvasBackground bool

	// CanvasBackgroundRootBox is set on a promoted body layer to store the
	// root element's box. Per CSS Backgrounds Level 3 §2.11.2, when the
	// body's background is promoted to the canvas, the positioning area is
	// the root element's padding box, not the body's.
	CanvasBackgroundRootBox *layout.Box

	// empty-cells: hide — skip background/border painting for empty table cells
	// (CSS 2.1 §17.6.1.1, only in separate border model).
	EmptyCellHide bool

	// IsCollapsedBorderCell marks a table cell whose table is in
	// border-collapse: collapse mode. Per CSS Tables 3 / CSS 2.1 Appendix E,
	// collapsed table borders paint in a dedicated phase AFTER cell
	// foreground content, so descendants (e.g. a positioned background)
	// cannot overpaint the border. Mirrors Blink's PaintPhase::kTable
	// CollapsedBorders dispatch in TablePainter (table_painters.cc,
	// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	IsCollapsedBorderCell bool

	// CollapsedBorderOutwardExtension carries the per-physical-side width
	// (px) of the collapsed-border "outside half" that this cell must
	// paint as an additive outward strip — its neighbor on that side is
	// missing from the grid (cell removed, or table outer edge with no
	// element border to share), so the live cell carries the part of the
	// collapsed border that would otherwise sit on the missing neighbor's
	// painted half.
	//
	// Indexed [top, right, bottom, left] (matches drawBorders index order).
	// Forwarded from layout.Box.CollapsedBorderOutwardExtension. Consumed
	// by paintCollapsedTableCellBorder in pkg/render/render.go.
	//
	// Spec: CSS 2.1 §17.6.3 / CSS Tables 3 §4.2. Blink reference:
	// TablePainter::PaintCollapsedBorders @ table_painters.cc:356-362
	// (Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	CollapsedBorderOutwardExtension [4]float64

	// Column rules (for multicol containers):
	IsMulticol      bool
	ColumnCount     int     // number of columns actually rendered
	ColumnWidth     float64 // used column width
	ColumnGap       float64 // gap between columns
	ColumnRuleWidth float64 // rule width in px
	ColumnRuleStyle string  // none, solid, dashed, dotted, etc.
	ColumnRuleColor css.Color

	// GapGeometry carries column-rule geometry for multicol containers.
	// Nil when no column rules are active or for non-multicol layers.
	GapGeometry *layout.GapGeometry
}

// BuildPaintTree constructs a PaintLayer tree from a layout Box tree.
// The root box is always treated as a stacking context root
// (CSS 2.1 Appendix E: root element establishes the initial stacking context).
//
// The implicit root Backdrop Root per CSS Filter Effects 2 §3.5 is left as
// a NIL currentBackdropRoot: when a top-level descendant has backdrop-filter,
// the nearest ancestor backdrop-root search returns nil, which the renderer
// reads as "sample the canvas directly" — exactly the implicit-root
// semantics. Routing the root layer itself through paintLayerIsolated would
// pre-fill the buffer with transparent black instead of the canvas
// background, breaking descendants whose backdrop-filter expects to invert
// the white canvas. Mirrors Blink's effect tree, where the root's
// kBackdropRoot flag is implicit and not realised as an isolation group.
func BuildPaintTree(root *layout.Box) *PaintLayer {
	if root == nil {
		return nil
	}
	rootLayer := newPaintLayer(root)
	buildPaintSubtree(root, rootLayer, rootLayer, nil, map[*layout.InlineItem]*PaintLayer{})
	// Re-home z-ordered layers to their nearest stacking-context-creating
	// DOM ancestor. This corrects out-of-flow children hoisted past their
	// DOM-ancestor SC by the layout containing-block rule. Mirrors Blink's
	// PaintLayerStackingNode::AncestorStackingContext semantics.
	reparentZOrderByAncestorSC(rootLayer)
	rootLayer.sortZLists()
	// CSS Transforms L2 §11: propagate backface-hidden down the DOM tree so
	// out-of-flow descendants (which the layout engine hoists into a higher
	// containing block's z-list) still inherit the back-face skip. Without
	// this pass, a position:fixed descendant of a back-face-hidden subtree
	// would be promoted into an ancestor SC's z-list and painted even though
	// its hosting subtree is back-facing. Mirrors Blink's PaintLayerPainter
	// behaviour where the entire subtree paint is short-circuited.
	propagateBackfaceHidden(rootLayer)
	// Consolidate outline drawing for multi-line inline elements. A split
	// inline span generates one PaintLayer per line fragment; each fragment
	// independently calls drawOutline using only its own fragment box, leaving
	// gaps in the outline ring between lines. This pass:
	//   1. Finds groups of FlowChild PaintLayers that share the same html.Node
	//      and have a visible outline.
	//   2. Computes the union bounding box of all those fragments.
	//   3. Sets OutlineBox on the first fragment (so drawOutline draws around
	//      the full element) and suppresses outline on the rest.
	// Mirrors Blink's OutlinePainter::PaintOutline, which iterates all line
	// boxes for a LayoutInline before drawing the outline ring
	// (outline_painter.cc @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
	consolidateInlineOutlines(rootLayer)
	// Focus-ring (outline-style: auto) outlines enclose the ink overflow of
	// the element's in-flow content, not just its border box. Mirrors Blink
	// adding block ink overflow to outline rects only for focus rings
	// (OutlineType::kIncludeBlockInkOverflow, LayoutBox::AddOutlineRects @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f); WPT
	// css-ui/outline-with-padding-001 is the contract (a block whose inline
	// child overflows the padding box gets the auto ring around the union).
	expandAutoOutlinesToInkOverflow(rootLayer)
	return rootLayer
}

// expandAutoOutlinesToInkOverflow widens OutlineBox on outline-style:auto
// layers to the union of the element's box (or existing OutlineBox) and the
// border boxes of all in-flow descendant fragments. Out-of-flow positioned
// descendants are excluded — they are not part of the element's ink
// overflow for focus-ring purposes.
func expandAutoOutlinesToInkOverflow(l *PaintLayer) {
	if l == nil {
		return
	}
	if l.OutlineStyle == "auto" && l.OutlineWidth > 0 && l.Box != nil {
		x0, y0 := l.Box.X, l.Box.Y
		x1, y1 := x0+l.Box.Width, y0+l.Box.Height
		if l.OutlineBox[2] > 0 {
			x0, y0 = l.OutlineBox[0], l.OutlineBox[1]
			x1, y1 = x0+l.OutlineBox[2], y0+l.OutlineBox[3]
		}
		var walk func(b *layout.Box)
		walk = func(b *layout.Box) {
			for _, c := range b.Children {
				if c == nil {
					continue
				}
				if c.Position == css.PositionAbsolute || c.Position == css.PositionFixed {
					continue
				}
				if c.Width > 0 && c.Height > 0 {
					x0 = min(x0, c.X)
					y0 = min(y0, c.Y)
					x1 = max(x1, c.X+c.Width)
					y1 = max(y1, c.Y+c.Height)
				}
				walk(c)
			}
		}
		walk(l.Box)
		l.OutlineBox = [4]float64{x0, y0, x1 - x0, y1 - y0}
	}
	for _, list := range [][]*PaintLayer{l.NegativeZ, l.AutoZero, l.PositiveZ, l.FlowChildren, l.FloatChildren} {
		for _, c := range list {
			expandAutoOutlinesToInkOverflow(c)
		}
	}
}

// buildLayerIndex builds a map from html.Node to PaintLayer for lookup
// during post-passes. Only the first layer per node is stored.
func buildLayerIndex(root *PaintLayer) map[*html.Node]*PaintLayer {
	idx := make(map[*html.Node]*PaintLayer)
	var collect func(*PaintLayer)
	collect = func(l *PaintLayer) {
		if l == nil || l.Box == nil {
			return
		}
		if l.Box.Node != nil {
			if _, ok := idx[l.Box.Node]; !ok {
				idx[l.Box.Node] = l
			}
		}
		for _, c := range l.NegativeZ {
			collect(c)
		}
		for _, c := range l.AutoZero {
			collect(c)
		}
		for _, c := range l.PositiveZ {
			collect(c)
		}
		for _, c := range l.FlowChildren {
			collect(c)
		}
		for _, c := range l.FloatChildren {
			collect(c)
		}
	}
	collect(root)
	return idx
}

// reparentZOrderByAncestorSC re-homes z-ordered layers to their nearest
// stacking-context-creating DOM ancestor. This corrects out-of-flow children
// hoisted past their DOM-ancestor SC by the layout containing-block rule.
// Mirrors Blink's PaintLayerStackingNode::AncestorStackingContext semantics
// (paint_layer_stacking_node.cc at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// Example: a static-position parent `#wc` establishes no containing block,
// so its absolutely-positioned child hoists to the ICB/root. buildPaintSubtree
// routes it to rootLayer.NegativeZ. This pass moves it back to #wc's z-list
// if #wc creates a stacking context.
func reparentZOrderByAncestorSC(root *PaintLayer) {
	if root == nil {
		return
	}
	idx := buildLayerIndex(root)

	// For each z-ordered layer, compute its correct z-order parent
	// (nearest ancestor in the DOM tree whose layer creates a stacking context)
	// and move it there if needed. Walk all z-lists recursively.
	var reparent func(*PaintLayer)
	reparent = func(parent *PaintLayer) {
		if parent == nil || parent.Box == nil {
			return
		}

		// Process and potentially reparent z-list children.
		parent.NegativeZ = reparentZList(parent.NegativeZ, parent, idx)
		parent.AutoZero = reparentZList(parent.AutoZero, parent, idx)
		parent.PositiveZ = reparentZList(parent.PositiveZ, parent, idx)

		// Recurse into all children.
		for _, c := range parent.NegativeZ {
			reparent(c)
		}
		for _, c := range parent.AutoZero {
			reparent(c)
		}
		for _, c := range parent.PositiveZ {
			reparent(c)
		}
		for _, c := range parent.FlowChildren {
			reparent(c)
		}
		for _, c := range parent.FloatChildren {
			reparent(c)
		}
	}
	reparent(root)
}

// reparentZList moves z-list entries to their correct ancestor stacking
// context parent if they were hoisted past it. Returns the filtered list
// for the current parent (entries that belong elsewhere are removed).
//
// Only absolutely-positioned children are reparented. Fixed-positioned
// children stay where buildPaintSubtree routed them, because fixed-CB
// semantics place them at the ICB or a fixed-CB ancestor, and the paint
// layer must respect that layout decision (even if the DOM parent creates
// a stacking context).
func reparentZList(zlist []*PaintLayer, parent *PaintLayer, idx map[*html.Node]*PaintLayer) []*PaintLayer {
	if len(zlist) == 0 {
		return zlist
	}

	filtered := make([]*PaintLayer, 0, len(zlist))
	for _, child := range zlist {
		if child == nil || child.Box == nil || child.Box.Node == nil {
			filtered = append(filtered, child)
			continue
		}

		// Only reparent absolutely-positioned children, not fixed-positioned.
		// Fixed children's layout CB (which may be an ancestor's fixed-CB or
		// the ICB) takes precedence over DOM ancestry for paint tree routing.
		if child.Box.Position == css.PositionFixed {
			filtered = append(filtered, child)
			continue
		}

		// Find the correct z-order parent by walking the DOM ancestor chain
		// until we find a layer that creates a stacking context.
		correctParent := parent
		for n := child.Box.Node.Parent; n != nil; n = n.Parent {
			if ancestorLayer, ok := idx[n]; ok && ancestorLayer.Box.CreatesStackingContext() {
				correctParent = ancestorLayer
				break
			}
		}

		// If the child belongs in a different parent, move it there.
		if correctParent != parent {
			// Determine the z-index bucket and append to the correct parent.
			z := child.ZIndex
			switch {
			case z < 0:
				correctParent.NegativeZ = append(correctParent.NegativeZ, child)
			case z > 0:
				correctParent.PositiveZ = append(correctParent.PositiveZ, child)
			default:
				correctParent.AutoZero = append(correctParent.AutoZero, child)
			}
			// Do NOT add to filtered — this entry is moved elsewhere.
		} else {
			// Child stays in current parent.
			filtered = append(filtered, child)
		}
	}
	return filtered
}

// propagateBackfaceHidden ensures every layer whose DOM ancestor (in the
// document tree, not the layout tree) is back-face-hidden also carries the
// flag. We build a `Node → *PaintLayer` index from the assembled tree, then
// for each layer walk its element's DOM Node.Parent chain looking up each
// ancestor's pre-computed BackfaceHidden flag.
func propagateBackfaceHidden(root *PaintLayer) {
	if root == nil {
		return
	}
	idx := buildLayerIndex(root)
	// For each layer, walk its DOM ancestors and inherit BackfaceHidden if
	// any ancestor element layer carries it.
	var propagate func(*PaintLayer)
	propagate = func(l *PaintLayer) {
		if l == nil || l.Box == nil {
			return
		}
		if !l.BackfaceHidden && l.Box.Node != nil {
			for n := l.Box.Node.Parent; n != nil; n = n.Parent {
				if anc, ok := idx[n]; ok && anc.BackfaceHidden {
					l.BackfaceHidden = true
					break
				}
			}
		}
		for _, c := range l.NegativeZ {
			propagate(c)
		}
		for _, c := range l.AutoZero {
			propagate(c)
		}
		for _, c := range l.PositiveZ {
			propagate(c)
		}
		for _, c := range l.FlowChildren {
			propagate(c)
		}
		for _, c := range l.FloatChildren {
			propagate(c)
		}
	}
	propagate(root)
}

// consolidateInlineOutlines resolves the outline-drawing geometry for split
// inline elements. A non-atomic inline element (display:inline) that spans
// multiple lines produces one PaintLayer per line fragment, each with its own
// outline. Drawn independently, the outlines leave gaps between lines. This
// pass unions the bounding boxes of all FlowChild layers that share the same
// html.Node and have a visible outline, stores the union on the first
// fragment's OutlineBox, and suppresses the outline on all other fragments.
//
// Only FlowChildren are inspected — z-ordered layers are self-painting contexts
// that manage their own outlines and don't participate in the inline flow.
//
// Mirrors Blink's OutlinePainter::PaintOutline, which collects all line boxes
// for a LayoutInline before drawing (outline_painter.cc @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
func consolidateInlineOutlines(root *PaintLayer) {
	if root == nil {
		return
	}
	var walk func(*PaintLayer)
	walk = func(parent *PaintLayer) {
		if parent == nil {
			return
		}

		// Scan FlowChildren for groups of layers sharing the same html.Node
		// with a visible outline on a pure-inline (display:inline) box.
		// nodeOrder preserves insertion (DOM) order for deterministic union iteration.
		nodeOrder := make([]*html.Node, 0)
		byNode := make(map[*html.Node][]*PaintLayer)

		for _, child := range parent.FlowChildren {
			if child == nil || child.Box == nil || child.Box.Node == nil {
				continue
			}
			if child.OutlineStyle == "none" || child.OutlineWidth <= 0 {
				continue
			}
			if child.Box.Style == nil || child.Box.Style.GetDisplay() != css.DisplayInline {
				continue
			}
			n := child.Box.Node
			if _, seen := byNode[n]; !seen {
				nodeOrder = append(nodeOrder, n)
			}
			byNode[n] = append(byNode[n], child)
		}

		// For each node with 2+ outline fragments, union the boxes and
		// suppress outline on all but the first fragment.
		for _, n := range nodeOrder {
			layers := byNode[n]
			if len(layers) < 2 {
				continue
			}
			// Compute the union bounding box.
			first := layers[0]
			uX := first.Box.X
			uY := first.Box.Y
			uR := first.Box.X + first.Box.Width
			uB := first.Box.Y + first.Box.Height
			for _, l := range layers[1:] {
				if l.Box.X < uX {
					uX = l.Box.X
				}
				if l.Box.Y < uY {
					uY = l.Box.Y
				}
				if r := l.Box.X + l.Box.Width; r > uR {
					uR = r
				}
				if b := l.Box.Y + l.Box.Height; b > uB {
					uB = b
				}
			}
			first.OutlineBox = [4]float64{uX, uY, uR - uX, uB - uY}
			// Suppress outline on all subsequent fragments.
			for _, l := range layers[1:] {
				l.OutlineStyle = "none"
			}
		}

		// Recurse into all children.
		for _, child := range parent.FlowChildren {
			walk(child)
		}
		for _, child := range parent.FloatChildren {
			walk(child)
		}
		for _, child := range parent.NegativeZ {
			walk(child)
		}
		for _, child := range parent.AutoZero {
			walk(child)
		}
		for _, child := range parent.PositiveZ {
			walk(child)
		}
	}
	walk(root)
}

func newPaintLayer(box *layout.Box) *PaintLayer {
	layer := &PaintLayer{
		Box:      box,
		Position: css.PositionStatic,
		Visible:  true,
		Opacity:  1.0,
	}
	s := box.Style
	if s == nil {
		// Unstyled root (rare): no paint properties to compute.
		return layer
	}

	layer.Position = box.Position
	if layer.Position == "" {
		layer.Position = css.PositionStatic
	}
	layer.ZIndex = box.ZIndex

	// Overflow clip (or paint containment clip at padding box).
	// Per CSS Overflow Level 3, overflow-x and overflow-y are independent:
	// a box may clip in one axis but not the other.
	overflowX := s.GetOverflowX()
	overflowY := s.GetOverflowY()

	// CSS Overflow 3 §3.3: when the BODY is the viewport-defining element
	// (root has overflow:visible, no containment, body is first body that
	// generates a box), the body's `overflow` propagates to the viewport
	// and the body's OWN paint pass uses `overflow: visible` (no body-local
	// clip). Mirrors Blink's `Document::ViewportDefiningElement()` query
	// consumed by `ViewPainter::PaintRootElementGroup` at chromium
	// @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if IsViewportDefiningBody(box) {
		overflowX = css.OverflowVisible
		overflowY = css.OverflowVisible
	}
	hasPaintContain := s.ShouldApplyPaintContainment()
	isClippedAxis := func(o css.OverflowType) bool {
		return o == css.OverflowHidden || o == css.OverflowScroll || o == css.OverflowAuto || o == css.OverflowClip
	}
	clipX := isClippedAxis(overflowX) || hasPaintContain
	clipY := isClippedAxis(overflowY) || hasPaintContain

	// CSS Tables 3 §5.4.1: rowspan cells whose span overlaps a
	// visibility:collapse row must clip content to their border-box,
	// regardless of the computed `overflow` property (the default on
	// <td> is `visible`). Layout sets Box.ClipContentToBorderBox on
	// such cells; here we promote it into a two-axis clip with the
	// border-box as the clip rectangle (not the padding box, per the
	// spec wording "clip the content to the table-cell's border-box").
	forceBorderBoxClip := box.ClipContentToBorderBox
	if forceBorderBoxClip {
		clipX = true
		clipY = true
	}

	// SVG roots are always clipped per Blink's
	// `LayoutSVGRoot::ComputeOverflowClipAxes` (Chromium @
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f) — "svg document roots
	// are always clipped". For an inline <svg> with no
	// overflow-clip-margin we still let `paintSVGRoot`'s own viewport
	// clip do the job (avoiding a duplicate pixel-snapped CSS clip
	// that can shift by 1px relative to the SVG content origin, which
	// is unsnapped). But when `overflow-clip-margin` is set, the SVG
	// must go through the standard CSS-overflow paint path — that is
	// the only path that consults the margin. We trigger that path
	// here by forcing clipX/clipY true when the SVG carries the
	// property.
	isSVGRootNode := box.Node != nil && box.Node.TagName == "svg"
	if isSVGRootNode {
		if _, ok := s.GetOverflowClipMargin(); ok {
			clipX = true
			clipY = true
		}
	}

	// No multicol-container clip special case. Blink's UpdateFromStyle
	// computes HasNonVisibleOverflow purely from (!IsOverflowVisibleAlongBothAxes()
	// || ShouldApplyPaintContainment()) && RespectsCSSOverflow(). There is no
	// multicol-specific branch — the clip is a function of the CSS overflow
	// property, not the formatting context type.

	// Phase 16.e+18 v2 B3: ClipBlockAxisOnly paint branch removed.
	// Blink has no per-column paint clip (box_fragment_painter.cc:
	// 1080-1114 — fragmentainer branch sets up a paint-cache scope but
	// no clip recorder). The Phase 12h F2 partial workaround that set
	// this flag is gone; layout-time floors (16.d.1 per-fragment clamp,
	// 16.d.2/3 TallestUnbreakable carrier) prevent the visual overflow
	// the clip used to mask. See findings.md § "Phase 16.d Blink
	// research" + "v2 Step 0 diagnostic".
	//
	// Phase 20 P20.3: column fragments now carry box.IsColumnBox
	// (forwarded from PhysicalFragment.BoxType == BoxTypeColumn).
	// Per Blink box_fragment_painter.cc::PaintBlockChild, the
	// fragmentainer branch only establishes a display-item-fragment
	// identity scope (paint cache identity across multiple fragments
	// of the same LayoutObject) — no clip is emitted. louis14 has no
	// paint-cache mechanism today, so IsColumnBox is currently
	// consumed only by drawColumnRules (P20.4) for rule extents; no
	// per-column clip is set here, matching Blink.

	if clipX || clipY {
		layer.HasClip = true
		layer.ClipX = clipX
		layer.ClipY = clipY

		// CSS Overflow 3 §3.2: overflow-clip-margin applies only when the
		// box is clipped via `overflow: clip` (on at least one axis) or via
		// paint containment, AND the box is NOT a scroll container (i.e.
		// no axis is scroll/auto/hidden). Blink's LayoutBox::OverflowClipRect
		// honors overflow_clip_margin only on the non-scroll clip path.
		//
		// SVG roots count as clip (not scroll) for this purpose: per Blink's
		// `LayoutSVGRoot::ComputeOverflowClipAxes`, an SVG root is always
		// clipped on both axes (UA behavior, not the cascaded `overflow`
		// value), so overflow-clip-margin set on the SVG applies. The
		// outer clip path is enabled only when the SVG actually has
		// `overflow-clip-margin` set — see the `isSVGRootNode` block
		// above for the rationale.
		isScrollContainer := overflowX == css.OverflowScroll || overflowX == css.OverflowAuto || overflowX == css.OverflowHidden ||
			overflowY == css.OverflowScroll || overflowY == css.OverflowAuto || overflowY == css.OverflowHidden
		if isSVGRootNode {
			isScrollContainer = false
		}
		applyMargin := false
		marginBox := css.OverflowClipMarginPaddingBox
		var marginLength float64
		if !isScrollContainer && (overflowX == css.OverflowClip || overflowY == css.OverflowClip || hasPaintContain || isSVGRootNode) {
			if m, ok := s.GetOverflowClipMargin(); ok {
				applyMargin = true
				marginBox = m.Box
				marginLength = m.Length
			}
		}

		var padX, padY, clipW, clipH float64
		// visualBoxInset records, per side, the linear shrink from the
		// border-box to the chosen visual-box (always ≥ 0). The clip
		// rect's geometry is `visual_box outset by marginLength on every
		// side`. The corresponding border-radius is computed lazily by
		// overflowClipRadii (coverage-factor corner correction shared
		// with box-shadow spread).
		var visualBoxInset [4]float64 // [Top, Right, Bottom, Left]
		switch {
		case forceBorderBoxClip:
			// CSS Tables 3 §5.4.1 forces a border-box clip irrespective of
			// the overflow property; no overflow-clip-margin involvement.
			padX = box.X
			padY = box.Y
			clipW = box.Width
			clipH = box.Height
		case applyMargin:
			switch marginBox {
			case css.OverflowClipMarginContentBox:
				visualBoxInset = [4]float64{
					box.Border.Top + box.Padding.Top,
					box.Border.Right + box.Padding.Right,
					box.Border.Bottom + box.Padding.Bottom,
					box.Border.Left + box.Padding.Left,
				}
			case css.OverflowClipMarginPaddingBox:
				visualBoxInset = [4]float64{
					box.Border.Top, box.Border.Right,
					box.Border.Bottom, box.Border.Left,
				}
			case css.OverflowClipMarginBorderBox:
				// All insets stay 0 — clip edge starts at the border edge.
			}
			// Per-side outward offset from the border-edge to the clip edge
			// (positive = outward, negative = inward).
			edgeTop := -visualBoxInset[0] + marginLength
			edgeRight := -visualBoxInset[1] + marginLength
			edgeBottom := -visualBoxInset[2] + marginLength
			edgeLeft := -visualBoxInset[3] + marginLength
			padX = box.X - edgeLeft
			padY = box.Y - edgeTop
			clipW = box.Width + edgeLeft + edgeRight
			clipH = box.Height + edgeTop + edgeBottom
		default:
			// Overflow clip defaults to the padding box minus the scrollbar gutter.
			// Per CSS Overflow §3, overflow:scroll/auto clips to the scrollable viewport,
			// which excludes the scrollbar reservation.
			padX = box.X + box.Border.Left + box.Scrollbar.Left
			padY = box.Y + box.Border.Top + box.Scrollbar.Top
			clipW = box.Width - box.Border.Left - box.Border.Right - box.Scrollbar.Left - box.Scrollbar.Right
			clipH = box.Height - box.Border.Top - box.Border.Bottom - box.Scrollbar.Top - box.Scrollbar.Bottom
		}
		if clipW < 0 {
			clipW = 0
		}
		if clipH < 0 {
			clipH = 0
		}

		// For an unclipped axis, extend the clip rect to a very large range
		// so that content is not clipped along that axis.
		const largeExtent = 1e7
		if !clipX {
			padX = -largeExtent / 2
			clipW = largeExtent
		}
		if !clipY {
			padY = -largeExtent / 2
			clipH = largeExtent
		}

		layer.ClipRect = [4]float64{padX, padY, clipW, clipH}
		// Stash the visual-box selection and outward length so
		// overflowClipRadii can rebuild the clip radii from the
		// padding-box radii with one corner-corrected outset, matching
		// Blink's `AdjustRoundedClipForOverflowClipMargin` flow.
		layer.ClipMarginVisualBoxInset = visualBoxInset
		layer.ClipMarginLength = marginLength
		layer.HasClipMargin = applyMargin && (visualBoxInset[0] != 0 ||
			visualBoxInset[1] != 0 || visualBoxInset[2] != 0 ||
			visualBoxInset[3] != 0 || marginLength != 0)
	}

	// CSS clip: rect() — applies to absolutely positioned elements (CSS 2.1 §11.1.2).
	// Values are physical offsets from the element's border-box top-left corner.
	if cr := s.GetClipRect(); cr != nil {
		layer.HasCSSClip = true
		clipRight := cr.Right
		if clipRight < 0 {
			clipRight = box.Width // "auto" sentinel
		}
		clipBottom := cr.Bottom
		if clipBottom < 0 {
			clipBottom = box.Height // "auto" sentinel
		}
		layer.CSSClipRect = [4]float64{
			box.X + cr.Left,
			box.Y + cr.Top,
			clipRight - cr.Left,
			clipBottom - cr.Top,
		}
	}

	// Compositing.
	if vis, ok := s.Get("visibility"); ok && (vis == "hidden" || vis == "collapse") {
		layer.Visible = false
	}
	layer.Opacity = s.GetOpacity()

	// Replaced element (img): capture src for paint-time image loading.
	if box.Node != nil && box.Node.TagName == "img" {
		if src, ok := box.Node.GetAttribute("src"); ok {
			layer.ImageSrc = src
		}
		layer.ObjectFit = s.GetObjectFit()
		opX, opY := s.GetObjectPosition()
		layer.ObjectPosition = [2]float64{opX, opY}
	}
	// image-rendering applies to all elements (images and background images).
	if ir, ok := s.Get("image-rendering"); ok {
		layer.ImageRendering = ir
	}

	// Background color.
	// Text runs (box.Text != "") inherit the parent element's style which may
	// include background-color, but CSS backgrounds are painted on element boxes,
	// not on individual text runs within them. Skip background for text runs
	// to avoid painting the inherited background outside the element's area.
	//
	// `background-color: currentcolor` resolves to the element's computed
	// `color` value per CSS Color 4 §4.4. ParseColor doesn't recognize
	// `currentcolor` as a color literal, so we resolve it here against the
	// same Style.GetColor() that other color-property accessors use
	// (e.g. GetColumnRuleColor in pkg/css/style.go). The same accessor handles
	// CSS Color 5 §4 relative-color forms `<func>(from currentColor ...)`
	// where the base color is the element's computed `color` value.
	if box.Text == "" {
		if bg, ok := s.Get("background-color"); ok {
			if c, ok := css.ParseColorWithCurrentColor(bg, s.GetColor()); ok {
				layer.BackgroundColor = c
			}
		}
	}

	// Background clip for background-color (uses bottom layer's clip per CSS spec).
	layer.BackgroundClip = s.GetBackgroundColorClip()

	// Background layers (multi-layer support via FillLayer linked list).
	// Text runs inherit the parent element's style but CSS background-image
	// is painted on element boxes, not on individual text runs within them.
	if box.Text == "" {
		layer.BackgroundLayers = s.GetBackgroundLayers()
	}
	// Fallback for single-layer backgrounds not caught by GetBackgroundLayers:
	// e.g., when background-image is a single gradient not in url() form.
	// Text runs do not paint backgrounds (see above).
	if layer.BackgroundLayers == nil && box.Text == "" {
		if val, ok := s.Get("background-image"); ok && val != "none" && val != "" {
			if isGradientValue(val) {
				layer.BackgroundLayers = &css.FillLayer{
					Gradient:      val,
					ImageSet:      true,
					Repeat:        s.GetBackgroundRepeat(),
					RepeatSet:     true,
					Position:      s.GetBackgroundPosition(),
					PositionSet:   true,
					Size:          s.GetBackgroundSize(),
					SizeSet:       true,
					Clip:          s.GetBackgroundClip(),
					ClipSet:       true,
					Origin:        s.GetBackgroundOrigin(),
					OriginSet:     true,
					Attachment:    s.GetBackgroundAttachment(),
					AttachmentSet: true,
				}
			}
		}
	}

	// Border colors: currentColor fallback.
	currentColor := css.Color{R: 0, G: 0, B: 0, A: 1.0}
	if cv, ok := s.Get("color"); ok {
		if c, ok := css.ParseColor(cv); ok {
			currentColor = c
		}
	}
	sides := [4]string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}
	for i, prop := range sides {
		if val, ok := s.Get(prop); ok {
			if c, ok := css.ParseColor(val); ok {
				layer.BorderColors[i] = c
				continue
			}
		}
		layer.BorderColors[i] = currentColor
	}

	// Border styles.
	bs := s.GetBorderStyle()
	layer.BorderStyles = [4]css.BorderStyle{bs.Top, bs.Right, bs.Bottom, bs.Left}

	// Border radius — resolve percentages against box dimensions and constrain.
	layer.BorderRadius = s.GetBorderRadiiResolved(box.Width, box.Height).
		ConstrainRadii(box.Width, box.Height)

	// Border image (9-slice). Only the url() form is supported in
	// rendering today; gradient values still pass through Style but
	// load-time path can't fetch them as images and they fall through
	// to regular borders. Phase 7.3 of LOU-138 typed the url() form;
	// gradient support would need a parallel typed field.
	if biSrc := s.GetBorderImageSource(); biSrc != "" && biSrc != "none" {
		if url, ok := css.ParseURLValue(biSrc); ok {
			layer.BorderImageSource = &css.CSSImageValue{Data: css.URLData{Relative: url, Absolute: url}}
			layer.BorderImageSlice = s.GetBorderImageSlice()
			bwArr := [4]float64{box.Border.Top, box.Border.Right, box.Border.Bottom, box.Border.Left}
			layer.BorderImageOutset = s.GetBorderImageOutset(bwArr)
			// Per CSS Backgrounds 3 §6.3, border-image-width <percentage>
			// resolves against the border image area's relevant dimension
			// (top/bottom against height, left/right against width). The
			// border image area is the border-box extended outward by
			// border-image-outset on each side. Mirrors Blink's
			// BorderImageLength::ResolveAsLength at SHA
			// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
			areaW := box.Width + layer.BorderImageOutset[1] + layer.BorderImageOutset[3]
			areaH := box.Height + layer.BorderImageOutset[0] + layer.BorderImageOutset[2]
			layer.BorderImageWidth, layer.BorderImageWidthAuto = s.GetBorderImageWidth(bwArr, areaW, areaH)
			layer.BorderImageRepeat = s.GetBorderImageRepeat()
		}
	}

	// Box shadows. Resolve currentcolor to the element's text color.
	layer.BoxShadows = s.GetBoxShadow()
	for i := range layer.BoxShadows {
		if layer.BoxShadows[i].UseCurrentColor {
			layer.BoxShadows[i].Color = currentColor
		}
	}

	// Outline — applies to element boxes only, not text runs.
	// Text run boxes inherit the parent element's style (including outline),
	// but CSS outlines are drawn on the element's border-box, not per text run.
	// This mirrors how background-color is suppressed for text runs above.
	if box.Text == "" {
		layer.OutlineStyle = s.GetOutlineStyle()
		if layer.OutlineStyle != "none" {
			layer.OutlineWidth = s.GetOutlineWidth()
			r, g, b, a := s.GetOutlineColor()
			layer.OutlineColor = css.Color{R: r, G: g, B: b, A: a}
			layer.OutlineOffset = s.GetOutlineOffset()
		}
	}

	// Text. The paint layer rasterizes glyphs at the *used* font-size so
	// `font-size-adjust` (CSS Fonts 5 §1.7) reaches the shaper. The 0 →
	// 16 clamp below is **disabled when the used size is genuinely 0**
	// (`font-size-adjust: 0` collapses glyphs); only negative or NaN-shaped
	// values fall back to 16.
	layer.TextColor = currentColor // default: currentColor
	layer.FontSize = s.GetUsedFontSize()
	if layer.FontSize < 0 {
		layer.FontSize = 16
	}
	layer.FontBold = s.GetFontWeight() == css.FontWeightBold
	layer.FontItalic = s.GetFontStyle() == css.FontStyleItalic
	layer.FontMono = s.IsMonospaceFamily()
	layer.FontAhem = s.IsAhemFamily()
	synth := s.GetFontSynthesis()
	layer.FontSynthesisWeight = synth.Weight
	layer.FontSynthesisStyle = synth.Style
	layer.FontSynthesisSmallCaps = synth.SmallCaps
	if family, ok := s.Get("font-family"); ok {
		layer.FontFamily = family
	}
	layer.LetterSpacing = s.GetLetterSpacing()
	layer.WordSpacing = s.GetWordSpacing()
	layer.TabSize, layer.TabSizeIsLength = s.GetTabSize()
	layer.IsVerticalText = box.IsVerticalText
	layer.IsSidewaysLR = box.IsSidewaysLR
	layer.IsSidewaysRL = box.IsSidewaysRL
	layer.IsWritingModeVerticalLR = box.IsWritingModeVerticalLR
	// IsUprightVertical: vertical-rl/vertical-lr writing modes that retain a
	// central baseline (text-orientation != "sideways"). Gates the
	// text-underline-position: left|right handling in the sideways-rotated
	// decoration painter — sideways-* and vertical+sideways must IGNORE those
	// keywords per CSS Text Decor 3 §3.5.
	{
		wm, _ := s.Get("writing-mode")
		to, _ := s.Get("text-orientation")
		layer.IsUprightVertical = (wm == "vertical-rl" || wm == "vertical-lr") && to != "sideways"
	}

	// Text decoration (CSS Text Decor 3 §2 — AppliedTextDecoration vector).
	// The new accumulated vector supersedes the legacy single-enum fields when
	// non-nil; Phase 2's geometry port will eventually retire them entirely.
	//
	// LOU-149 Phase 4: a text fragment that participates in a multi-fragment
	// decorating box carries its own stamped vector on the Box, with per-
	// fragment DecoratingBoxOriginX/Width + IsFirstFragment/IsLastFragment
	// flags. Prefer that when present; otherwise fall back to the cascade
	// vector (LOU-142 single-fragment behavior).
	if box.AppliedTextDecorations != nil {
		layer.AppliedTextDecorations = box.AppliedTextDecorations
	} else {
		layer.AppliedTextDecorations = s.GetAppliedTextDecorations()
	}

	// Legacy single-decoration fields. Kept for the (now-empty) fallback path
	// in drawTextDecoration when the AppliedTextDecorations vector is absent
	// (e.g. anonymous boxes synthesized without a cascade pass).
	layer.TextDecoration = s.GetTextDecoration()
	if decColor, ok := s.GetTextDecorationColor(); ok {
		layer.TextDecorationColor = decColor
	} else {
		layer.TextDecorationColor = currentColor
	}
	layer.TextDecorationThickness = s.GetTextDecorationThickness()
	layer.TextDecorationStyle = s.GetTextDecorationStyle()
	layer.TextDecorationSkipInk = s.GetTextDecorationSkipInk()
	layer.TextUnderlineOffset = s.GetTextUnderlineOffset()
	// text-underline-position is not yet typed in louis14's css pkg; pull the
	// raw cascaded string so vertical painters can detect explicit `left` /
	// `right` overrides without needing a full typed property.
	if tup, ok := s.Get("text-underline-position"); ok {
		layer.TextUnderlinePosition = tup
	}
	// CJK language hint for CSS Text Decor 3 §3.5's default-flip rule:
	// resolve the inherited `lang` attribute via DOM-tree walk (not cascade —
	// `lang` is HTML attribute inheritance) and classify the locale prefix.
	// Mirrors Blink's ComputeInheritedLanguage() → ComputedStyle::Locale()
	// chain consumed by ResolveUnderlinePosition. Box.Node may be the text
	// fragment's parent element; either way the walk terminates at the first
	// ancestor that declares `lang` (typically <body lang="ja"> or <html>).
	if box.Node != nil {
		layer.TextLangIsCJK = isCJKLocale(box.Node.InheritedLanguage())
	}
	layer.TextShadows = s.GetTextShadow()
	layer.FontVariantCaps = s.GetFontVariantCaps()

	// CSS font-feature-settings.
	if ffs, ok := s.Get("font-feature-settings"); ok && ffs != "normal" && ffs != "" {
		layer.FontFeatures = parseFontFeatureSettings(ffs)
	}

	// CSS Fonts 4 §6.7 font-variant-caps: emit the corresponding OpenType
	// feature tags so a font carrying native small-caps / petite-caps /
	// unicase / titling-caps glyphs activates them. Emission is unconditional
	// on font-synthesis-small-caps — §6.6 makes font-synthesis-small-caps
	// gate only the *synthesized* fallback (drawTextSmallCaps), not the
	// font's native feature. Mirrors Blink's
	// CapsFeatureSettingsScopedOverlay in
	// third_party/blink/renderer/platform/fonts/shaping/harfbuzz_shaper.cc
	// at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f, which is constructed
	// from FontDescription::VariantCaps() alone; synthesis is consulted only
	// at the synth-font selection site (OpenTypeCapsSupport::
	// SyntheticSmallCapsAllowed), never at feature emission. Appended AFTER
	// font-feature-settings so the high-level property wins per CSS Fonts 4
	// §7 "Resolution of font feature values".
	switch layer.FontVariantCaps {
	case "small-caps":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'s', 'm', 'c', 'p'}, Value: 1},
		)
	case "all-small-caps":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'s', 'm', 'c', 'p'}, Value: 1},
			textshape.FontFeature{Tag: [4]byte{'c', '2', 's', 'c'}, Value: 1},
		)
	case "petite-caps":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'p', 'c', 'a', 'p'}, Value: 1},
		)
	case "all-petite-caps":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'p', 'c', 'a', 'p'}, Value: 1},
			textshape.FontFeature{Tag: [4]byte{'c', '2', 'p', 'c'}, Value: 1},
		)
	case "unicase":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'u', 'n', 'i', 'c'}, Value: 1},
		)
	case "titling-caps":
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'t', 'i', 't', 'l'}, Value: 1},
		)
	}

	// CSS Fonts 4 §6.4 font-variant-ligatures: each sub-property keyword
	// toggles a fixed OpenType feature tag (CSS Fonts 4 §6.4 table). Emitted
	// AFTER font-feature-settings so the high-level property wins per CSS
	// Fonts 4 §7 "Resolution of font feature values". Mirrors Blink's
	// FontDescription::SetVariantLigatures →
	// FontDescription::FeatureSettings path in
	// third_party/blink/renderer/platform/fonts/font_description.cc at
	// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	//   normal                  -- HarfBuzz defaults (no emit needed).
	//   none                    -- liga/clig/calt/hlig/dlig all off.
	//   common-ligatures        -- liga=1, clig=1.
	//   no-common-ligatures     -- liga=0, clig=0.
	//   discretionary-ligatures -- dlig=1.
	//   no-discretionary-ligatures -- dlig=0.
	//   historical-ligatures    -- hlig=1.
	//   no-historical-ligatures -- hlig=0.
	//   contextual              -- calt=1.
	//   no-contextual           -- calt=0.
	//
	// Multiple keywords (space-separated) combine; conflicting pairs in the
	// same declaration are an authoring error and CSS Fonts 4 §6.4 leaves
	// last-wins to the UA; we honor whichever keyword appears last by
	// emission order (HarfBuzz also takes last-wins on duplicate tags).
	if ligs := s.GetFontVariantLigatures(); ligs != "" && ligs != "normal" {
		layer.FontFeatures = append(layer.FontFeatures,
			fontVariantLigatureFeatures(ligs)...)
	}

	// CSS Fonts 4 §6.4 font-variant-numeric: keyword → OpenType tag table.
	// Same emission-order rules as ligatures above. Mirrors
	// FontDescription::SetVariantNumeric() in font_description.cc at
	// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	//   normal             -- HarfBuzz defaults (no emit needed).
	//   lining-nums        -- lnum=1.
	//   oldstyle-nums      -- onum=1.
	//   proportional-nums  -- pnum=1.
	//   tabular-nums       -- tnum=1.
	//   diagonal-fractions -- frac=1.
	//   stacked-fractions  -- afrc=1.
	//   ordinal            -- ordn=1.
	//   slashed-zero       -- zero=1.
	if num := s.GetFontVariantNumeric(); num != "" && num != "normal" {
		layer.FontFeatures = append(layer.FontFeatures,
			fontVariantNumericFeatures(num)...)
	}

	// CSS Fonts 4 §6.5 font-variant-east-asian: keyword → OpenType tag table.
	// Same emission-order rules as ligatures/numeric above. Mirrors Blink's
	// FontDescription::SetVariantEastAsian() in font_description.cc at
	// Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	//   jis78               -- jp78=1.
	//   jis83               -- jp83=1.
	//   jis90               -- jp90=1.
	//   jis04               -- jp04=1.
	//   simplified          -- smpl=1.
	//   traditional         -- trad=1.
	//   full-width          -- fwid=1.
	//   proportional-width  -- pwid=1.
	//   ruby                -- ruby=1.
	if ea := s.GetFontVariantEastAsian(); ea != "" && ea != "normal" {
		layer.FontFeatures = append(layer.FontFeatures,
			fontVariantEastAsianFeatures(ea)...)
	}

	// CSS Fonts 4 §6.6 font-variant-alternates: keyword + functional notation
	// → OpenType tag table. Functional values (stylistic/styleset/
	// character-variant/swash/ornaments/annotation) carry @font-feature-values
	// names that resolve to a per-family index list at paint time. Mirrors
	// Blink's FontDescription::SetVariantAlternates() in font_description.cc
	// at Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	if alt := s.GetFontVariantAlternates(); alt != "" && alt != "normal" {
		family, _ := s.Get("font-family")
		layer.FontFeatures = append(layer.FontFeatures,
			fontVariantAlternatesFeatures(alt, family, s.FontFeatureValues)...)
	}

	// CSS Fonts 4 §6.2 font-kerning: only `none` is observable on top of
	// HarfBuzz defaults — `auto` and `normal` both leave the kern feature on
	// (HarfBuzz enables kern for horizontal text by default). When `none`,
	// push `kern=0` (and `vkrn=0` for vertical writing modes) AFTER any
	// font-feature-settings entries so the explicit property wins per §7
	// "Resolution of font feature values" (font-* properties override
	// font-feature-settings). Mirrors Blink's
	// HarfBuzzShaper SetFontFeatures path
	// (third_party/blink/renderer/platform/fonts/shaping/harfbuzz_shaper.cc)
	// where FontDescription::Kerning()==kNoneKerning emits kern=0/vkrn=0.
	if s.GetFontKerning() == css.FontKerningNone {
		layer.FontFeatures = append(layer.FontFeatures,
			textshape.FontFeature{Tag: [4]byte{'k', 'e', 'r', 'n'}, Value: 0},
			textshape.FontFeature{Tag: [4]byte{'v', 'k', 'r', 'n'}, Value: 0},
		)
	}

	// Text emphasis marks.
	isHorizontal := !box.IsVerticalText && !box.IsSidewaysRL && !box.IsSidewaysLR
	if mark := s.GetTextEmphasisMark(isHorizontal); mark != "" {
		layer.TextEmphasisMark = mark
		layer.TextEmphasisColor = s.GetTextEmphasisColor()
		layer.TextEmphasisOver = s.GetTextEmphasisLineLogicalOver(isHorizontal, box.IsSidewaysLR)
	}

	layer.TextTransform = s.GetTextTransform()
	layer.TextOverflow = s.GetTextOverflow()
	if layer.TextOverflow == css.TextOverflowString {
		layer.TextOverflowString = s.GetTextOverflowString()
	}

	// List markers are real laid-out ::marker boxes (marker-foundation Phases
	// 3-4) — no paint-layer marker setup is needed; the marker box fragment
	// paints itself like any other box.

	// CSS Transforms (individual properties + shorthand).
	// Per CSS Transforms Level 2, the effective transform is:
	//   translate * rotate * scale * transform
	// i.e., individual properties are applied first, then the shorthand.
	//
	// Per CSS Transforms Level 1 §3 "transformable element":
	//   "A transformable element is an element in one of these
	//    categories: all elements whose layout is governed by the CSS
	//    box model except for non-replaced inline boxes, table-column
	//    boxes, and table-column-group boxes, [+ certain SVG elements]."
	// (https://www.w3.org/TR/css-transforms-1/#transformable-element)
	//
	// So transform / translate / rotate / scale all silently no-op on
	// non-replaced inline-level boxes (`display: inline | ruby |
	// ruby-text`) and on table column / column-group boxes. Mirrors
	// Blink's gate in paint_property_tree_builder.cc:1310 (NeedsTransform,
	// :1299-:1319) at SHA 4883d11fef — `if (!object.IsBox()) return false;`
	// short-circuits transform consideration for non-atomic inlines.
	//
	// Text-fragment pseudo-boxes (box.Text != "") share their parent
	// element's *css.Style pointer. Reading the parent's transform here
	// would double-apply it — the parent element's own PaintLayer already
	// consumed the transform. Mirrors Blink's LayoutText which has no
	// TransformPropertyTreeNode. Same suppression pattern as background-color
	// above (line 594: `if box.Text == ""`).
	if layout.IsTransformableBox(s, box.Node) && box.Text == "" {
		// Collect individual transform properties.
		var individualTransforms []css.Transform
		if tx, ty, txPct, tyPct, ok := s.GetIndividualTranslate(); ok {
			if txPct {
				tx = (tx / 100) * box.Width
			}
			if tyPct {
				ty = (ty / 100) * box.Height
			}
			individualTransforms = append(individualTransforms, css.Transform{Type: "translate", Values: []float64{tx, ty}})
		}
		if deg, ok := s.GetIndividualRotate(); ok {
			individualTransforms = append(individualTransforms, css.Transform{Type: "rotate", Values: []float64{deg}})
		}
		if sx, sy, ok := s.GetIndividualScale(); ok {
			individualTransforms = append(individualTransforms, css.Transform{Type: "scale", Values: []float64{sx, sy}})
		}

		// Collect shorthand transforms.
		transforms := s.GetTransforms()

		if len(individualTransforms) > 0 || len(transforms) > 0 {
			layer.HasTransform = true
			layer.HasTransformPaint = true
			// ResolveTransformOriginPx handles length, percent, and calc()
			// (including calc-with-percent) relative to the border box.
			ox, oy := s.ResolveTransformOriginPx(box.Width, box.Height)
			layer.TransformOrigin = [2]float64{ox, oy}
			// Z-component (length only; default 0). Used by applyTransforms to
			// expand the 4×4 composition with T(0,0,oz) * M * T(0,0,-oz) so the
			// projected 2D affine correctly accounts for the 3D origin shift.
			layer.TransformOriginZ = s.ResolveTransformOriginZ()
			// Resolve percentage translate values in shorthand transforms via the
			// explicit IsPercent flag from the parser.
			resolved := make([]css.Transform, len(transforms))
			for i, t := range transforms {
				resolved[i] = css.Transform{Type: t.Type, Values: make([]float64, len(t.Values))}
				copy(resolved[i].Values, t.Values)
				if t.Type == "translate" {
					if len(resolved[i].Values) > 0 && len(t.IsPercent) > 0 && t.IsPercent[0] {
						resolved[i].Values[0] = (resolved[i].Values[0] / 100) * box.Width
					}
					if len(resolved[i].Values) > 1 && len(t.IsPercent) > 1 && t.IsPercent[1] {
						resolved[i].Values[1] = (resolved[i].Values[1] / 100) * box.Height
					}
				}
			}
			// Compose: individual properties first, then shorthand.
			layer.Transforms = append(individualTransforms, resolved...)
		}
	}

	// `will-change: transform` (or any will-change-any-transform-property
	// member) creates a transform paint context per CSS Will Change 1 §3:
	// for descendant fixed-attachment backgrounds it acts like a transform,
	// degrading them to scroll. This mirrors Blink's
	// HasWillChangeAnyTransformProperty() check in
	// background_image_geometry.cc @ 4883d11fef.
	if s.HasWillChangeAnyTransformProperty() {
		layer.HasTransformPaint = true
	}

	// CSS Transforms L2 §11 backface-visibility: when set to `hidden`, an
	// element is not painted if its back face is currently presented to the
	// viewer. The back face is determined by applying the element's own
	// transform (plus any preserve-3d ancestor transforms that share the
	// 3D rendering context) to the local normal vector (0, 0, 1) and
	// checking the sign of the resulting Z. Mirrors Blink's
	// PaintLayerPainter::ShouldPaintForBackfaceHidden @ SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	//
	// The subtree of a back-face-hidden ancestor is also skipped, including
	// out-of-flow descendants that have been hoisted into a higher
	// containing block (Blink handles this implicitly because the paint
	// walk descends through the parent's stacking-context paint subtree;
	// in louis14 OOF promotion would expose the escapee for paint, so we
	// propagate the flag down the DOM parent chain).
	if s.GetBackfaceVisibility() == css.BackfaceVisibilityHidden {
		layer.BackfaceHidden = computeBackfaceHidden(box, layer.Transforms)
	}

	// CSS Filters. SVG content elements (descendants of an inline <svg> other
	// than the SVG root itself) carry CSS `filter` only because the SVG
	// presentation attribute `filter="url(#id)"` maps to the CSS property
	// through the cascade. The SVG paint walk (svg_shape_painter.go) is the
	// authoritative consumer: paintWithSVGFilter is called from
	// svgShapePainter.paint when the shape has a `filter` style. Letting the
	// generic CSS-layer dispatch ALSO run paintLayerWithFilter would
	// double-paint the filter, and on the CSS path the reference box / region
	// resolves from the layer's HTML border-box (which has degenerate
	// 1-pixel-tall dimensions for an SVG <rect>'s flex/inline-box layout),
	// producing a wrong-sized FEImage source that leaks pixels past the SVG
	// content. Mirrors Blink's split: LayoutSVGShape/LayoutSVGContainer use
	// SVGObjectPainter (which routes filters through SVGFilterPainter); the
	// PaintLayer.Filter dispatch only fires for HTML LayoutBox descendants.
	filters := s.GetFilter()
	if len(filters) > 0 && !isSVGContentBox(box) {
		layer.Filters = filters
		layer.HasFilter = true
	}

	// CSS Backdrop Filters.
	//
	// backdrop-filter on the document element (<html>) is a no-op: the root
	// element forms the implicit backdrop root and there is no backdrop
	// content behind it to filter (CSS Filter Effects 2 §3 — the root's
	// backdrop is the canvas itself, which the filter does not apply to).
	// Mirrors Blink, which excludes the LayoutView/document element from
	// backdrop-filter painting (ComputedStyle::IsBackdropRoot handles the
	// root separately) @ 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
	bdFilters := s.GetBackdropFilter()
	if len(bdFilters) > 0 && !(box.Node != nil && box.Node.TagName == "html") {
		layer.BackdropFilters = bdFilters
		layer.HasBackdropFilter = true
	}

	// CSS clip-path.
	layer.ClipPath = s.GetClipPath()

	// CSS mask-image.
	if mi := s.GetMaskImage(); mi != "" && mi != "none" {
		layer.MaskImage = mi
		layer.HasMaskImage = true
	}

	// CSS mix-blend-mode.
	layer.BlendMode = s.GetMixBlendMode()

	// CSS Filter Effects 2 §3.5: Backdrop Root predicate. The flag is
	// consumed by paintLayer in combination with HasBackdropFilterDescendant
	// (set in the subtree walk) to decide whether to route this layer
	// through paintLayerIsolated so a descendant's backdrop-filter samples
	// the isolated buffer instead of the underlying canvas.
	if s.IsBackdropRoot() {
		layer.IsBackdropRoot = true
	}

	// Column rules for multicol containers.
	// Guards:
	//   - !IsColumnBox: per-column fragmentainers are tagged BoxTypeColumn
	//     and should never paint rules. Mirrors Blink, where PaintColumnRules
	//     is dispatched only from the multicol container's BoxFragmentPainter
	//     (box_fragment_painter.cc PaintColumnRules call site), never from
	//     kColumnBox fragmentainers.
	//   - Text == "": text fragments inherit Style from their parent element
	//     (inline_layout.go:1639 r.Item.Style), which for direct text
	//     children of a multicol container carries column-count > 0; without
	//     this guard each text run would paint its parent's rules at the
	//     text-run box origin, producing phantom rules inside columns
	//     (multicol-rule-solid-000.xht and siblings).
	if !box.IsColumnBox && box.Text == "" &&
		(s.GetColumnCount() > 0 || s.GetColumnWidth() > 0) {
		layer.IsMulticol = true
		layer.ColumnRuleWidth = clampLineWidth(s.GetColumnRuleWidth())
		layer.ColumnRuleStyle = s.GetColumnRuleStyle()
		layer.ColumnRuleColor = s.GetColumnRuleColor()
		layer.ColumnGap = s.GetColumnGapMulticol()

		// Compute used column count and width from the content width.
		contentW := box.Width - box.Border.Left - box.Border.Right - box.Padding.Left - box.Padding.Right
		if contentW < 0 {
			contentW = 0
		}
		colCount, colWidth := layout.ResolveColumnCountForPaint(contentW, s.GetColumnCount(), s.GetColumnWidth(), layer.ColumnGap)
		layer.ColumnCount = colCount
		layer.ColumnWidth = colWidth
		// CSS Multicol L1 §5: column rules are only drawn between columns that
		// both have content. With column-fill:auto a row may render fewer
		// columns than column-count, in which case multicol layout reports the
		// actual placed count via Box.RenderedColumnCount. Zero means
		// non-multicol or pre-12e fallback (paint all CSS-count rules).
		if box.RenderedColumnCount > 0 && box.RenderedColumnCount < colCount {
			layer.ColumnCount = box.RenderedColumnCount
		}
		// GapGeometry: forwarded from layout when column rules are active.
		if box.GapGeometry != nil {
			layer.GapGeometry = box.GapGeometry
		}
	}

	// empty-cells: hide — suppress background/border for empty table cells
	// in the separate border model (CSS 2.1 §17.6.1.1).
	if s.GetDisplay() == css.DisplayTableCell &&
		s.GetEmptyCells() == "hide" &&
		s.GetBorderCollapse() == css.BorderCollapseSeparate &&
		len(box.Children) == 0 &&
		box.Node != nil && isCellNodeEmpty(box.Node) {
		layer.EmptyCellHide = true
	}

	// border-collapse: collapse — defer cell border paint to after descendants
	// so cell content can't overpaint the (shared) border. CSS 2.1 §17.6.2
	// + CSS Tables 3 collapsed-borders paint phase; matches Blink's
	// TablePainter::PaintCollapsedBorders ordering. The flag is set on the
	// cell itself; the paint pipeline reads it to skip drawBorders during
	// the cell's own paintSelfDecorations and instead invokes drawBorders
	// after the cell's foreground walk completes.
	if s.GetDisplay() == css.DisplayTableCell &&
		s.GetBorderCollapse() == css.BorderCollapseCollapse {
		layer.IsCollapsedBorderCell = true
	}
	// Forward the missing-neighbor outward-strip widths produced by table
	// layout (CSS 2.1 §17.6.3; Blink table_painters.cc:356-362 @ SHA
	// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f). Non-zero only for
	// border-collapse:collapse cells with a missing neighbor on that
	// side; zero array on every other layer.
	layer.CollapsedBorderOutwardExtension = box.CollapsedBorderOutwardExtension

	return layer
}

// isCellNodeEmpty returns true if a table cell's DOM node has no visible content.
// Per CSS 2.1 §17.6.1.1: whitespace-only text is not "visible content",
// but &nbsp; (U+00A0) IS content, and any child element means not empty.
func isCellNodeEmpty(node *html.Node) bool {
	for _, child := range node.Children {
		if child.Type == html.TextNode {
			if strings.TrimSpace(child.Text) != "" {
				return false
			}
		} else if child.Type == html.ElementNode {
			return false
		}
	}
	return true
}

// The list-item ordinal is now resolved at layout-tree-build time by the
// MarkerTextSource adapter (layout_tree_builder.go getListItemCounterValue),
// feeding the real ::marker box's text — the paint-time computeListItemIndex
// DOM-sibling hack has been retired (marker-foundation Phase 4).

// domOrderedChildren returns the children of box in DOM tree order.
//
// In-flow children (block, inline, anonymous) are already laid out in DOM
// order and appear in box.Children in that order. Out-of-flow (abs-pos/fixed)
// children are appended at the end of box.Children by OutOfFlowLayoutPart,
// regardless of their DOM position. This function re-inserts out-of-flow
// children at their correct DOM position while keeping in-flow children
// (including anonymous boxes) in their original order.
func domOrderedChildren(box *layout.Box) []*layout.Box {
	// Flex containers: children are already in order-modified document order
	// from flex layout. CSS Flexbox §4.3 says flex items paint in this order,
	// NOT in DOM order. Skip the DOM reordering.
	if box.Style != nil {
		d := box.Style.GetDisplay()
		if d == css.DisplayFlex || d == css.DisplayInlineFlex {
			return box.Children
		}
	}

	lin := box.LayoutNode
	if lin == nil {
		return box.Children
	}

	// Identify out-of-flow (abs-pos/fixed) children. These are appended at
	// the end of box.Children by OutOfFlowLayoutPart out of DOM order.
	oofSet := make(map[*layout.Box]bool)
	for _, child := range box.Children {
		if child.Position == css.PositionAbsolute || child.Position == css.PositionFixed {
			oofSet[child] = true
		}
	}

	if len(oofSet) == 0 {
		// No out-of-flow children — in-flow order is already correct.
		return box.Children
	}

	// Build LIN → Box map for out-of-flow children only.
	byLIN := make(map[*layout.LayoutInputNode]*layout.Box, len(oofSet))
	for _, child := range box.Children {
		if oofSet[child] && child.LayoutNode != nil {
			byLIN[child.LayoutNode] = child
		}
	}

	// Collect in-flow children in their original (already DOM-correct) order.
	inFlow := make([]*layout.Box, 0, len(box.Children)-len(oofSet))
	for _, child := range box.Children {
		if !oofSet[child] {
			inFlow = append(inFlow, child)
		}
	}

	// Walk lin.Children() (DOM order) to interleave out-of-flow boxes at their
	// correct positions. Each non-OOF LIN child corresponds to one in-flow box
	// (block, anonymous block, or continuation). OOF LIN children are inserted
	// at their DOM position without consuming an in-flow slot.
	result := make([]*layout.Box, 0, len(box.Children))
	inserted := make(map[*layout.Box]bool)
	inFlowIdx := 0
	for _, linChild := range lin.Children() {
		if cb, ok := byLIN[linChild]; ok {
			// Out-of-flow child: insert at this DOM position.
			result = append(result, cb)
			inserted[cb] = true
		} else if linChild.IsText() {
			// Text node: block layout never emits a box child for text nodes
			// (they are handled by inline layout inside anonymous blocks).
			// Skip without consuming an inFlow slot.
			continue
		} else {
			// In-flow (or anonymous) child: emit next in-flow box in order.
			if inFlowIdx < len(inFlow) {
				result = append(result, inFlow[inFlowIdx])
				inFlowIdx++
			}
		}
	}
	// Emit any remaining in-flow boxes (e.g. line boxes with no LIN).
	for ; inFlowIdx < len(inFlow); inFlowIdx++ {
		result = append(result, inFlow[inFlowIdx])
	}
	// Insert OOF children that propagated up from descendants (not direct
	// DOM children of this box) at their correct DOM position.
	// These weren't found during the DOM walk above because their
	// LayoutInputNode lives deeper in the tree. We use DOMIndex to find
	// the ancestor that IS a direct child of this box, then insert the
	// OOF child just before that ancestor so it's processed in correct
	// DOM tree order relative to positioned descendants encountered
	// during subtree recursion (CSS 2.1 Appendix E).
	for _, child := range box.Children {
		if oofSet[child] && !inserted[child] {
			pos := len(result)
			// Find the DOMIndex of the direct child (of this box's LIN)
			// that is an ancestor of this OOF child. The OOF child should
			// be painted within that ancestor's subtree.
			if child.LayoutNode != nil && lin != nil {
				ancestorIdx := findAncestorDOMIndex(child.LayoutNode, lin)
				if ancestorIdx >= 0 {
					// Insert just before the ancestor in the result, so the
					// OOF child is processed before the ancestor's subtree.
					for k := 0; k < len(result); k++ {
						if result[k].DOMIndex == ancestorIdx {
							pos = k
							break
						}
					}
				}
			}
			result = append(result, nil)
			copy(result[pos+1:], result[pos:])
			result[pos] = child
		}
	}
	return result
}

// findAncestorDOMIndex walks up from lin's DOM node to find the
// LayoutInputNode that is a direct child of parentLIN, returning its DOMIndex.
// Returns -1 if not found.
func findAncestorDOMIndex(lin, parentLIN *layout.LayoutInputNode) int {
	if lin.DOMNode == nil || parentLIN.DOMNode == nil {
		return -1
	}
	parentDOM := parentLIN.DOMNode
	// Walk up the DOM tree from lin's node to find the child of parentDOM.
	node := lin.DOMNode
	for node != nil && node.Parent != parentDOM {
		node = node.Parent
	}
	if node == nil {
		return -1
	}
	// node is a direct child of parentDOM. Find its LIN.
	for _, ch := range parentLIN.Children() {
		if ch.DOMNode == node {
			return ch.DOMIndex
		}
	}
	return -1
}

// isFlexContainer returns true if box is a flex or inline-flex container.
func isFlexContainer(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	d := box.Style.GetDisplay()
	return d == css.DisplayFlex || d == css.DisplayInlineFlex
}

// paintOrderChildren returns the children of box in the order they should
// be painted. For flex containers, box.Children is already in order-modified
// document order (sorted by the CSS order property during flex layout).
// For other boxes, DOM tree order is used.
func paintOrderChildren(box *layout.Box) []*layout.Box {
	if isFlexContainer(box) {
		// Flex layout produces children in order-modified document order
		// (sorted by CSS order property, ties broken by DOM order).
		// Use this order directly — do NOT re-sort to DOM order.
		return box.Children
	}
	return moveOutsideMarkerToFront(domOrderedChildren(box))
}

// moveOutsideMarkerToFront reorders a list item's children so an OUTSIDE
// ::marker box paints FIRST (under the item's content). CSS Lists: an outside
// marker is generated content at the start of the list item — like ::before, it
// paints before (under) the principal content. louis14's layout appends the
// marker fragment LAST (UnpositionedListMarker.AddToBox adds it after the
// content's line boxes), so in raw child order it would paint OVER the content.
// That is invisible for an ordinary text bullet sitting in the gutter, but a
// wide image marker (list-style-image / ::marker content:url()) overflows into
// the content box and abuts the text; painted last it occludes the glyph's
// antialiased edge where they meet (LOU-337 / css-pseudo marker-content-012:
// the "item" glyph against the 32px green marker). The reference models the
// same marker as an inline-block ::before, which paints before the text — this
// reordering reproduces that. Returns the slice unchanged when there is no
// outside marker child (the common case) or it is already first.
func moveOutsideMarkerToFront(children []*layout.Box) []*layout.Box {
	markerIdx := -1
	for i, c := range children {
		if c.LayoutNode != nil && c.LayoutNode.IsMarkerNode() && c.LayoutNode.MarkerIsOutside {
			markerIdx = i
			break
		}
	}
	if markerIdx <= 0 {
		return children
	}
	reordered := make([]*layout.Box, 0, len(children))
	reordered = append(reordered, children[markerIdx])
	reordered = append(reordered, children[:markerIdx]...)
	reordered = append(reordered, children[markerIdx+1:]...)
	return reordered
}

// buildPaintSubtree walks the Box tree, creating PaintLayers and assigning
// them to the correct parent/stacking-context lists.
//
// parentLayer:        the PaintLayer that owns FlowChildren at this level.
// currentSC:          the nearest ancestor stacking context's PaintLayer.
// currentBackdropRoot: the nearest ancestor PaintLayer with IsBackdropRoot set.
//
//	Per CSS Filter Effects 2 §3.5, this is the boundary of
//	the backdrop sampled by any backdrop-filter descendant
//	within the subtree. Set by HasBackdropFilterDescendant
//	when a descendant with backdrop-filter is encountered.
//
// ensureInlinePaintGroup returns the single PaintLayer for an inline paint
// group (an opacity inline — see layout.IsInlinePaintGroup), creating it on
// first sighting. The layer's Box is synthesized (zero-rect, the inline's
// style+node): geometry lives on the member fragments routed into
// FlowChildren/FloatChildren; the layer exists to apply the element's alpha
// ONCE around all of them. Keyed by the OpenTag InlineItem, not the style
// pointer — inline layout clones styles (e.g. the position reset on
// background fragments), but every fragment of one inline references the
// same item. The group routes like today's per-fragment SC inline layers:
// AutoZero of the nearest stacking context (CSS Color 3 §3.2 "treated as if
// it created a new stacking context", painted as a positioned z-index:0
// element at Appendix E step 6); a nested group nests in its outer group.
func ensureInlinePaintGroup(item *layout.InlineItem, currentSC *PaintLayer, groups map[*layout.InlineItem]*PaintLayer) *PaintLayer {
	if g, ok := groups[item]; ok {
		return g
	}
	g := newPaintLayer(&layout.Box{Node: item.Node, Style: item.Style})
	groups[item] = g
	if item.EnclosingPaintGroup != nil {
		outer := ensureInlinePaintGroup(item.EnclosingPaintGroup, currentSC, groups)
		outer.AutoZero = append(outer.AutoZero, g)
	} else {
		currentSC.AutoZero = append(currentSC.AutoZero, g)
	}
	return g
}

// noteCompositingDescendants records the compositing bookkeeping a child layer
// imposes on its ancestors and returns the backdrop root in effect for the
// child's own subtree. CSS Compositing 1 §8: a blended child makes its nearest
// ancestor stacking context an isolated group. CSS Filter Effects 2 §3.5: a
// backdrop-filter child samples back to its nearest Backdrop Root. Shared by
// the inline-paint-group branch and the ordinary descendant walk so grouped
// members get identical treatment.
func noteCompositingDescendants(childLayer, enclosingSC, currentBackdropRoot *PaintLayer) *PaintLayer {
	if childLayer.BlendMode != css.MixBlendModeNormal && childLayer.BlendMode != "" {
		enclosingSC.HasBlendingDescendant = true
	}
	if childLayer.HasBackdropFilter && currentBackdropRoot != nil {
		currentBackdropRoot.HasBackdropFilterDescendant = true
	}
	if childLayer.IsBackdropRoot {
		return childLayer
	}
	return currentBackdropRoot
}

func buildPaintSubtree(box *layout.Box, parentLayer, currentSC, currentBackdropRoot *PaintLayer, groups map[*layout.InlineItem]*PaintLayer) {
	for _, child := range paintOrderChildren(box) {
		if child.Style == nil {
			// Unstyled box (line box, text run) — no PaintLayer.
			// Recurse to find any styled descendants.
			buildPaintSubtree(child, parentLayer, currentSC, currentBackdropRoot, groups)
			continue
		}
		// CSS Color 3 §3.2: opacity on a non-atomic inline is GROUP opacity
		// over the whole element. Inline layout stamps every fragment that
		// must composite inside such a group (the group span's own per-line
		// background fragments, text runs, atomic inlines, and floats) with
		// Box.PaintGroup; all of them route into ONE lazily-created layer per
		// group inline, which applies the alpha once. Mirrors Blink's
		// LayoutObject::PaintingLayer (layout_object.cc:1218 @ 4883d11f)
		// resolving every fragment and inline-contained float of the
		// LayoutInline to the same self-painting PaintLayer.
		if child.PaintGroup != nil {
			groupLayer := ensureInlinePaintGroup(child.PaintGroup, currentSC, groups)
			childLayer := newPaintLayer(child)
			// The group applies the element's alpha once; the element's own
			// fragments (which carry the same style) must not re-apply it.
			// A synthetic ::first-line group (LOU-305) has no DOM node and owns
			// the alpha of ALL its direct members — the band and the first-line
			// text/atomic fragments each carry the merged ::first-line opacity,
			// so every one must drop to 1.0 and let the group composite it once.
			ownFragment := child.PaintGroup.Node == nil ||
				(child.Node != nil && child.Node == child.PaintGroup.Node)
			if ownFragment {
				childLayer.Opacity = 1.0
			}
			// A member that is itself a genuine stacking context (e.g. an
			// atomic inline with its own opacity/transform) collects its own
			// descendants; the group's own fragments do not.
			memberSC := groupLayer
			if !ownFragment && child.CreatesStackingContext() {
				memberSC = childLayer
			}
			// A grouped member that blends or samples a backdrop imposes the
			// same isolation bookkeeping as any other descendant — its
			// enclosing stacking context is the group layer.
			childBackdropRoot := noteCompositingDescendants(childLayer, groupLayer, currentBackdropRoot)
			if isFloat(child) {
				// CSS 2.1 Appendix E step 4 inside the group: the float
				// paints below the inline's background and text.
				groupLayer.FloatChildren = append(groupLayer.FloatChildren, childLayer)
			} else {
				groupLayer.FlowChildren = append(groupLayer.FlowChildren, childLayer)
			}
			buildPaintSubtree(child, childLayer, memberSC, childBackdropRoot, groups)
			continue
		}
		// Text fragments (LayoutNode==nil with Text set) carry their parent
		// element's Style so the renderer can resolve font/color. The
		// parent style's `float:left` does NOT apply to text content —
		// a text run is inline-level and paints during the parent's
		// foreground phase, not as an independent float at step 4.
		if child.LayoutNode == nil && child.Text != "" {
			childLayer := newPaintLayer(child)
			parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
			continue
		}

		childLayer := newPaintLayer(child)
		isPositioned := child.Position != css.PositionStatic && child.Position != ""

		// Blend / backdrop-filter isolation bookkeeping (CSS Compositing 1 §8,
		// CSS Filter Effects 2 §3.5). currentBackdropRoot may be nil for
		// descendants of the implicit root backdrop-root — the canvas IS the
		// backdrop there, so no extra isolation is needed.
		childBackdropRoot := noteCompositingDescendants(childLayer, currentSC, currentBackdropRoot)

		// CSS Flexbox §4.3: Flex items with explicit z-index create stacking
		// contexts even if position is static. They participate in the nearest
		// ancestor stacking context's z-lists, just like positioned elements
		// with explicit z-index.
		if !isPositioned && child.IsFlexItem() && child.Style.HasExplicitZIndex() {
			z := child.ZIndex
			switch {
			case z < 0:
				currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
			case z > 0:
				currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
			default:
				currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			}
			// New stacking context — descendants collected by childLayer.
			buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
			continue
		}

		if !isPositioned {
			if child.CreatesStackingContext() {
				// Non-positioned elements that create stacking contexts (due to
				// transform, opacity, will-change, contain:layout/paint, etc.)
				// paint at CSS 2.1 Appendix E step 6 in DOM order, alongside
				// positioned z-index:auto descendants. The CSS Stacking spec
				// (and all browser implementations) treat all z-index:auto
				// stacking contexts uniformly at step 6 — "flow order" (step 3/5)
				// only applies to non-SC elements.
				//
				// Exception: if the SC is contained within an overflow-clipping
				// ancestor, keep it in FlowChildren so the parent's HasClip bracket
				// clips it naturally — same logic as the positioned path at line
				// ~1844 below. A non-positioned SC contained by overflow must NOT
				// escape to the ancestor SC's AutoZero list or the clip is lost.
				if isFloat(child) {
					parentLayer.FloatChildren = append(parentLayer.FloatChildren, childLayer)
					buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
				} else if isContainedByAnyOverflow(box, currentSC.Box) {
					// Contained by an overflow-clipping ancestor between this box
					// and the SC boundary — stay in flow order so the clip applies.
					parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
					buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
				} else if box.Style != nil && box.Style.GetTransformStyle() == css.TransformStylePreserve3D {
					// Inside a preserve-3d rendering context: children's visual
					// order is 3D-sorted, not 2D-stacked. Keep in FlowChildren so
					// applyTransforms' preserve-3d ancestor composition fires at
					// PhaseForeground and the 3D z-order is respected.
					parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
					buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
				} else {
					currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
					buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
				}
			} else if isFloat(child) {
				// CSS 2.1 Appendix E step 4: floats paint after non-float block
				// backgrounds (step 3) so they appear above block backgrounds.
				parentLayer.FloatChildren = append(parentLayer.FloatChildren, childLayer)
				buildPaintSubtree(child, childLayer, currentSC, childBackdropRoot, groups)
			} else {
				parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
				buildPaintSubtree(child, childLayer, currentSC, childBackdropRoot, groups)
			}
			continue
		}

		// Positioned child forming a stacking context. Per CSS 2.1 Appendix E,
		// z-index ordering takes precedence over overflow containment — the
		// child is z-sorted in the nearest ancestor stacking context even
		// when clipped by a parent's overflow. Clipping is applied at paint
		// time; it does not change z-order.
		//
		// When the child is contained by an ancestor's overflow clip but
		// escapes to a higher stacking context for paint, capture the
		// chain of intervening ancestor overflow clips on the child so
		// the renderer can Push them before painting. Blink achieves the
		// same effect via the paint property tree, which all descendants
		// inherit regardless of stacking. See AncestorOverflowClips above.
		if child.CreatesStackingContext() {
			childLayer.AncestorOverflowClips = collectAncestorOverflowClips(child, currentSC)
			z := child.ZIndex
			switch {
			case z < 0:
				currentSC.NegativeZ = append(currentSC.NegativeZ, childLayer)
			case z > 0:
				currentSC.PositiveZ = append(currentSC.PositiveZ, childLayer)
			default:
				currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
			}
			// New stacking context — descendants collected by childLayer.
			buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
			continue
		}

		// Positioned child without stacking context. Check overflow clip:
		// contained children stay in DOM-order painting (FlowChildren).
		if isContainedByOverflow(child, box) {
			parentLayer.FlowChildren = append(parentLayer.FlowChildren, childLayer)
			buildPaintSubtree(child, childLayer, currentSC, childBackdropRoot, groups)
			continue
		}

		// Positioned, z-index:auto, not contained — participates at Appendix E step 6.
		currentSC.AutoZero = append(currentSC.AutoZero, childLayer)
		if hasOverflowClipping(child) {
			// Overflow containment boundary — positioned descendants
			// stay within this subtree.
			buildPaintSubtree(child, childLayer, childLayer, childBackdropRoot, groups)
		} else {
			buildPaintSubtree(child, childLayer, currentSC, childBackdropRoot, groups)
		}
	}
}

// isContainedByAnyOverflow returns true if immediateParent or any ancestor up
// to (but not including) scRoot has overflow clipping. Used to decide whether a
// non-positioned SC should stay in FlowChildren (where the parent's HasClip
// bracket clips it naturally) or be promoted to AutoZero (step 6 paint order).
func isContainedByAnyOverflow(immediateParent *layout.Box, scRoot *layout.Box) bool {
	for ancestor := immediateParent; ancestor != nil && ancestor != scRoot; ancestor = ancestor.Parent {
		if hasOverflowClipping(ancestor) {
			return true
		}
	}
	return false
}

// isFloat returns true if the box has float:left or float:right.
func isFloat(box *layout.Box) bool {
	if box.Style == nil {
		return false
	}
	f := box.Style.GetFloat()
	return f == css.FloatLeft || f == css.FloatRight
}

// collectAncestorOverflowClips walks from `child`'s parent up to (but not
// including) the box that owns `currentSC`, collecting overflow-clip
// rectangles from any ancestor that has overflow clipping AND would
// contain `child`. Returns clip rects in innermost→outermost order.
//
// This is the louis14 substitute for the Blink paint property tree's
// inherited clip chain. It runs only on positioned stacking-context
// children that escape their parent box's overflow via z-list escalation
// (see buildPaintSubtree). The collected clips are Pushed by the renderer
// before painting the child layer, ensuring the overflow clip honoured
// even though the child paints outside its parent's HasClip bracket.
//
// Per CSS Overflow 3 §3.3, `overflow: clip` clips descendants without
// establishing a stacking context — the very case that requires this
// helper. Hidden/scroll/auto behave the same in louis14's model
// because none of them are SCs by themselves in this codebase.
func collectAncestorOverflowClips(child *layout.Box, currentSC *PaintLayer) [][4]float64 {
	var scBox *layout.Box
	if currentSC != nil {
		scBox = currentSC.Box
	}
	var clips [][4]float64
	for ancestor := child.Parent; ancestor != nil && ancestor != scBox; ancestor = ancestor.Parent {
		if !hasOverflowClipping(ancestor) {
			continue
		}
		if !isContainedByOverflow(child, ancestor) {
			continue
		}
		// Padding-box clip rectangle. Matches the default clip in
		// newPaintLayer's overflow-clip path (paint_layer.go:557-560).
		// overflow-clip-margin extension is not modelled here; the
		// failing tests do not exercise it. When/if needed, mirror the
		// applyMargin geometry from newPaintLayer.
		clipX := ancestor.X + ancestor.Border.Left
		clipY := ancestor.Y + ancestor.Border.Top
		clipW := ancestor.Width - ancestor.Border.Left - ancestor.Border.Right
		clipH := ancestor.Height - ancestor.Border.Top - ancestor.Border.Bottom
		if clipW < 0 {
			clipW = 0
		}
		if clipH < 0 {
			clipH = 0
		}
		// Per-axis: when only one axis is clipped (overflow: clip per CSS
		// Overflow 3 §3.3 keeps the other axis visible when paired with
		// `visible`), extend the unclipped axis to a large range so the
		// rectangle effectively clips only the active axis. Mirrors the
		// large-extent encoding in newPaintLayer (paint_layer.go:572-579).
		ancStyle := ancestor.Style
		overflowX := ancStyle.GetOverflowX()
		overflowY := ancStyle.GetOverflowY()
		clipAxisActive := func(o css.OverflowType) bool {
			return o == css.OverflowHidden || o == css.OverflowScroll ||
				o == css.OverflowAuto || o == css.OverflowClip
		}
		axisClipX := clipAxisActive(overflowX) || ancStyle.ShouldApplyPaintContainment()
		axisClipY := clipAxisActive(overflowY) || ancStyle.ShouldApplyPaintContainment()
		const largeExtent = 1e7
		if !axisClipX {
			clipX = -largeExtent / 2
			clipW = largeExtent
		}
		if !axisClipY {
			clipY = -largeExtent / 2
			clipH = largeExtent
		}
		clips = append(clips, [4]float64{clipX, clipY, clipW, clipH})
	}
	return clips
}

// fontVariantLigatureFeatures maps a CSS font-variant-ligatures value (one
// or more space-separated keywords from §6.4) to the OpenType feature tag
// list per CSS Fonts 4 §6.4 (Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f,
// FontDescription::SetVariantLigatures in
// third_party/blink/renderer/platform/fonts/font_description.cc).
//
// Callers handle `normal` (empty/default) themselves; this function assumes
// non-default input. `none` emits the full five-tag off list per spec.
func fontVariantLigatureFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" {
		return []textshape.FontFeature{
			{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 0},
			{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 0},
			{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 0},
			{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 0},
			{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 0},
		}
	}
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "common-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 1},
				textshape.FontFeature{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-common-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'i', 'g', 'a'}, Value: 0},
				textshape.FontFeature{Tag: [4]byte{'c', 'l', 'i', 'g'}, Value: 0},
			)
		case "discretionary-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-discretionary-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'d', 'l', 'i', 'g'}, Value: 0},
			)
		case "historical-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 1},
			)
		case "no-historical-ligatures":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'h', 'l', 'i', 'g'}, Value: 0},
			)
		case "contextual":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 1},
			)
		case "no-contextual":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'c', 'a', 'l', 't'}, Value: 0},
			)
		}
	}
	return features
}

// fontVariantNumericFeatures maps a CSS font-variant-numeric value to the
// OpenType feature tag list per CSS Fonts 4 §6.4. Mirrors Blink's
// FontDescription::SetVariantNumeric in font_description.cc at Chromium
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Callers handle `normal`/empty themselves.
func fontVariantNumericFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "lining-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'l', 'n', 'u', 'm'}, Value: 1},
			)
		case "oldstyle-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'o', 'n', 'u', 'm'}, Value: 1},
			)
		case "proportional-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'p', 'n', 'u', 'm'}, Value: 1},
			)
		case "tabular-nums":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'t', 'n', 'u', 'm'}, Value: 1},
			)
		case "diagonal-fractions":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'f', 'r', 'a', 'c'}, Value: 1},
			)
		case "stacked-fractions":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'a', 'f', 'r', 'c'}, Value: 1},
			)
		case "ordinal":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'o', 'r', 'd', 'n'}, Value: 1},
			)
		case "slashed-zero":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'z', 'e', 'r', 'o'}, Value: 1},
			)
		}
	}
	return features
}

// fontVariantEastAsianFeatures maps a CSS font-variant-east-asian value
// (CSS Fonts 4 §6.5) to the OpenType feature tag list. Mirrors Blink's
// FontDescription::SetVariantEastAsian in
// third_party/blink/renderer/platform/fonts/font_description.cc at Chromium
// SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Callers handle `normal`/empty themselves.
func fontVariantEastAsianFeatures(value string) []textshape.FontFeature {
	value = strings.ToLower(strings.TrimSpace(value))
	var features []textshape.FontFeature
	for _, kw := range strings.Fields(value) {
		switch kw {
		case "jis78":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '7', '8'}, Value: 1},
			)
		case "jis83":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '8', '3'}, Value: 1},
			)
		case "jis90":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '9', '0'}, Value: 1},
			)
		case "jis04":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'j', 'p', '0', '4'}, Value: 1},
			)
		case "simplified":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'s', 'm', 'p', 'l'}, Value: 1},
			)
		case "traditional":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'t', 'r', 'a', 'd'}, Value: 1},
			)
		case "full-width":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'f', 'w', 'i', 'd'}, Value: 1},
			)
		case "proportional-width":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'p', 'w', 'i', 'd'}, Value: 1},
			)
		case "ruby":
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'r', 'u', 'b', 'y'}, Value: 1},
			)
		}
	}
	return features
}

// fontVariantAlternatesFeatures maps a CSS font-variant-alternates value to
// the OpenType feature tag list per CSS Fonts 4 §6.6. Functional notations
// (stylistic/styleset/character-variant/swash/ornaments/annotation) carry
// @font-feature-values names that resolve against `ffvRules` filtered by
// `family` (the element's first font-family).
//
// Mirrors Blink's FontDescription::SetVariantAlternates path in
// font_description.cc at Chromium SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// Callers handle `normal`/empty themselves.
func fontVariantAlternatesFeatures(value, family string, ffvRules []css.FontFeatureValuesRule) []textshape.FontFeature {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	// Resolve the @font-feature-values rule for this family once.
	ffv := css.LookupFontFeatureValues(ffvRules, firstFontFamily(family))

	var features []textshape.FontFeature

	// Tokenize: the value is a space-separated mix of bare keywords and
	// functional notations like `stylistic(name)` or `styleset(a, b, c)`.
	tokens := splitAlternatesTokens(value)
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// Bare `historical-forms` keyword (the only non-functional positional).
		if strings.EqualFold(tok, "historical-forms") {
			features = append(features,
				textshape.FontFeature{Tag: [4]byte{'h', 'i', 's', 't'}, Value: 1},
			)
			continue
		}
		// Functional: name(args).
		openParen := strings.IndexByte(tok, '(')
		if openParen < 0 || !strings.HasSuffix(tok, ")") {
			continue
		}
		fnName := strings.ToLower(strings.TrimSpace(tok[:openParen]))
		argStr := tok[openParen+1 : len(tok)-1]
		args := splitCommaArgs(argStr)
		for _, name := range args {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			features = append(features,
				resolveAlternateName(fnName, name, ffv)...)
		}
	}
	return features
}

// resolveAlternateName produces the OpenType feature(s) for one functional
// notation `fn(name)` per CSS Fonts 4 §6.6. The mapping from function name
// to OT tag is fixed; the index comes from the @font-feature-values rule
// lookup.
func resolveAlternateName(fn, name string, ffv css.FontFeatureValuesRule) []textshape.FontFeature {
	switch fn {
	case "stylistic":
		// salt <index>
		if idxs, ok := ffv.Stylistic[name]; ok && len(idxs) > 0 {
			return []textshape.FontFeature{
				{Tag: [4]byte{'s', 'a', 'l', 't'}, Value: uint32(idxs[0])},
			}
		}
	case "styleset":
		// ssXX for each index (capped at 99 per spec / ss01..ss20 are the
		// canonical set, but the CSS spec allows up to ss99).
		if idxs, ok := ffv.Styleset[name]; ok {
			out := make([]textshape.FontFeature, 0, len(idxs))
			for _, i := range idxs {
				if i < 1 || i > 99 {
					continue
				}
				tag := ssTag("ss", i)
				out = append(out, textshape.FontFeature{Tag: tag, Value: 1})
			}
			return out
		}
	case "character-variant":
		// cvXX. Index 1..99 → "cv01".."cv99". Two-value form: `name: 1 3` →
		// cv01=3 (per CSS Fonts 4 §6.6 — selects variant 3 of cv01).
		if idxs, ok := ffv.CharacterVariant[name]; ok && len(idxs) >= 1 {
			i := idxs[0]
			if i < 1 || i > 99 {
				return nil
			}
			tag := ssTag("cv", i)
			val := uint32(1)
			if len(idxs) >= 2 && idxs[1] >= 0 {
				val = uint32(idxs[1])
			}
			return []textshape.FontFeature{{Tag: tag, Value: val}}
		}
	case "swash":
		// Per spec, swash() activates BOTH swsh AND cswh at the named index.
		if idxs, ok := ffv.Swash[name]; ok && len(idxs) > 0 {
			i := uint32(idxs[0])
			return []textshape.FontFeature{
				{Tag: [4]byte{'s', 'w', 's', 'h'}, Value: i},
				{Tag: [4]byte{'c', 's', 'w', 'h'}, Value: i},
			}
		}
	case "ornaments":
		// ornm <index>
		if idxs, ok := ffv.Ornaments[name]; ok && len(idxs) > 0 {
			return []textshape.FontFeature{
				{Tag: [4]byte{'o', 'r', 'n', 'm'}, Value: uint32(idxs[0])},
			}
		}
	case "annotation":
		// nalt <index>
		if idxs, ok := ffv.Annotation[name]; ok && len(idxs) > 0 {
			return []textshape.FontFeature{
				{Tag: [4]byte{'n', 'a', 'l', 't'}, Value: uint32(idxs[0])},
			}
		}
	}
	return nil
}

// ssTag returns the 4-byte OT tag for a ssXX or cvXX feature: prefix + 2
// decimal digits zero-padded. prefix must be 2 bytes ("ss" or "cv"); i in
// 1..99.
func ssTag(prefix string, i int) [4]byte {
	var t [4]byte
	t[0] = prefix[0]
	t[1] = prefix[1]
	t[2] = byte('0' + i/10)
	t[3] = byte('0' + i%10)
	return t
}

// splitAlternatesTokens splits a font-variant-alternates value into its
// space-separated tokens, respecting parenthesized argument lists. So
// `swash(foo) styleset(bar, baz)` → ["swash(foo)", "styleset(bar, baz)"].
func splitAlternatesTokens(value string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ' ', '\t':
			if depth == 0 {
				if i > start {
					out = append(out, value[start:i])
				}
				start = i + 1
			}
		}
	}
	if start < len(value) {
		out = append(out, value[start:])
	}
	return out
}

// splitCommaArgs splits a comma-separated argument list, trimming each
// element.
func splitCommaArgs(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(a))
	}
	return out
}

// firstFontFamily returns the first comma-separated entry from a CSS
// font-family value, unquoted.
func firstFontFamily(family string) string {
	for _, f := range strings.Split(family, ",") {
		f = strings.TrimSpace(f)
		f = strings.Trim(f, `"'`)
		if f != "" {
			return f
		}
	}
	return ""
}

// parseFontFeatureSettings parses a CSS font-feature-settings value like
// `"kern" 1, "liga" 0` into a slice of FontFeature.
// CSS syntax: each entry is a 4-character tag in quotes, optionally followed by
// an integer value (default 1) or "on"/"off".
func parseFontFeatureSettings(value string) []textshape.FontFeature {
	var features []textshape.FontFeature
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "normal" {
			continue
		}
		// Split into tag and optional value.
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		// Extract tag: must be a quoted 4-character string.
		tag := fields[0]
		tag = strings.Trim(tag, `"'`)
		if len(tag) != 4 {
			continue
		}
		// Parse value: default is 1 (on).
		val := uint32(1)
		if len(fields) > 1 {
			switch strings.ToLower(fields[1]) {
			case "on":
				val = 1
			case "off":
				val = 0
			default:
				// Parse integer.
				n := 0
				for _, c := range fields[1] {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					} else {
						break
					}
				}
				val = uint32(n)
			}
		}
		features = append(features, textshape.FontFeature{
			Tag:   [4]byte{tag[0], tag[1], tag[2], tag[3]},
			Value: val,
		})
	}
	return features
}

// blockUnderDirection returns the physical direction that is "toward block-end"
// (the canonical under-side) for an upright vertical text run on this layer.
// vertical-rl: block-end = physical left  → blockUnderLeft.
// vertical-lr: block-end = physical right → blockUnderRight.
//
// The condition mirrors the three inline call sites in render.go:
// IsSidewaysRL && !IsWritingModeVerticalLR is true only for genuine vertical-rl.
// vertical-lr is represented as IsSidewaysRL=true, IsWritingModeVerticalLR=true.
func (layer *PaintLayer) blockUnderDirection() blockUnderDir {
	if layer.IsSidewaysRL && !layer.IsWritingModeVerticalLR {
		return blockUnderLeft
	}
	return blockUnderRight
}

// sortZLists sorts NegativeZ and PositiveZ by z-index (ascending). AutoZero
// is DOMIndex-sorted only when no entry is a flex item: for CSS 2.1 Appendix
// E step 6 we want tree order on z-index:auto positioned descendants, but
// flex items paint in order-modified document order per CSS Flexbox §4.3 and
// their insertion order already reflects that — don't clobber it.
func (layer *PaintLayer) sortZLists() {
	sort.SliceStable(layer.NegativeZ, func(i, j int) bool {
		return layer.NegativeZ[i].ZIndex < layer.NegativeZ[j].ZIndex
	})
	sort.SliceStable(layer.PositiveZ, func(i, j int) bool {
		return layer.PositiveZ[i].ZIndex < layer.PositiveZ[j].ZIndex
	})
	hasFlexItem := false
	for _, entry := range layer.AutoZero {
		if entry.Box != nil && entry.Box.IsFlexItem() {
			hasFlexItem = true
			break
		}
	}
	if !hasFlexItem {
		sort.SliceStable(layer.AutoZero, func(i, j int) bool {
			bi, bj := layer.AutoZero[i].Box, layer.AutoZero[j].Box
			if bi == nil || bj == nil {
				return false
			}
			return bi.DOMIndex < bj.DOMIndex
		})
	}
	for _, child := range layer.NegativeZ {
		child.sortZLists()
	}
	for _, child := range layer.AutoZero {
		child.sortZLists()
	}
	for _, child := range layer.PositiveZ {
		child.sortZLists()
	}
	for _, child := range layer.FlowChildren {
		child.sortZLists()
	}
	for _, child := range layer.FloatChildren {
		child.sortZLists()
	}
}

// computeBackfaceHidden returns true when the element's back face is currently
// facing the viewer, taking into account the chain of preserve-3d ancestors
// that share the element's 3D rendering context.
//
// Per CSS Transforms L2 §11 the back-face test consults the element's
// "accumulated transformation matrix": start with the element's own
// transform and prepend each ancestor transform whose parent uses
// `transform-style: preserve-3d`. The walk stops at the first flattening
// boundary (default `transform-style: flat`) — that ancestor's transform
// is NOT included because flattening collapses the descendant subtree
// into the ancestor's plane before the back-face check fires on the
// descendant. Mirrors Blink's `TransformPaintPropertyNode::Build`
// flattening logic at SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.
//
// `selfTransforms` already contains the element's own resolved transforms
// (with percent translates resolved against its own border-box).
func computeBackfaceHidden(box *layout.Box, selfTransforms []css.Transform) bool {
	cumulative := append([]css.Transform(nil), selfTransforms...)
	// Walk ancestors: include each ancestor's transform iff its PARENT
	// is preserve-3d (the standard 3D rendering context rule). The first
	// ancestor whose parent is flat (or which has no parent) terminates
	// the walk — its own transform IS still included because that
	// ancestor itself sits in a flat context, but its own contribution to
	// the back-face check is only relevant when the descendant shares
	// its 3D context. Concretely: walk while `ancestor.Style.GetTransformStyle()
	// == preserve-3d`; that means "descendants see my transform".
	for ancestor := box.Parent; ancestor != nil && ancestor.Style != nil; ancestor = ancestor.Parent {
		if ancestor.Style.GetTransformStyle() != css.TransformStylePreserve3D {
			break
		}
		// Compose ancestor's transform into the cumulative list (prepended
		// because parent transforms apply first). Convert the parsed CSS
		// transforms directly — percent translates on an ancestor don't
		// affect back-face math (translates touch tx/ty only).
		ancTransforms := ancestor.Style.GetTransforms()
		if len(ancTransforms) > 0 {
			cumulative = append(append([]css.Transform(nil), ancTransforms...), cumulative...)
		}
		// Also include the ancestor's individual transform properties
		// (translate / rotate / scale longhands) in canonical order.
		if deg, ok := ancestor.Style.GetIndividualRotate(); ok {
			cumulative = append([]css.Transform{{Type: "rotate", Values: []float64{deg}}}, cumulative...)
		}
		if sx, sy, ok := ancestor.Style.GetIndividualScale(); ok {
			cumulative = append([]css.Transform{{Type: "scale", Values: []float64{sx, sy}}}, cumulative...)
		}
		// translate ancestor longhand: not relevant to back-face math.
		_ = ancestor
	}
	return css.IsBackFaceVisible(cumulative)
}
