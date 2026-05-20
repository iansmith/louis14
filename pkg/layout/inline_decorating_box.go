package layout

import (
	"math"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// onLineDecorator tracks one inline element on a line that declares a
// text-decoration. originX/endX accumulate the row-span across all text
// fragments the inline contains; firstIdx/lastIdx point into the per-line
// text-fragment list and let the painter trim insets at the inline's
// logical edges only.
//
// Mirrors a single entry of Blink's InlinePaintContext::DecoratingBoxList
// (`core/paint/inline_paint_context.h:20-26` @ SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
type onLineDecorator struct {
	node      *html.Node
	originX   float64
	endX      float64
	firstSeen bool
	firstIdx  int
	lastIdx   int
	// continuedFromPriorLine is true when this decorator entered the line
	// via enteringSpanStack (the inline started on a prior line). The
	// painter must NOT treat any fragment on this line as the source-order
	// first fragment of the decorating box.
	continuedFromPriorLine bool
	// closedOnLine is true after a matching CloseTag pops this decorator.
	// False means the decorator's inline continues to a subsequent line —
	// in which case no fragment on this line can be IsLastFragment.
	closedOnLine bool
}

// computeDecoratingBoxMetadataPerLine pre-computes per-text-fragment
// AppliedTextDecoration vectors with fragment-continuity metadata
// (LOU-149 Phase 4). The result maps each text InlineItem on the line to a
// per-fragment vector that overrides the cascade-time vector at paint time.
//
// Mirrors Blink's InlinePaintContext::PushDecoratingBoxAncestors flow at
// `core/paint/inline_paint_context.h:26` (SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
// Per the risk note in LOU-149 findings.md, the stack is built in walk order
// over `line.Results` — the same iteration the existing span-stack uses — so
// bidi reordering is honored as long as OpenTag/CloseTag remain matched, the
// same invariant the span stack already relies on.
//
// alignOffset is the inline-axis text-align offset already applied to
// trackPos; it is the X of the leading edge of the line's content area.
// enteringSpanStack carries inlines opened on prior lines and still open
// here — exactly the set the createLineBoxEx span pre-pass also seeds with.
//
// Returns nil when no text fragment on the line carries a non-empty
// AppliedTextDecorations vector (the common case — no allocation).
func computeDecoratingBoxMetadataPerLine(
	line *LineInfo,
	alignOffset float64,
	enteringSpanStack []*InlineItem,
) map[*InlineItem][]css.AppliedTextDecoration {
	hasAny := false
	for _, r := range line.Results {
		if r.Item.Type != InlineItemText || r.Item.Style == nil {
			continue
		}
		if len(r.Item.Style.GetAppliedTextDecorations()) > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}

	type lineFrag struct {
		item *InlineItem
		x    float64
	}

	// allDecorators owns every decorator created on this line (continuing
	// from prior + newly opened). The flat slice replaces a map: typical
	// decorator count per line is 0-3, where a linear scan beats a map
	// lookup. stack is the currently-open subset (LIFO).
	var allDecorators []*onLineDecorator
	var stack []*onLineDecorator
	var lineFrags []lineFrag

	for _, item := range enteringSpanStack {
		if item == nil || item.Style == nil || item.Node == nil {
			continue
		}
		if item.Style.GetTextDecorationLine().IsNone() {
			continue
		}
		d := &onLineDecorator{
			node:                   item.Node,
			originX:                math.Inf(1),
			endX:                   math.Inf(-1),
			continuedFromPriorLine: true,
		}
		allDecorators = append(allDecorators, d)
		stack = append(stack, d)
	}

	lineContentMinX := math.Inf(1)
	lineContentMaxX := math.Inf(-1)
	firstLineTextIdx := -1
	lastLineTextIdx := -1

	trackX := alignOffset
	for _, r := range line.Results {
		switch r.Item.Type {
		case InlineItemOpenTag:
			if r.Item.Style != nil && r.Item.Node != nil &&
				!r.Item.Style.GetTextDecorationLine().IsNone() {
				d := &onLineDecorator{
					node:    r.Item.Node,
					originX: math.Inf(1),
					endX:    math.Inf(-1),
				}
				allDecorators = append(allDecorators, d)
				stack = append(stack, d)
			}
			trackX += r.InlineSize
		case InlineItemCloseTag:
			// Same invariant as the span-stack pre-pass at inline_layout.go:1399:
			// a CloseTag always pairs with top-of-stack. Pop only when the top
			// matches this node (so unmatched closes are silently ignored, as
			// in the span pass).
			if r.Item.Node != nil && len(stack) > 0 &&
				stack[len(stack)-1].node == r.Item.Node {
				stack[len(stack)-1].closedOnLine = true
				stack = stack[:len(stack)-1]
			}
			trackX += r.InlineSize
		case InlineItemText:
			xStart := trackX
			xEnd := trackX + r.InlineSize
			idx := len(lineFrags)
			lineFrags = append(lineFrags, lineFrag{item: r.Item, x: xStart})

			if xStart < lineContentMinX {
				lineContentMinX = xStart
			}
			if xEnd > lineContentMaxX {
				lineContentMaxX = xEnd
			}
			if firstLineTextIdx < 0 {
				firstLineTextIdx = idx
			}
			lastLineTextIdx = idx

			for _, d := range stack {
				if xStart < d.originX {
					d.originX = xStart
				}
				if xEnd > d.endX {
					d.endX = xEnd
				}
				if !d.firstSeen {
					d.firstIdx = idx
					d.firstSeen = true
				}
				d.lastIdx = idx
			}
			trackX += r.InlineSize
		case InlineItemAtomicInline:
			// Atomic inlines (inline-block, replaced, etc.) are decoration
			// boundaries per spec: a decoration declared on an ancestor of
			// an atomic does NOT propagate into the atomic's own inline-
			// formatting-context. The cascade-side enforcement of that
			// reset is Phase-pending in louis14 (style.go:10499 "Phase 4
			// will wire boundary resets"). Here we merely don't descend;
			// the atomic's inline width still counts toward an outer
			// decorator's row-span — Blink treats the atomic as an opaque
			// inline-content unit for extent purposes.
			trackX += r.Margins.InlineStart + r.InlineSize + r.Margins.InlineEnd
		default:
			trackX += r.InlineSize
		}
	}

	// Cache the innermost-first ancestor-decorator chain per text-source
	// Node. Bidi-split fragments from the same text node share the same
	// chain — avoid re-walking item.Node.Parent for every visual fragment.
	contribCache := map[*html.Node][]*onLineDecorator{}

	out := map[*InlineItem][]css.AppliedTextDecoration{}
	for fragIdx, lf := range lineFrags {
		item := lf.item
		if item.Style == nil {
			continue
		}
		vec := item.Style.GetAppliedTextDecorations()
		if len(vec) == 0 {
			continue
		}

		inlineContribInnerFirst, cached := contribCache[item.Node]
		if !cached {
			for n := item.Node; n != nil; n = n.Parent {
				for _, d := range allDecorators {
					if d.node == n {
						inlineContribInnerFirst = append(inlineContribInnerFirst, d)
						break
					}
				}
			}
			contribCache[item.Node] = inlineContribInnerFirst
		}

		stamped := make([]css.AppliedTextDecoration, len(vec))
		copy(stamped, vec)

		k := len(inlineContribInnerFirst)
		nEntries := len(vec)
		// External (block / above-IFC) contributor entries [0..N-K-1].
		for i := range nEntries - k {
			stamped[i].HasDecoratingBox = true
			stamped[i].DecoratingBoxOriginX = lineContentMinX
			stamped[i].DecoratingBoxWidth = lineContentMaxX - lineContentMinX
			stamped[i].IsFirstFragment = fragIdx == firstLineTextIdx
			stamped[i].IsLastFragment = fragIdx == lastLineTextIdx
		}
		// On-line inline-contributor entries. vec[N-1] ↔ innermost,
		// vec[N-K] ↔ outermost on-line contributor.
		for i := range k {
			d := inlineContribInnerFirst[i]
			vecIdx := nEntries - 1 - i
			stamped[vecIdx].HasDecoratingBox = true
			stamped[vecIdx].DecoratingBoxOriginX = d.originX
			stamped[vecIdx].DecoratingBoxWidth = d.endX - d.originX
			stamped[vecIdx].IsFirstFragment = !d.continuedFromPriorLine && fragIdx == d.firstIdx
			stamped[vecIdx].IsLastFragment = d.closedOnLine && fragIdx == d.lastIdx
		}

		out[item] = stamped
	}

	return out
}

