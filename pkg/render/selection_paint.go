package render

// selection_paint.go implements ::selection highlight painting (LOU-344).
// Conceptually mirrors Blink's HighlightPainter / SelectionPaintState
// (third_party/blink/renderer/core/paint/highlight_painter.h @ blob
// 96737c206898cbd27aed2cbeb07c455a3fa3d2dd), simplified to a single
// highlight type (no multi-layer PaintCase dispatch — see LOU-344's
// "Theory of root cause" for why that scope cut is deliberate).
//
// Architecture note: pkg/render previously had NO access to the DOM
// Selection or to stylesheets (it only consumed the pre-built Box tree —
// see Renderer.SetSelectionContext's doc comment). Rather than threading
// selection state through pkg/layout's fragment-emission machinery
// (inline_layout.go's emitTextFragment, deep inside per-line text
// shaping), this resolves the selected sub-range of each text fragment
// AT PAINT TIME using only data already on Box/PaintLayer: box.Node (the
// fragment's originating ELEMENT — see fragmentToBox/emitTextFragment,
// which set frag.Node to the parent element, not the DOM text node) and
// box.Text (the fragment's own rendered string). selectionTextConsumed
// recovers the node-local offset a fragment covers by tracking, per DOM
// text node, how many code units have already been consumed by earlier
// fragments painted in this Render call — DOM text nodes are walked by
// drawText in paint order, which is document order for the common case
// (no bidi reordering across fragment boundaries within one node).

