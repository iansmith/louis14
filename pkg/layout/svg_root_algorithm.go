package layout

import (
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
	"louis14/pkg/layout/svg"
)

// SVGRootAlgorithm lays out an inline <svg> element. Mirrors Blink's
// LayoutSVGRoot::LayoutRoot pattern: the <svg> is a CSS replaced-element
// box to its HTML parent (sized via getInlineSVGIntrinsicInfo +
// ComputeReplacedSize, exactly as before), but it owns an SVG subtree
// internally — viewBox / preserveAspectRatio / localToBorderBoxTransform
// plus the SVG layout nodes that paint inside it.
//
// Phase 1 scope: outer sizing is delegated to BlockLayoutAlgorithm so the
// HTML-flow position and size of the <svg> box stay byte-identical to the
// pre-Phase-1 behavior. The SVGRoot is built and stashed on the
// LayoutInputNode where the painter can reach it via Box.LayoutNode.
// Phase 2 fills in shape paint and wires the painter; Phase 1 SVG content
// still renders blank.
type SVGRootAlgorithm struct {
	ctx   *LayoutContext
	node  *LayoutInputNode
	space ConstraintSpace
}

// NewSVGRootAlgorithm constructs the algorithm. Callers should check
// IsInlineSVG(node) before invoking.
func NewSVGRootAlgorithm(ctx *LayoutContext, node *LayoutInputNode, space ConstraintSpace) *SVGRootAlgorithm {
	return &SVGRootAlgorithm{ctx: ctx, node: node, space: space}
}

// Layout produces the LayoutResult for the <svg> box. The result is
// byte-identical to what BlockLayoutAlgorithm would produce for the same
// <svg> node — outer sizing flows through the same ComputeReplacedSize
// path. The SVG subtree is attached as a side-channel on the
// LayoutInputNode.
func (sra *SVGRootAlgorithm) Layout() *LayoutResult {
	// Step 1: outer sizing via the standard block path. <svg> is a
	// replaced element (IsReplacedElement returns true); the block
	// algorithm's replaced-element branch already calls
	// ComputeReplacedSize via getInlineSVGIntrinsicInfo. This gives us a
	// LayoutResult with the correct CSS-box geometry, byte-identical to
	// the pre-Phase-1 behavior.
	result := NewBlockLayoutAlgorithm(sra.ctx, sra.node, sra.space).Layout()
	if result == nil || result.Fragment == nil {
		return result
	}

	// Step 2: derive the SVG content-box physical size from the result.
	// fragment.Size is the border-box size; subtract border + padding to
	// get the content-box, which is the SVG viewport. We read these from
	// the BoxData populated by BlockLayoutAlgorithm.
	frag := result.Fragment
	borderBoxW := frag.Size.WidthF64()
	borderBoxH := frag.Size.HeightF64()
	var borderPaddingX, borderPaddingY float64
	if frag.BoxData != nil {
		borderPaddingX = frag.BoxData.Border.Left + frag.BoxData.Border.Right +
			frag.BoxData.Padding.Left + frag.BoxData.Padding.Right
		borderPaddingY = frag.BoxData.Border.Top + frag.BoxData.Border.Bottom +
			frag.BoxData.Padding.Top + frag.BoxData.Padding.Bottom
	}
	contentW := borderBoxW - borderPaddingX
	if contentW < 0 {
		contentW = 0
	}
	contentH := borderBoxH - borderPaddingY
	if contentH < 0 {
		contentH = 0
	}
	containerSize := geometry.SizeF{Width: contentW, Height: contentH}

	// Step 3: build the SVG root + subtree. The style resolver gives
	// each SVGShape its computed style by looking up the cascade map
	// supplied on LayoutContext (LayoutEngine.Layout populates this
	// with the same per-node styles ApplyStylesToDocument produced).
	styles := sra.ctx.ComputedStyles
	styleResolver := func(elt svg.ElementAdapter) *css.Style {
		if styles == nil {
			return nil
		}
		if adapter, ok := elt.(svgElementAdapter); ok && adapter.node != nil {
			return styles[adapter.node]
		}
		return nil
	}
	root := svg.BuildSVGRoot(newSVGElementAdapter(sra.node.DOMNode), containerSize, styleResolver)

	// Step 4: run the SVG layout pass. Phase 1: this is a recursive
	// no-op (every node returns an empty SVGLayoutResult), but wiring
	// the call now makes Phase 2's geometry consumers a pure addition.
	root.UpdateSVGLayout(svg.SVGLayoutInfo{ForceLayout: true})

	// Step 5: stash the SVGRoot on the LayoutInputNode for the painter.
	sra.node.SVGRoot = root

	return result
}

