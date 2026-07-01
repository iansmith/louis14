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
// background rect should use for a text fragment box: the originating
// element's own inline background-fragment box when one exists as a
// sibling of textBox, falling back to textBox's own (Y, Height)
// otherwise.
//
// pkg/layout/inline_layout.go emits an inline element's own background as
// a SEPARATE sibling PhysicalFragment from its text fragments (see that
// file's "An inline background covers at least the font's em box... and
// grows to the line box when line-height exceeds it — max(em box, line
// box)" comment), sharing textBox.Parent but carrying no .Text. A text
// fragment's own box.Height is ALWAYS just the font's em box (fontSize,
// per emitTextFragment), never the line box — so when line-height >
// font-size (line-height: normal's 1.2x default, the common case), the
// background fragment is taller than the text fragment and anchored
// higher (line-box top vs. the text's baseline-anchored em box). Using
// the text fragment's own (Y, Height) for the selection rect then leaves
// a gap at the top where the originating element's own background
// (e.g. `background-color: red`) shows through above the selection
// highlight (selection-contenteditable-011.html's ~3px red sliver,
// LOU-344). Reusing the SAME background-fragment box the originating
// element's own background already painted with (rather than
// re-deriving the max(em box, line box) sizing independently at paint
// time, which would duplicate pkg/layout's logic and risk drifting out
// of sync with it) keeps the two rects pixel-identical by construction.
func selectionBackgroundRectY(textBox *layout.Box) (y, height float64) {
	if textBox == nil {
		return 0, 0
	}
	if textBox.Parent != nil && textBox.Node != nil {
		for _, sibling := range textBox.Parent.Children {
			if sibling != textBox && sibling.Node == textBox.Node && sibling.Text == "" {
				return sibling.Y, sibling.Height
			}
		}
	}
	return textBox.Y, textBox.Height
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

// resolveHighlightPseudoStyle is the shared implementation behind
// resolveSelectionPseudoStyle/resolveTargetTextPseudoStyle, parameterized
// by pseudo-element name and which per-Render-call cache to memoize into —
// factored out when LOU-349 added the ::target-text variant with an
// otherwise byte-identical body (CLAUDE.md §4: 2+ near-identical paths are
// dedupe-in-scope).
//
// Deliberately NOT gated on len(r.selectionStylesheets) == 0: a page with
// no stylesheets at all (or none containing a rule for this pseudo) must
// still resolve the UA default highlight colors when this highlight is
// active — ComputePseudoElementStyle's applySelectionCascade/
// applyTargetTextCascade apply those UA defaults itself when authorSet is
// empty, so an empty stylesheet list is a valid, expected input here, not a
// reason to skip resolution entirely.
func (r *Renderer) resolveHighlightPseudoStyle(originating *html.Node, pseudoElement string, cache map[*html.Node]*css.Style) *css.Style {
	if originating == nil {
		return nil
	}
	if cached, ok := cache[originating]; ok {
		return cached
	}
	// originating.Style isn't tracked on *html.Node directly (style lives
	// on layout.Box/css cascade results, not the DOM tree) — look it up
	// via r.nodeBoxIndex (populated once per Render call, see render.go)
	// and pass it as ComputePseudoElementStyle's parentStyles so custom
	// properties (--x) on the originating element are visible to var()
	// references in the highlight pseudo's rule (CSS Custom Properties §3
	// — pseudo-elements inherit custom properties from their originating
	// element). Without this, `main::selection { background-color:
	// var(--x, red) }` with `main { --x: green }` silently fell back to
	// the var() fallback value instead of resolving --x (highlight-
	// styling-002.html). A missing originating box (nil parentStyles) is
	// still a valid input — ComputePseudoElementStyle's parent-inheritance
	// block is a no-op then, same as before this fix for any node not yet
	// in the index.
	var parentStyles []*css.Style
	if box, ok := r.nodeBoxIndex[originating]; ok && box.Style != nil {
		parentStyles = []*css.Style{box.Style}
	}
	style := css.ComputePseudoElementStyle(originating, pseudoElement, r.selectionStylesheets, r.selectionViewportW, r.selectionViewportH, parentStyles...)
	cache[originating] = style
	return style
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
	if bg, ok := pseudoStyle.Get("background-color"); ok {
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
	// touch text-shadow at all".
	if _, ok := pseudoStyle.Get("text-shadow"); ok {
		hc.hasOwnTextShadow = true
		hc.textShadows = pseudoStyle.GetTextShadow()
	}
	return hc
}

// computeSelectionSegments splits a text fragment into 1-3 segments
// (pre-highlight / highlighted / post-highlight) based on the configured
// ::selection range OR ::target-text match ranges (LOU-349) — whichever is
// active for this fragment's originating text node — and resolves the
// applicable highlight pseudo-element's colors. Returns a single
// unhighlighted segment spanning the whole text when neither context is
// configured or neither overlaps this fragment; the common fast-path
// callers should check via len(segs) == 1 && !segs[0].selected to skip the
// splitting machinery entirely.
//
// ::selection is checked first and, if it overlaps, wins outright — none
// of LOU-349's 14 target tests combine ::selection and ::target-text on
// the same page (confirmed via grep), so simultaneous multi-highlight-type
// layering (Blink's full HighlightPainter PaintCase dispatch / highlight
// "layer" stacking) is explicitly out of scope; this ordering is a
// reasonable, simple tie-break rather than a deliberate priority model.
func (r *Renderer) computeSelectionSegments(box *layout.Box, text string) []selectionSegment {
	whole := []selectionSegment{{start: 0, end: len(text)}}
	if box == nil || box.Node == nil || text == "" {
		return whole
	}
	if r.selectionRange == nil && len(r.targetTextRanges) == 0 {
		return whole
	}
	textNode, fragStart, fragEnd := r.findOriginatingTextNode(box.Node, text)
	if textNode == nil {
		return whole
	}

	if r.selectionRange != nil {
		if selStart, selEnd, ok := selectionOverlapForTextNode(r.selectionRange, textNode, fragStart, fragEnd); ok {
			selectionStyle := r.resolveSelectionPseudoStyle(box.Node)
			hc := resolveHighlightColors(selectionStyle)
			// target-text-004.html / target-text-005.html: ::selection over
			// the SAME text a ::target-text rule also covers (here, both
			// span the whole "match me" via selectNodeContents). Per CSS
			// Pseudo-4 §highlight-overlay-stack, ::selection sits ABOVE
			// ::target-text in the highlight layer stack, so a property
			// ::selection leaves at its CSS-wide initial value — `color:
			// currentColor` (target-text-004) or `background-color:
			// transparent` (target-text-005) — must resolve by looking
			// THROUGH to the layer below (::target-text), not straight to
			// the originating element. Confirmed against both tests' WPT
			// references: 004 wants ::target-text's lime color to show
			// through selection's currentColor; 005 wants ::target-text's
			// green background to show through selection's transparent
			// background. Full multi-layer overlay painting (Blink's
			// HighlightPainter PaintCase dispatch) is out of scope (LOU-349's
			// ticket: none of the 14 targets need genuinely SIMULTANEOUS
			// layering — only this "see-through" carve-out for the two
			// properties whose initial/keyword value is itself
			// transparent/inherit-like) — decoration/shadow are NOT given
			// the same treatment since no test exercises a see-through case
			// for either.
			if (hc.foreground == nil && selectionStyle != nil && isCurrentColorKeyword(selectionStyle)) ||
				(hc.background != nil && hc.background.A == 0) {
				if ttSpans := targetTextOverlapsForTextNode(r.targetTextRanges, textNode, fragStart, fragEnd); len(ttSpans) > 0 {
					ttHC := resolveHighlightColors(r.resolveTargetTextPseudoStyle(box.Node))
					if hc.foreground == nil && ttHC.foreground != nil {
						hc.foreground = ttHC.foreground
						if hc.decoration == nil {
							hc.decoration = ttHC.foreground
						}
					}
					if hc.background != nil && hc.background.A == 0 && ttHC.background != nil {
						hc.background = ttHC.background
					}
				}
			}
			return highlightSegmentsFromSpans(text, []highlightSpan{{start: selStart, end: selEnd}}, hc)
		}
	}

	if spans := targetTextOverlapsForTextNode(r.targetTextRanges, textNode, fragStart, fragEnd); len(spans) > 0 {
		hc := resolveHighlightColors(r.resolveTargetTextPseudoStyle(box.Node))
		return highlightSegmentsFromSpans(text, spans, hc)
	}

	return whole
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

// highlightSegmentsFromSpans converts a sorted, non-overlapping list of
// highlight spans (already fragment-local — see targetTextOverlapsForTextNode/
// selectionOverlapForTextNode) into the full selectionSegment sequence
// drawText consumes: unhighlighted gaps before/between/after each span,
// interleaved with one "selected" segment per span, all carrying the SAME
// resolved colors (a single highlight pseudo-element style applies
// uniformly across every span for one fragment — e.g. target-text-009.html's
// two overlapping/merged spans share one p::target-text rule).
func highlightSegmentsFromSpans(text string, spans []highlightSpan, hc highlightColors) []selectionSegment {
	var segs []selectionSegment
	cursor := 0
	for _, span := range spans {
		if span.start > cursor {
			segs = append(segs, selectionSegment{start: cursor, end: span.start})
		}
		segs = append(segs, selectionSegment{
			start: span.start, end: span.end, selected: true,
			backgroundColor: hc.background, textColor: hc.foreground, decorationColor: hc.decoration,
			hasOwnDecoration: hc.hasOwnDecorationLine, decorationLine: hc.decorationLine,
			hasOwnTextShadow: hc.hasOwnTextShadow, textShadows: hc.textShadows,
		})
		cursor = span.end
	}
	if cursor < len(text) {
		segs = append(segs, selectionSegment{start: cursor, end: len(text)})
	}
	return segs
}