import (
	"sort"
	"strings"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// selectionBackgroundRectY returns the (Y, Height) the ::selection
// background rect should use for a text fragment box under a HORIZONTAL
// writing mode. Thin wrapper over selectionBackgroundRectCrossAxis (see
// that function's doc comment for the shared fallback logic and rationale)
// — kept as its own entry point since it predates LOU-347's vertical-mode
// generalization and is covered by its own direct tests.
func selectionBackgroundRectY(textBox *layout.Box) (y, height float64) {
	return selectionBackgroundRectCrossAxis(textBox, false)
}

// selectionBackgroundRectCrossAxis returns the (origin, extent) the
// ::selection background rect should use along the CROSS axis: the axis a
// highlighted segment's rect does NOT grow along as more characters are
// selected — physical Y/Height for horizontal text, physical X/Width for
// vertical text (pkg/layout/writing_mode_converter.go's ToPhysicalSize
// swaps logical inline-size into physical Height for every vertical writing
// mode, so the cross axis swaps to X/Width there — see drawText's LOU-347
// vertical dispatch). vertical selects which physical axis pair to read;
// selectionBackgroundRectY (the original LOU-344 shape) is the
// vertical=false case.
//
// The origin/extent selection logic (prefer the originating element's own
// inline background-fragment sibling; else a line-height:normal block
// ancestor; else textBox's own box) is IDENTICAL between the two axes —
// only which struct fields are read differs:
//
// pkg/layout/inline_layout.go emits an inline element's own background as
// a SEPARATE sibling PhysicalFragment from its text fragments (see that
// file's "An inline background covers at least the font's em box... and
// grows to the line box when line-height exceeds it — max(em box, line
// box)" comment), sharing textBox.Parent but carrying no .Text. A text
// fragment's own cross-axis extent is ALWAYS just the font's em box
// (fontSize, per emitTextFragment), never the line box — so when
// line-height > font-size (line-height: normal's 1.2x default, the common
// case), the background fragment is larger than the text fragment and
// anchored earlier (line-box start vs. the text's baseline-anchored em
// box). Using the text fragment's own cross-axis origin/extent for the
// selection rect then leaves a gap at the line-box start where the
// originating element's own background (e.g. `background-color: red`)
// shows through above the selection highlight
// (selection-contenteditable-011.html's ~3px red sliver, LOU-344). Reusing
// the SAME background-fragment box the originating element's own
// background already painted with (rather than re-deriving the max(em box,
// line box) sizing independently at paint time, which would duplicate
// pkg/layout's logic and risk drifting out of sync with it) keeps the two
// rects pixel-identical by construction.
//
// Falling back further to a BLOCK-level ancestor box sharing textBox.Node
// (rather than textBox's own em-box) fixes selection-over-highlight-001
// .html's 1px top sliver (a plain block <div>, `line-height: normal`, no
// own background-color — the highlight background is the ONLY thing
// painted for that cross-axis range, so it must cover the full
// UA-computed line-box leading, not just the em-box). But applying it
// UNCONDITIONALLY over-extends the highlighted background 2-3px past the
// text when the author sets an EXPLICIT numeric line-height (e.g.
// `line-height: 1` via support/highlights.css's .highlight_reftest, used
// by active-selection-014/025/027.html): geometry dump confirmed
// pkg/layout's block box is larger than its own text's em-box even under
// `line-height: 1` (active-selection-014.html's 300%-sized div: block box
// H=50.83 vs text em-box H=48) — a pre-existing pkg/layout
// line-height/block-height computation gap, unrelated to highlight
// painting, that the block-ancestor fallback would otherwise propagate
// into the highlight rect.
//
// KNOWN LIMITATION, not a permanent design: gating on IsLineHeightNormal()
// (only extend to the block ancestor when line-height is the UA default,
// not an author override) is a paint-layer workaround for a layout-layer
// bug, not a real semantic distinction Blink makes — it happens to
// separate the two known cases correctly today, but will silently become
// an unnecessary (and possibly wrong) restriction the day pkg/layout's
// block-height-under-explicit-line-height computation is fixed to agree
// with its own text's em-box + leading. When that pkg/layout fix lands,
// this gate should be removed and the block-ancestor fallback applied
// unconditionally, same as the sibling-background-fragment case above it.
func selectionBackgroundRectCrossAxis(textBox *layout.Box, vertical bool) (origin, extent float64) {
	if textBox == nil {
		return 0, 0
	}
	if textBox.Parent != nil && textBox.Node != nil {
		for _, sibling := range textBox.Parent.Children {
			if sibling != textBox && sibling.Node == textBox.Node && sibling.Text == "" {
				return crossAxisOriginExtent(sibling, vertical)
			}
		}
		if ancestor := textBox.Parent.Parent; ancestor != nil && ancestor.Node == textBox.Node && ancestor.Text == "" &&
			textBox.Style != nil && textBox.Style.IsLineHeightNormal() {
			return crossAxisOriginExtent(ancestor, vertical)
		}
	}
	return crossAxisOriginExtent(textBox, vertical)
}

// crossAxisOriginExtent reads a box's cross-axis (origin, extent) pair:
// (X, Width) under a vertical writing mode, (Y, Height) otherwise. A plain
// top-level function rather than a closure over `vertical` so
// selectionBackgroundRectCrossAxis's 3 call sites don't allocate a closure
// per invocation in this per-highlighted-segment paint-time path.
func crossAxisOriginExtent(b *layout.Box, vertical bool) (float64, float64) {
	if vertical {
		return b.X, b.Width
	}
	return b.Y, b.Height
}

// selectionOverlapForTextNode returns the [start,end) sub-range of a text
// fragment — expressed in FRAGMENT-LOCAL offsets (i.e. already shifted so
// 0 means the start of THIS fragment's own text, not the node's) — that
// falls inside rng, given the fragment covers node-local offsets
// [fragStart, fragEnd) of node's text content. ok is false when rng is nil
// or doesn't touch this node at all, or the computed range is empty.
//
// Mirrors the DOM Range "partially contained node" containment check
// (https://dom.spec.whatwg.org/#concept-range-partially-contained), with
// node-local offsets just used directly since both StartContainer and
// EndContainer here are always the same text node (the simplified single-
// Range model this engine supports — see html.Range's doc comment).
func selectionOverlapForTextNode(rng *html.Range, node *html.Node, fragStart, fragEnd int) (start, end int, ok bool) {
	if rng == nil || node == nil {
		return 0, 0, false
	}

	// nodeSelStart/nodeSelEnd: the selected range expressed in node-local
	// offsets, clamped to [0, len(node.Text)]. A boundary container that
	// is an ancestor ELEMENT (e.g. selectNodeContents(div) where div has
	// one text-node child) selects the text node fully when the text
	// node's index-among-siblings falls within [StartOffset, EndOffset) —
	// the common case in every LOU-344 target test is "select all of the
	// element's children" via selectNodeContents, so the text node is
	// either fully in or fully out for an element-container boundary.
	nodeSelStart, hasStart, startExcluded := nodeLocalBoundary(rng.StartContainer, rng.StartOffset, node, true)
	nodeSelEnd, hasEnd, endExcluded := nodeLocalBoundary(rng.EndContainer, rng.EndOffset, node, false)
	if startExcluded || endExcluded {
		// One side IS related to node's ancestry chain but explicitly
		// excludes it (e.g. a collapsed (div,0)-(div,0) range, where the
		// end boundary's child-index check places node's subtree entirely
		// AFTER the boundary) — this is a hard "not selected" verdict for
		// node, not "this boundary doesn't constrain node" (which would
		// fall through to the permissive 0/len(node.Text) default below).
		return 0, 0, false
	}
	if !hasStart && !hasEnd {
		return 0, 0, false
	}
	if !hasStart {
		nodeSelStart = 0
	}
	if !hasEnd {
		nodeSelEnd = len(node.Text)
	}
	nodeSelStart = max(nodeSelStart, 0)
	nodeSelEnd = min(nodeSelEnd, len(node.Text))
	if nodeSelStart >= nodeSelEnd {
		return 0, 0, false
	}

	// Intersect [nodeSelStart, nodeSelEnd) with this fragment's own
	// [fragStart, fragEnd) node-local span, then shift to fragment-local.
	overlapStart := max(nodeSelStart, fragStart)
	overlapEnd := min(nodeSelEnd, fragEnd)
	if overlapStart >= overlapEnd {
		return 0, 0, false
	}
	return overlapStart - fragStart, overlapEnd - fragStart, true
}

// nodeLocalBoundary resolves a single DOM Range boundary point (container,
// offset) against a specific text node, returning the node-local character
// offset that boundary implies for that node. The two bools distinguish
// three outcomes the caller (selectionOverlapForTextNode) must NOT
// conflate:
//   - ok=true:                the boundary constrains node to the returned offset.
//   - ok=false, excluded=false: the boundary is UNRELATED to node (its
//     container isn't node or an ancestor of node) — doesn't constrain
//     node's selected range at all; the caller may default-fill this side.
//   - ok=false, excluded=true:  the boundary IS related to node's ancestry
//     but explicitly places node's subtree entirely outside the selected
//     range (e.g. a collapsed (div,0)-(div,0) range, where node's subtree
//     starts at-or-after the end boundary) — node is NOT selected, full
//     stop; default-filling this side as if unconstrained would wrongly
//     select all of node.
//
// isStart selects which extreme to assume when container is an ancestor
// ELEMENT of node (at any depth, not just the direct parent — e.g.
// selectNodeContents(div) where node is several levels deep inside a
// nested <span>, active-selection-018.html) rather than node itself: per
// DOM Range semantics, a child-index offset into an element container
// selects whole child subtrees [offset_start, offset_end). Walks UP from
// node to find the ancestor-or-self whose direct parent is container, then
// applies the same child-index containment check against THAT ancestor's
// position among container's children — node is included (fully, from 0 /
// to its full length) exactly when that ancestor subtree is.
func nodeLocalBoundary(container *html.Node, offset int, node *html.Node, isStart bool) (off int, ok bool, excluded bool) {
	if container == nil {
		return 0, false, false
	}
	if container == node {
		// offset is a UTF-16 code-unit offset (html.Range's doc comment);
		// node.Text is a Go UTF-8 string, so it must be converted before
		// use as a byte index — see html.UTF16OffsetToByteOffset's doc
		// comment.
		return html.UTF16OffsetToByteOffset(node.Text, offset), true, false
	}
	// Walk up from node looking for the ancestor-or-self whose parent is
	// container — that ancestor is the whole subtree the child-index
	// offset selects or excludes.
	ancestor := node
	for ancestor != nil && ancestor.Parent != container {
		ancestor = ancestor.Parent
	}
	if ancestor == nil {
		return 0, false, false // container is not an ancestor of node at all
	}
	idx := -1
	for i, c := range container.Children {
		if c == ancestor {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, false, false
	}
	if isStart {
		if offset <= idx {
			return 0, true, false // node's containing subtree starts at-or-after the start boundary: fully included from 0
		}
		return 0, false, true // start boundary is past this subtree: node excluded
	}
	if offset > idx {
		return len(node.Text), true, false // node's containing subtree ends at-or-before the end boundary: fully included to the end
	}
	return 0, false, true // end boundary is at-or-before this subtree: node excluded
}

// highlightSpan is a [start,end) fragment-local byte range that should be
// painted with highlight colors — the target-text analog of the single
// (selStart, selEnd) pair selectionOverlapForTextNode returns, generalized
// to a slice since ::target-text (unlike ::selection) can have multiple
// simultaneous, possibly-overlapping match ranges (LOU-349).
type highlightSpan struct {
	start, end int
}

// targetTextOverlapsForTextNode computes the fragment-local highlight spans
// for node given ALL of the document's current ::target-text match ranges,
// merging overlapping/adjacent spans into one continuous span (
// target-text-009.html's "match me" + "me and me" overlap at "me" and must
// paint as a single uninterrupted band, not two abutting rects with a
// double-painted seam). Reuses selectionOverlapForTextNode per range
// (LOU-349's ticket: prefer the smallest change over generalizing
// nodeLocalBoundary itself — see text_fragment.go's
// FindTextFragmentMatches doc comment for why cross-text-node matches are
// already decomposed into one same-node Range per spanned node before
// reaching here, so every range passed in is guaranteed StartContainer ==
// EndContainer == some single text node and nodeLocalBoundary's existing
// same-node fast path applies directly).
func targetTextOverlapsForTextNode(ranges []*html.Range, node *html.Node, fragStart, fragEnd int) []highlightSpan {
	if len(ranges) == 0 {
		return nil
	}
	var spans []highlightSpan
	for _, rng := range ranges {
		start, end, ok := selectionOverlapForTextNode(rng, node, fragStart, fragEnd)
		if !ok {
			continue
		}
		spans = append(spans, highlightSpan{start: start, end: end})
	}
	return mergeHighlightSpans(spans)
}

// mergeHighlightSpans sorts spans by start offset and merges any that
// overlap or touch (span A's end >= span B's start) into one continuous
// span. Standard "merge intervals" pass — kept as its own function (rather
// than inlined in targetTextOverlapsForTextNode) since the merge logic is
// independent of how the input spans were derived.
func mergeHighlightSpans(spans []highlightSpan) []highlightSpan {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := []highlightSpan{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			last.end = max(last.end, s.end)
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

// resolveSelectionPseudoStyle returns the computed ::selection style for
// originating (the originating element of a text fragment), or nil if no
// selection context is configured or originating is nil. Memoized per
// Render call in r.selectionStyleCache since multiple fragments commonly
// share one originating element (line-wrapped runs of the same <p>).
func (r *Renderer) resolveSelectionPseudoStyle(originating *html.Node) *css.Style {
	if r.selectionStyleCache == nil {
		r.selectionStyleCache = make(map[*html.Node]*css.Style)
	}
	return r.resolveHighlightPseudoStyle(originating, "selection", r.selectionStyleCache)
}

// resolveTargetTextPseudoStyle returns the computed ::target-text style for
// originating, or nil if originating is nil. Mirrors
// resolveSelectionPseudoStyle exactly (LOU-349) — memoized in its own
// r.targetTextStyleCache (kept separate from selectionStyleCache since one
// originating element can carry both an author ::selection AND
// ::target-text rule simultaneously, two independent style values that
// must not collide on a shared cache key).
func (r *Renderer) resolveTargetTextPseudoStyle(originating *html.Node) *css.Style {
	if r.targetTextStyleCache == nil {
		r.targetTextStyleCache = make(map[*html.Node]*css.Style)
	}
	return r.resolveHighlightPseudoStyle(originating, "target-text", r.targetTextStyleCache)
}

// resolveCustomHighlightPseudoStyle returns the computed ::highlight(name)
// style for originating, or nil if originating is nil (LOU-354). Mirrors
// resolveSelectionPseudoStyle/resolveTargetTextPseudoStyle, but memoized in
// a per-NAME cache (r.customHighlightStyleCaches[name]) rather than one
// shared map — see that field's doc comment in render.go for why one
// originating element can need independent styles for multiple
// simultaneously-registered highlight names (highlight-painting-
// currentcolor-001.html's ::highlight(a) and ::highlight(b)).
func (r *Renderer) resolveCustomHighlightPseudoStyle(originating *html.Node, name string) *css.Style {
	if r.customHighlightStyleCaches == nil {
		r.customHighlightStyleCaches = make(map[string]map[*html.Node]*css.Style)
	}
	cache, ok := r.customHighlightStyleCaches[name]
	if !ok {
		cache = make(map[*html.Node]*css.Style)
		r.customHighlightStyleCaches[name] = cache
	}
	return r.resolveHighlightPseudoStyle(originating, "highlight("+name+")", cache)
}

// resolveHighlightPseudoStyle is the shared implementation behind
// resolveSelectionPseudoStyle/resolveTargetTextPseudoStyle/
// resolveCustomHighlightPseudoStyle, parameterized
// by pseudo-element name and which per-Render-call cache to memoize into —
// factored out when LOU-349 added the ::target-text variant with an
// otherwise byte-identical body (CLAUDE.md §4: 2+ near-identical paths are
// dedupe-in-scope).
//
// LOU-352: resolution follows CSS Pseudo-4's highlight cascade — the chain
// walk in resolveHighlightChainStyle, mirroring Blink's per-element
// StyleHighlightData tree. When NO rule for this highlight matches
// originating or any of its ancestors, the chain style is nil (Blink:
// HighlightStyleUtils::HighlightPseudoStyle returns nullptr and paint falls
// back to DefaultHighlightColor — highlight_style_utils.cc:295-298 @
// Chromium main 72259ecfb56eacf259e21783dd2b31608ac951a3); louis14 instead
// bakes those UA defaults into a per-node resolved style here, via the
// plain ComputePseudoElementStyle fallback (its applySelectionCascade/
// applyTargetTextCascade stamp the UA pair when authorSet is empty), which
// is also why this is deliberately NOT gated on
// len(r.selectionStylesheets) == 0: a page with no stylesheets at all must
// still resolve the UA default highlight colors when this highlight is
// active.
func (r *Renderer) resolveHighlightPseudoStyle(originating *html.Node, pseudoElement string, cache map[*html.Node]*css.Style) *css.Style {
	if originating == nil {
		return nil
	}
	if cached, ok := cache[originating]; ok {
		return cached
	}
	style := r.resolveHighlightChainStyle(originating, pseudoElement)
	if style == nil {
		style = css.ComputePseudoElementStyle(originating, pseudoElement, r.selectionStylesheets, r.selectionViewportW, r.selectionViewportH, r.originatingParentStyles(originating)...)
	}
	cache[originating] = style
	return style
}

// resolveHighlightChainStyle resolves one node's position in the highlight
// inheritance chain for pseudoElement (CSS Pseudo-4 §highlight-cascade),
// memoized per pseudo-element name in r.highlightChainStyleCaches. Mirrors
// Blink's Element::RecalcHighlightStyles (core/dom/element.cc:7091-7195 @
// 72259ecf) walking the inherited ComputedStyle::HighlightData tree
// (computed_style_extra_fields.json5:540-548, `inherited: true`):
//
//   - a node NO rule for this highlight matches simply shares its parent
//     element's chain style (possibly nil) — Blink's inherited HighlightData
//     with no per-element recalc;
//   - a node with matching rules computes its own style with the parent's
//     chain style as the inheritance base for ALL properties
//     (ComputeHighlightPseudoElementStyle — Blink's
//     StyleForHighlightPseudoElement with highlight_parent =
//     parent_highlights->TargetText() etc., element.cc:7150-7157).
//
// The rule-match gate (css.HasPseudoElementRules) is the analog of the
// ComputedStyle::HasPseudoElementStyle(pseudo) bit
// ElementRuleCollector::DidMatchRule sets (element_rule_collector.cc:1376),
// which gates Blink's per-highlight recalc the same way.
//
// nil means no rule for this highlight matches originating OR any ancestor
// — the caller (resolveHighlightPseudoStyle) then bakes the UA-default
// fallback per node.
func (r *Renderer) resolveHighlightChainStyle(originating *html.Node, pseudoElement string) *css.Style {
	if originating == nil || originating.Type != html.ElementNode {
		return nil
	}
	if r.highlightChainStyleCaches == nil {
		r.highlightChainStyleCaches = make(map[string]map[*html.Node]*css.Style)
	}
	chainCache, ok := r.highlightChainStyleCaches[pseudoElement]
	if !ok {
		chainCache = make(map[*html.Node]*css.Style)
		r.highlightChainStyleCaches[pseudoElement] = chainCache
	}
	if cached, ok := chainCache[originating]; ok {
		return cached
	}
	highlightParent := r.resolveHighlightChainStyle(originating.Parent, pseudoElement)
	style := highlightParent
	if css.HasPseudoElementRules(originating, pseudoElement, r.selectionStylesheets, r.selectionViewportW, r.selectionViewportH) {
		style = css.ComputeHighlightPseudoElementStyle(originating, pseudoElement, r.selectionStylesheets, r.selectionViewportW, r.selectionViewportH, highlightParent, r.originatingParentStyles(originating)...)
	}
	chainCache[originating] = style
	return style
}

// originatingParentStyles looks up the originating element's own computed
// style for use as ComputePseudoElementStyle's parentStyles. Style isn't
// tracked on *html.Node directly (it lives on layout.Box/css cascade
// results, not the DOM tree) — it comes from r.nodeBoxIndex (populated once
// per Render call, see render.go). Passing it makes custom properties (--x)
// on the originating element visible to var() references in the highlight
// pseudo's rule (CSS Custom Properties §3 — pseudo-elements inherit custom
// properties from their originating element). Without this, `main::selection
// { background-color: var(--x, red) }` with `main { --x: green }` silently
// fell back to the var() fallback value instead of resolving --x
// (highlight-styling-002.html). A missing originating box (nil return) is
// still a valid input — ComputePseudoElementStyle's parent-inheritance
// block is a no-op then, same as for any node not in the index.
func (r *Renderer) originatingParentStyles(originating *html.Node) []*css.Style {
	if box, ok := r.nodeBoxIndex[originating]; ok && box.Style != nil {
		return []*css.Style{box.Style}
	}
	return nil
}

// findOriginatingTextNode finds the DOM text-node child of parent whose
// content, when fragments are consumed in document order, covers the
// given fragment text — returning the text node plus the node-local
// [fragStart, fragEnd) range this fragment's box.Text occupies. Uses
// r.selectionTextConsumed to track how much of each text node's content
// has already been claimed by earlier fragments painted this Render call.
//
// Offsets are node-local BYTE offsets into node.Text (Go string indexing),
// NOT the UTF-16 code-unit offsets html.Range's StartOffset/EndOffset use
// (see html.UTF16OffsetToByteOffset's doc comment for where that
// conversion happens before a Range offset reaches this byte space).
func (r *Renderer) findOriginatingTextNode(parent *html.Node, fragmentText string) (*html.Node, int, int) {
	if parent == nil {
		return nil, 0, 0
	}
	if r.selectionTextConsumed == nil {
		r.selectionTextConsumed = make(map[*html.Node]int)
	}
	fragLen := len(fragmentText)
	for _, child := range parent.Children {
		if child.Type != html.TextNode {
			continue
		}
		consumed := r.selectionTextConsumed[child]
		remaining := len(child.Text) - consumed
		if remaining <= 0 {
			continue
		}
		if fragLen <= remaining {
			fragStart := consumed
			fragEnd := consumed + fragLen
			r.selectionTextConsumed[child] = fragEnd
			return child, fragStart, fragEnd
		}
	}
	// No exact-fit text-node child found (text-transform changed the
	// length, or the fragment spans a generated/pseudo text node not
	// present in the DOM, e.g. ::before content, or a non-text-only
	// child mix this lookup doesn't model). Selection painting is
	// skipped for this fragment — drawText's caller falls back to the
	// single-color path, which is correct-but-unhighlighted rather than
	// wrong.
	return nil, 0, 0
}

// selectionSegment describes one paint segment of a split text run: a
// [start,end) byte range of the original fragment text, plus the resolved
// color overrides to use for that segment (nil overrides mean "use the
// layer's existing values unchanged" — the pre-/post-selection segments).
type selectionSegment struct {
	start, end      int
	selected        bool
	backgroundColor *css.Color // nil = no background rect painted
	textColor       *css.Color // nil = use layer.TextColor
	decorationColor *css.Color // nil = use layer.TextDecorationColor

	// hasOwnDecoration + decorationLine: ::selection's OWN
	// text-decoration (e.g. active-selection-014.html's
	// `div::selection { text-decoration: underline }`, where the
	// ORIGINATING div has no decoration at all — the underline exists
	// purely because ::selection introduces it). Mirrors the legacy
	// single-decoration layer.TextDecoration field's type/semantics
	// (paint_layer.go's s.GetTextDecoration()). hasOwnDecoration is
	// false when ::selection declared no text-decoration-line of its
	// own, in which case the segment keeps the originating layer's
	// existing TextDecoration/AppliedTextDecorations untouched.
	hasOwnDecoration bool
	decorationLine   css.TextDecoration

	// hasOwnTextShadow + textShadows: a highlight pseudo-element's OWN
	// text-shadow (LOU-349's target-text-shadow-horizontal/-vertical.html:
	// `p::target-text { text-shadow: ... }`, distinct from the originating
	// <p>'s own text-shadow). No LOU-344 ::selection target test exercises
	// text-shadow override (confirmed via grep — none of the
	// active-selection-*/selection-*.html tests set text-shadow on
	// ::selection), so this is net-new rather than a pre-existing gap.
	// hasOwnTextShadow is false when the highlight pseudo declared no
	// text-shadow of its own, in which case the segment keeps the
	// originating layer's existing TextShadows untouched — same
	// "introduces vs. inherits" shape as hasOwnDecoration above.
	// textShadows may be a non-nil EMPTY slice (the highlight explicitly
	// sets `text-shadow: none`), which must still overwrite — not skip
	// overwriting — the originating layer's shadows.
	hasOwnTextShadow bool
	textShadows      []css.TextShadow
}

// highlightColors bundles every override resolveHighlightColors extracts
// from a highlight pseudo-element's resolved style — grouped into a struct
// (rather than 6+ positional returns) once LOU-349 added textShadows/
// hasOwnTextShadow alongside LOU-344's original color/decoration fields.
type highlightColors struct {
	background           *css.Color // nil = no background rect painted
	foreground           *css.Color // nil = use layer.TextColor
	decoration           *css.Color // nil = use layer.TextDecorationColor
	hasOwnDecorationLine bool
	decorationLine       css.TextDecoration
	hasOwnTextShadow     bool
	textShadows          []css.TextShadow
}

// resolveHighlightColors reads the color/background-color/text-decoration*/
// text-shadow overrides a highlight pseudo-element's resolved style
// introduces, in the shape computeSelectionSegments's segment construction
// consumes directly. Factored out of computeSelectionSegments when
// LOU-349 needed the identical extraction for ::target-text's resolved
// style (CLAUDE.md §4: 2+ near-identical paths are dedupe-in-scope) —
// pseudoStyle may be either a ::selection or ::target-text
// ComputePseudoElementStyle result; the property names consulted (color,
// background-color, text-decoration-color/-line, text-shadow) are
// identical for both per CSS Pseudo-4's shared valid_for_highlight set.
func resolveHighlightColors(pseudoStyle *css.Style) highlightColors {
	if pseudoStyle == nil {
		return highlightColors{}
	}
	var hc highlightColors
	// A literal `background-color: currentColor` is NOT resolved here
	// against this layer's own color (unlike every other currentColor use
	// in this function, which IS same-element/same-property resolution) —
	// per CSS Pseudo-4's highlight-overlay-stack, `background-color:
	// currentColor` on a highlight pseudo-element instead falls through to
	// the layer immediately BELOW's already-resolved background-color, the
	// same property one layer down (confirmed against highlight-painting-
	// currentcolor-002/002a/002b.html's WPT references: `::selection {
	// background-color: currentColor }` paints the custom highlight's OWN
	// background-color — blue — not ::selection's own resolved `color`,
	// even when `color` is itself an explicit non-currentColor value like
	// green in 002b). Leaving hc.background nil here lets
	// highlightLayerStackColors' existing "background unset → copy the
	// layer below's background" fallthrough handle it uniformly with a
	// genuinely-unset background-color.
	if bg, ok := pseudoStyle.Get("background-color"); ok && !strings.EqualFold(bg, "currentColor") {
		if c, ok := css.ParseColorWithCurrentColor(bg, pseudoStyle.GetColor()); ok {
			hc.background = &c
		}
	}
	if cv, ok := pseudoStyle.Get("color"); ok {
		if c, ok := css.ParseColor(cv); ok {
			hc.foreground = &c
		}
	}
	if dc, hasDC := pseudoStyle.GetTextDecorationColor(); hasDC {
		hc.decoration = &dc
	} else {
		hc.decoration = hc.foreground
	}
	// CSS Pseudo-4 §highlight-painting: a highlight pseudo-element's OWN
	// text-decoration introduces a NEW decoration on the highlighted
	// segment even when the originating element has none
	// (active-selection-014.html). Use GetTextDecorationLine (reads the
	// "text-decoration-line" longhand, which the cascade DOES populate via
	// shorthand expansion) rather than the legacy GetTextDecoration (reads
	// the bare "text-decoration" shorthand key directly, which
	// applyDeclarationWithVisitedFilter expands away rather than storing
	// verbatim — confirmed empirically: a `text-decoration: underline`
	// rule leaves style.Get("text-decoration-line") == "underline" but
	// style.Get("text-decoration") == ("", false)). Mapped down to the
	// legacy single-value TextDecoration enum since that's
	// layer.TextDecoration's field type and every LOU-344/LOU-349 target
	// test using this path sets exactly one line value.
	if line := pseudoStyle.GetTextDecorationLine(); !line.IsNone() {
		switch {
		case line.Has(css.TextDecorationLineUnderline):
			hc.decorationLine = css.TextDecorationUnderline
		case line.Has(css.TextDecorationLineOverline):
			hc.decorationLine = css.TextDecorationOverline
		case line.Has(css.TextDecorationLineLineThrough):
			hc.decorationLine = css.TextDecorationLineThrough
		}
		hc.hasOwnDecorationLine = hc.decorationLine != ""
	}
	// LOU-349: target-text-shadow-horizontal/-vertical.html's
	// `p::target-text { text-shadow: ... }` introduces its OWN shadow set
	// on the highlighted segment, same "introduces vs. inherits" shape as
	// decoration above. hasOwnTextShadow is keyed off whether the
	// "text-shadow" property is present at all on pseudoStyle (Get's ok
	// return), NOT off len(textShadows) > 0 — an explicit `text-shadow:
	// none` is present-but-empty and must still override (clear) the
	// originating layer's shadows, not be mistaken for "highlight didn't
	// touch text-shadow at all". The actual shadow LIST is deliberately
	// NOT parsed here — hc.textShadows is left nil; the sole caller
	// (highlightLayerStackColors) parses it exactly once, with the
	// correctly-resolved currentColor for this layer, avoiding a
	// wasted/incorrect first parse against pseudoStyle.GetColor() here
	// that would immediately be discarded once a real foreground resolves
	// (LOU-354: an earlier version of this function called GetTextShadow()
	// eagerly and the caller re-parsed it anyway in the common case).
	if _, ok := pseudoStyle.Get("text-shadow"); ok {
		hc.hasOwnTextShadow = true
	}
	return hc
}

// highlightLayerName identifies one layer of the CSS Pseudo-4
// §highlight-overlay-stack, bottom-to-top: a named ::highlight(name)
// (LOU-354) below ::target-text below ::selection. Distinct from a plain
// string so highlightLayerStack's construction can't be confused with a
// custom-highlight NAME that happens to be spelled "target-text" or
// "selection" (::highlight(name)'s name is an author-chosen identifier,
// theoretically any string).
type highlightLayerName struct {
	kind string // "custom", "target-text", or "selection"
	name string // the custom-highlight registry name; "" for target-text/selection
}

// highlightLayer pairs one active layer's resolved highlightColors with the
// span it's active over (fragment-local, per targetTextOverlapsForTextNode/
// selectionOverlapForTextNode).
type highlightLayer struct {
	id    highlightLayerName
	spans []highlightSpan
}

// computeSelectionSegments splits a text fragment into paint segments based
// on the CSS Pseudo-4 §highlight-overlay-stack: the originating element at
// the bottom, then registered ::highlight(name) custom highlights (LOU-354,
// in r.customHighlightOrder), then ::target-text (LOU-349), then ::selection
// (LOU-344) on top. Returns a single unhighlighted segment spanning the
// whole text when no layer is configured or none overlaps this fragment;
// the common fast-path callers should check via
// len(segs) == 1 && !segs[0].selected to skip the splitting machinery
// entirely.
func (r *Renderer) computeSelectionSegments(box *layout.Box, text string) []selectionSegment {
	whole := []selectionSegment{{start: 0, end: len(text)}}
	if box == nil || box.Node == nil || text == "" {
		return whole
	}
	if r.selectionRange == nil && len(r.targetTextRanges) == 0 && len(r.customHighlights) == 0 {
		return whole
	}
	textNode, fragStart, fragEnd := r.findOriginatingTextNode(box.Node, text)
	if textNode == nil {
		return whole
	}

	var layers []highlightLayer
	for _, name := range r.customHighlightOrder {
		spans := targetTextOverlapsForTextNode(r.customHighlights[name], textNode, fragStart, fragEnd)
		if len(spans) == 0 {
			continue
		}
		layers = append(layers, highlightLayer{id: highlightLayerName{kind: "custom", name: name}, spans: spans})
	}
	if spans := targetTextOverlapsForTextNode(r.targetTextRanges, textNode, fragStart, fragEnd); len(spans) > 0 {
		layers = append(layers, highlightLayer{id: highlightLayerName{kind: "target-text"}, spans: spans})
	}
	if r.selectionRange != nil {
		if selStart, selEnd, ok := selectionOverlapForTextNode(r.selectionRange, textNode, fragStart, fragEnd); ok {
			layers = append(layers, highlightLayer{id: highlightLayerName{kind: "selection"}, spans: []highlightSpan{{start: selStart, end: selEnd}}})
		}
	}
	if len(layers) == 0 {
		return whole
	}

	return r.highlightSegmentsFromLayers(box.Node, text, layers)
}

// highlightSegmentsFromLayers splits text at every layer's span boundary
// and, for each resulting sub-segment, resolves the bottom-to-top active
// layer stack's colors (highlightLayerStackColors) — the general form of
// LOU-349's old 2-layer "selection sees through to target-text" carve-out,
// extended to however many layers (custom highlights + target-text +
// selection) are simultaneously active over that sub-segment.
func (r *Renderer) highlightSegmentsFromLayers(originating *html.Node, text string, layers []highlightLayer) []selectionSegment {
	boundarySet := map[int]bool{0: true, len(text): true}
	for _, layer := range layers {
		for _, span := range layer.spans {
			boundarySet[span.start] = true
			boundarySet[span.end] = true
		}
	}
	boundaries := make([]int, 0, len(boundarySet))
	for b := range boundarySet {
		boundaries = append(boundaries, b)
	}
	sort.Ints(boundaries)

	var segs []selectionSegment
	for i := 0; i+1 < len(boundaries); i++ {
		start, end := boundaries[i], boundaries[i+1]
		mid := start // any point strictly inside [start,end) identifies which spans cover it; start works since spans are half-open
		var active []highlightLayerName
		for _, layer := range layers {
			for _, span := range layer.spans {
				if mid >= span.start && mid < span.end {
					active = append(active, layer.id)
					break
				}
			}
		}
		if len(active) == 0 {
			segs = append(segs, selectionSegment{start: start, end: end})
			continue
		}
		hc := r.highlightLayerStackColors(originating, active)
		segs = append(segs, selectionSegment{
			start: start, end: end, selected: true,
			backgroundColor: hc.background, textColor: hc.foreground, decorationColor: hc.decoration,
			hasOwnDecoration: hc.hasOwnDecorationLine, decorationLine: hc.decorationLine,
			hasOwnTextShadow: hc.hasOwnTextShadow, textShadows: hc.textShadows,
		})
	}
	return segs
}

// highlightLayerStackColors resolves the paint colors for a sub-segment
// given its bottom-to-top active layer stack (CSS Pseudo-4
// §highlight-overlay-stack), mirroring Blink's HighlightPainter::
// ResolveColorsFromPreviousLayer (third_party/blink/renderer/core/paint/
// highlight_painter.h @ blob 96737c206898cbd27aed2cbeb07c455a3fa3d2dd):
// paint the stack from the originating element upward, and for any layer
// whose color/background-color is unset or currentColor/transparent, copy
// the layer immediately below's ALREADY-resolved value — not the
// originating element's value directly. Generalizes LOU-349's old
// 2-layer-only carve-out (selection sees through to target-text) to
// however many layers (custom highlights + target-text + selection,
// LOU-354) are active.
//
// The base of the stack (the originating element, "below" the bottom-most
// highlight layer) is intentionally represented as the ZERO highlightColors
// value — nil foreground/decoration/background, NOT the originating
// element's own resolved color. A nil field is drawText's existing "use
// this segment's per-FRAGMENT layer.TextColor/TextDecorationColor
// unchanged" signal (render.go: `if seg.textColor != nil ...`), which
// already correctly varies per fragment (e.g. active-selection-025.html's
// ::first-letter fragment vs. the rest of the text, two different
// layer.TextColor values from the SAME originating <div> node). Resolving
// a single originating.Style.GetColor() here instead would collapse that
// per-fragment variation to one flat value — confirmed via regression: an
// earlier version of this function did exactly that and broke
// active-selection-025/027.html (::selection with no author `color`,
// deferring through an unset/broken paired-cascade to the per-fragment
// originating color, not one node-wide color).
func (r *Renderer) highlightLayerStackColors(originating *html.Node, stack []highlightLayerName) highlightColors {
	var resolved highlightColors // zero value: the originating element, nil-signaling "defer to the fragment's own layer colors"

	for _, id := range stack {
		style := r.resolveLayerPseudoStyle(originating, id)
		hc := resolveHighlightColors(style)
		isCurrentColor := style != nil && isCurrentColorKeyword(style)
		if hc.foreground == nil || isCurrentColor {
			hc.foreground = resolved.foreground
		}
		if hc.decoration == nil {
			if hc.foreground != nil {
				hc.decoration = hc.foreground
			} else {
				hc.decoration = resolved.decoration
			}
		}
		if hc.background == nil || hc.background.A == 0 {
			hc.background = resolved.background
		}
		if !hc.hasOwnDecorationLine {
			hc.hasOwnDecorationLine = resolved.hasOwnDecorationLine
			hc.decorationLine = resolved.decorationLine
		}
		if !hc.hasOwnTextShadow {
			hc.hasOwnTextShadow = resolved.hasOwnTextShadow
			hc.textShadows = resolved.textShadows
		} else if style != nil {
			// A layer's OWN text-shadow may itself use currentColor
			// (highlight-painting-currentcolor-004/-004a.html:
			// `p::selection { text-shadow: 0 2em red, 0 4em currentColor }`),
			// which must resolve against THIS layer's own now-resolved
			// foreground (same-element currentColor resolution, CSS Color 4
			// §4.4) — not the layer below's, and not pseudoStyle's own
			// unresolved GetColor() (which defaults an unresolvable
			// "currentcolor" chain to black). Parsed exactly ONCE here
			// (resolveHighlightColors deliberately leaves hc.textShadows
			// unparsed for this reason — see that function's doc comment)
			// with whichever currentColor this layer ultimately resolved
			// to: its own hc.foreground when set, else the layer-below's
			// (falling through the same way foreground/decoration do).
			fg := hc.foreground
			if fg == nil {
				fg = resolved.foreground
			}
			if fg == nil {
				// No layer in the stack (nor any layer below it) has a
				// resolved foreground — unlike hc.foreground/resolved.foreground
				// above (which stay nil on purpose to defer to the fragment's
				// own per-fragment TextColor at paint time, see this
				// function's doc comment), a text-shadow color is resolved
				// and baked into hc.textShadows HERE, not deferred, so it
				// cannot fall back to "nil = defer". Use the originating
				// element's own resolved color instead: Blink's highlight-
				// overlay-stack always has a concrete color at layer 0 (the
				// originating element), so this is the correct fallback
				// value, not an arbitrary black default (CodeRabbit,
				// LOU-354 PR #150 — a bottom-most highlight layer with
				// `text-shadow: currentColor` and no explicit `color`
				// anywhere in the stack previously baked in synthetic
				// black instead).
				if box, ok := r.nodeBoxIndex[originating]; ok && box.Style != nil {
					c := box.Style.GetColor()
					fg = &c
				} else {
					black := css.Color{R: 0, G: 0, B: 0, A: 1.0}
					fg = &black
				}
			}
			// Prepend this layer's OWN shadows onto resolved.textShadows
			// (the layer(s) below's already-resolved shadows) rather than
			// replacing them outright — per CSS Text Decor 4's
			// "first-specified shadow is on top" rule (the same rule
			// render.go's drawText already applies when merging in the
			// ORIGINATING element's own shadow), this layer's shadow is the
			// topmost/most-recently-resolved so it goes FIRST. Previously
			// this assignment discarded resolved.textShadows outright,
			// silently dropping a lower highlight layer's own shadow
			// whenever a HIGHER layer (e.g. ::selection) also declared one
			// — highlight-painting-shadows-horizontal/-vertical.html's
			// 3-way originating+target-text+selection overlap needs all
			// three composited, not just the topmost (LOU-355; confirmed
			// during LOU-347's investigation).
			ownShadows := style.GetTextShadowWithCurrentColor(*fg)
			hc.textShadows = append(append([]css.TextShadow{}, ownShadows...), resolved.textShadows...)
		}
		resolved = hc
	}
	return resolved
}

// resolveLayerPseudoStyle resolves one highlightLayerName's
// ComputePseudoElementStyle, dispatching to the right per-kind cache
// (resolveSelectionPseudoStyle / resolveTargetTextPseudoStyle /
// resolveCustomHighlightPseudoStyle).
func (r *Renderer) resolveLayerPseudoStyle(originating *html.Node, id highlightLayerName) *css.Style {
	switch id.kind {
	case "selection":
		return r.resolveSelectionPseudoStyle(originating)
	case "target-text":
		return r.resolveTargetTextPseudoStyle(originating)
	case "custom":
		return r.resolveCustomHighlightPseudoStyle(originating, id.name)
	}
	return nil
}

// isCurrentColorKeyword reports whether style's "color" property is
// literally the unresolved "currentColor" keyword (case-insensitive, per
// CSS Color 4 §4.4 keyword matching) — ComputePseudoElementStyle (unlike
// ComputeStyle's main cascade) does not resolve `color: currentColor` to a
// concrete value for pseudo-element styles, so this string check is the
// only way to detect the keyword was used (css.ParseColor("currentColor")
// simply fails to parse, indistinguishable from any other invalid value,
// which is why this checks the raw string rather than relying on a failed
// parse as the signal).
func isCurrentColorKeyword(style *css.Style) bool {
	cv, ok := style.Get("color")
	return ok && strings.EqualFold(cv, "currentColor")
}
