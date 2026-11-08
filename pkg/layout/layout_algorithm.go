package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/geometry"
	"louis14/pkg/html"
	"louis14/pkg/images"
	"louis14/pkg/layout/svg"
	"louis14/pkg/text"
)

// LayoutAlgorithm is the interface for all formatting context algorithms.
// Each display type (block, flex, table, grid) implements this interface.
//
// Ported from Blink's LayoutAlgorithm pattern.
type LayoutAlgorithm interface {
	// Layout performs layout and returns the result.
	Layout() *LayoutResult
}

// DocumentFetcher returns HTML content for a given URI. Used to load
// nested documents for iframe/object elements during layout.
type DocumentFetcher func(uri string) (string, error)

// LayoutContext carries shared state needed by all layout algorithms.
// Algorithms receive this context rather than being methods on a god object.
// Style access is through LayoutInputNode.Style(), not a map lookup.
type LayoutContext struct {
	// Viewport dimensions for resolving viewport-relative units.
	ViewportWidth  float64
	ViewportHeight float64

	// BaseDir is the document's base, the analog of Document::BaseURL
	// (third_party/blink/renderer/core/dom/document.cc:5083 @
	// d4ecdfed88f962439247c2ad36b8fe47805b1520); ApplyStylesToDocument
	// stamps the same value on each computed Style.BaseDir. Relative URLs
	// are completed against it where they are used, as Document::CompleteURL
	// (document.cc:7553) does: <img src> against Style.BaseDir
	// (getImgIntrinsicInfo) and iframe/object src against it
	// (frameOwnerSource), before they reach the document-agnostic
	// ImageFetcher and DocumentFetcher. Empty for a top-level document.
	BaseDir string

	// ImageFetcher for loading images during layout (intrinsic sizing).
	ImageFetcher images.ImageFetcher

	// SVGRootContainerSize is the container size an SVG image's document is
	// laid out in: the document-element <svg> takes exactly this size
	// (LayoutSVGRoot::SetContainerSize, layout_svg_root.h:84-95, read by
	// ComputeReplacedSize, length_utils.cc:1238-1249 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Blink keeps it on the
	// LayoutSVGRoot; louis14 rebuilds layout objects per pass, so it rides on
	// the context. Empty outside an SVG image.
	SVGRootContainerSize geometry.PhysicalSize

	// DocumentFetcher for loading nested documents (iframe, object).
	DocumentFetcher DocumentFetcher

	// FontConfig provides font paths for text measurement, including @font-face fonts.
	// If zero-value, DefaultFontConfig() is used.
	FontConfig text.FontConfig

	// orthogonalInProgress is the set of orthogonal flow roots whose
	// contribution is being measured (orthogonalFlowRootContribution), to
	// detect a cycle.
	orthogonalInProgress map[*LayoutInputNode]bool

	// orthogonalContributions caches orthogonal flow roots' min/max
	// contributions, so a min-content and a max-content pass share one
	// layout.
	orthogonalContributions map[*LayoutInputNode]orthogonalContribution

	// layoutSideEffectsDisabled counts the open DisableLayoutSideEffects
	// scopes (layout_box.go).
	layoutSideEffectsDisabled int

	// ComputedStyles is the per-DOM-node style map produced by
	// css.ApplyStylesToDocument and stored on LayoutEngine. SVG
	// descendants of an inline <svg> are not LayoutInputNodes (they
	// live under the SVGRoot subtree, not under the CSS box tree),
	// so the SVGRootAlgorithm needs this map to attach computed
	// style to each SVGShape at build time. Populated by
	// LayoutEngine.Layout before laying out the document root.
	// Nil-tolerant: SVG descendants without a style entry fall back
	// to SVG property defaults.
	ComputedStyles map[*html.Node]*css.Style

	// SVGRoots maps each outermost <svg> laid out in this document to its
	// latest SVGRoot (recorded by SVGRootAlgorithm), for the document's id
	// map (treeScopeSVGResources).
	SVGRoots map[*html.Node]*svg.SVGRoot
}

// orthogonalContribution is an orthogonal flow root's min/max contribution
// (min equals max), for the parent-space inputs in space
// (orthogonalFlowRootContribution).
type orthogonalContribution struct {
	space        orthogonalContributionSpace
	contribution float64
}

// orthogonalContributionSpace is the parent-space inputs an orthogonal flow
// root's contribution depends on: its available inline size, the
// orthogonal fallback sizes, the parent's percentage block size (its
// percentage inline size), and the percentage resolution inline size.
type orthogonalContributionSpace [5]float64

// IsOrthogonalComputing reports whether an orthogonal flow root's
// contribution is being measured: measuring it again is a cycle.
func (ctx *LayoutContext) IsOrthogonalComputing(node *LayoutInputNode) bool {
	return ctx.orthogonalInProgress[node]
}

// SetOrthogonalComputing marks an orthogonal flow root as being measured,
// or (false) no longer.
func (ctx *LayoutContext) SetOrthogonalComputing(node *LayoutInputNode, computing bool) {
	if !computing {
		delete(ctx.orthogonalInProgress, node)
		return
	}
	if ctx.orthogonalInProgress == nil {
		ctx.orthogonalInProgress = make(map[*LayoutInputNode]bool)
	}
	ctx.orthogonalInProgress[node] = true
}
