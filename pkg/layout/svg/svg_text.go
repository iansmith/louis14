package svg

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// SVGText is the layout node for an SVG `<text>` element. Mirrors
// Blink's LayoutSVGText (core/layout/svg/layout_svg_text.h @ blob
// f53f8184c42dcf29c57c84f832fd84773d38b9cf), scoped down to what
// LOU-345's 5 target reftests need: a single line of direct
// text-node content positioned at the element's `x`/`y` attributes,
// left-to-right, `text-anchor: start` (the SVG default). No
// `<tspan>`, no `textPath`, no per-character x/y position lists — see
// the ticket plan's explicit scope cut.
//
// Like SVGShape, SVGText owns geometry only; pkg/render/svg_text_painter.go
// holds the DrawContext and does the actual glyph rasterization
// (mirrors Blink's LayoutSVGText / SVGTextPainter split).
type SVGText struct {
	// TagName is always "text" — kept for parity with SVGShape/
	// SVGContainer's TagName field (debug logging, painter dispatch
	// symmetry).
	TagName string

	// Element holds a reference to the original SVG element adapter,
	// same role as SVGShape.Element: the painter can read presentation
	// attributes / resolve style through it at paint time.
	Element ElementAdapter

	// Style is the resolved computed style for the element's DOM node,
	// attached by the tree builder via the StyleResolver callback —
	// same wiring as SVGShape.Style. Carries the folded `fill`/`stroke`/
	// `text-shadow`/etc. presentation attributes and any cascaded CSS
	// (pkg/css/cascade.go's applySVGPresentationalAttributes already
	// applies generically to every element, `<text>` included).
	Style *css.Style

	// X, Y are the resolved `x`/`y` attributes in user units — the text
	// baseline's start point per SVG 1.1 §10.5. Default 0 when absent.
	X, Y float64

	// Text is the element's own direct text-node content (SVG 1.1
	// §10.5's "character data content"). Deep-descendant content
	// (nested `<tspan>`) is out of scope — see this type's doc comment.
	Text string
}

// NewSVGText builds an SVGText from a `<text>` ElementAdapter,
// resolving x/y against the supplied SVGLengthContext. Mirrors
// SVGShape's NewSVGShape construction pattern: read geometry
// attributes, resolve lengths, done — no painting here.
//
// Returns nil when elt has any SVG-element children (e.g. `<tspan>`)
// — squarely the "no `<tspan>` support" scope cut this type's doc
// comment describes. The caller (BuildSVGTree/buildSVGTreeWithResources)
// falls back to the pre-existing SVGContainer path for a nil return,
// exactly as it already does for any other unrecognized-shape tag.
// Without this guard, a `<text>` with BOTH a `<tspan>` child AND its
// own trailing direct text (e.g. `<text><tspan>A</tspan> B</text>`)
// would silently drop the `<tspan>`'s content and render only " B" —
// a worse, silently-wrong partial render, not merely an unsupported
// no-op. Falling back to SVGContainer instead reproduces this ticket's
// pre-existing (contentless, but at least not WRONG) behavior for
// exactly the same "nothing renders" outcome text-decoration-
// propagation-display-contents.html's reference already relies on
// (both test and reference used to render blank there; a naive fix
// made the test side render partial garbage instead, a regression
// caught by this ticket's own no-regression sweep, not by a target
// test).
func NewSVGText(elt ElementAdapter, lengthCtx SVGLengthContext) *SVGText {
	if elt == nil {
		return nil
	}
	if len(elt.SVGChildren()) > 0 {
		return nil
	}
	return &SVGText{
		TagName: elt.TagName(),
		Element: elt,
		X:       getLength(elt, "x", LengthModeWidth, lengthCtx, 0),
		Y:       getLength(elt, "y", LengthModeHeight, lengthCtx, 0),
		Text:    elt.TextContent(),
	}
}

// ObjectBoundingBox returns an empty box. Phase scope: SVGText doesn't
// yet measure its own glyph extents at layout time (the painter
// measures at paint time via the font's actual metrics, same
// division of labor as Blink's LayoutSVGText, which computes its
// bounding box from InlineItems the painter also consumes). Nothing
// in the 5 target reftests reads an SVGText's bounding box — no
// gradient/pattern/mask/clip is applied to `<text>` in-scope — so this
// is a safe placeholder, exactly like SVGContainer's pre-Phase-3 bbox.
func (t *SVGText) ObjectBoundingBox() geometry.RectF {
	return geometry.RectF{}
}

// LocalTransform returns identity. `<text>` doesn't carry its own SVG
// `transform` in the 5 target reftests; a future phase can add
// transform-attribute parsing the same way SVGContainer does if a
// broader-corpus test needs it.
func (t *SVGText) LocalTransform() geometry.AffineTransform {
	return geometry.Identity()
}

// UpdateSVGLayout is a no-op, mirroring SVGShape's Phase-2-era
// UpdateSVGLayout: geometry was resolved at construction time and
// louis14 doesn't yet incrementally invalidate SVG geometry.
func (t *SVGText) UpdateSVGLayout(info SVGLayoutInfo) SVGLayoutResult {
	return SVGLayoutResult{}
}

// Paint is a no-op — pkg/render/svg_text_painter.go drives the actual
// glyph rasterization, same split as SVGShape.Paint.
func (t *SVGText) Paint(ctx *SVGPaintContext) {
	// Intentionally empty; see doc comment above.
}