// IsInlineSVG reports whether a layout node is an outermost inline
// <svg> element — the trigger for SVGRootAlgorithm dispatch in
// layoutElement. Nested <svg> elements (an <svg> with an SVG ancestor)
// are NOT outermost — they live inside the parent SVG's subtree as
// SVGViewportContainers and must not become their own SVGRoot or CSS
// replaced-element box. Mirrors Blink's
// LayoutSVGRoot::IsLayoutSVGRootForSVGElement check, which only flags
// the root <svg> as a CSS box; nested <svg>'s LayoutObject is a
// LayoutSVGViewportContainer.
func IsInlineSVG(node *LayoutInputNode) bool {
	if node == nil || node.DOMNode == nil || node.DOMNode.TagName != "svg" {
		return false
	}
	// Walk up the DOM looking for an SVG ancestor. If we find one,
	// this <svg> is nested and is handled by the outer SVG's
	// SVGViewportContainer path.
	for p := node.DOMNode.Parent; p != nil; p = p.Parent {
		if p.TagName == "svg" {
			return false
		}
	}
	return true
}

// svgElementAdapter implements svg.ElementAdapter over an html.Node,
// bridging the layout/svg package's abstract DOM view to louis14's
// concrete DOM type without creating an import cycle. The adapter is
// stateless — every call walks the underlying DOM directly.
type svgElementAdapter struct {
	node *html.Node
}

func newSVGElementAdapter(n *html.Node) svg.ElementAdapter {
	if n == nil {
		return nil
	}
	return svgElementAdapter{node: n}
}

func (a svgElementAdapter) TagName() string {
	return a.node.TagName
}

func (a svgElementAdapter) Attribute(name string) (string, bool) {
	return a.node.GetAttribute(name)
}

func (a svgElementAdapter) SVGChildren() []svg.ElementAdapter {
	if a.node == nil {
		return nil
	}
	var out []svg.ElementAdapter
	for _, c := range a.node.Children {
		if c == nil || c.Type != html.ElementNode {
			continue
		}
		out = append(out, svgElementAdapter{node: c})
	}
	return out
}

// TextContent implements svg.ElementAdapter. Concatenates this
// element's own direct TextNode children (e.g. `<text>Text to
// select</text>`'s single text-node child) — SVGChildren above
// filters text nodes out entirely, so `<text>` needs this separate
// accessor to see them. Deep-descendant text (e.g. a nested `<tspan>`)
// is intentionally NOT walked: `<tspan>` is out of this ticket's scope
// (LOU-345), and none of the 5 target reftests nest anything inside
// `<text>`.
func (a svgElementAdapter) TextContent() string {
	if a.node == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range a.node.Children {
		if c != nil && c.Type == html.TextNode {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// SVGElementDOMNode returns the *html.Node backing elt (the ELEMENT
// node itself, e.g. the `<text>` element — not a text-node child) when
// elt is the real DOM-backed svg.ElementAdapter (svgElementAdapter), or
// nil for any other implementation (e.g. the external-SVG-document
// adapter, or a test fake). Exported so pkg/render's SVG text painter
// (LOU-345) can find an SVGText's originating element for ::selection
// resolution (ComputePseudoElementStyle matches against the element,
// same as drawText's box.Node).
//
// DELIBERATE TRADEOFF (flagged per this ticket's plan rather than
// decided silently): this is a type-assertion against the unexported
// svgElementAdapter, not a new svg.ElementAdapter interface method —
// the ticket's pre-approval covers exactly ONE new interface method,
// already spent on TextContent() (svg_root.go). A cleaner alternative
// would add a second method (e.g. `DOMNode() *html.Node`, nil-returning
// on externalSVGElement/test fakes) so a FUTURE third ElementAdapter
// implementer fails to compile here instead of silently degrading to
// nil — but that would be a second interface addition beyond what's
// pre-approved. Revisit as a real interface method if a third
// DOM-backed adapter is ever added (today only svgElementAdapter wraps
// a real *html.Node; externalSVGElement's tree has no DOM node at all,
// so its nil return is correct, not degraded, either way).
func SVGElementDOMNode(elt svg.ElementAdapter) *html.Node {
	if adapter, ok := elt.(svgElementAdapter); ok {
		return adapter.node
	}
	return nil
}
