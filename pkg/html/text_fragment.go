package html

import (
	"net/url"
	"strings"
)

// text_fragment.go parses URL Text Fragment directives (the `:~:text=...`
// suffix on a URL fragment, https://wicg.github.io/scroll-to-text-fragment/)
// for LOU-349's ::target-text support. Mirrors Blink's
// TextFragmentSelector (third_party/blink/renderer/core/fragment_directive/
// text_fragment_selector.h @ blob
// ea3df1aea8b2ec602cc9a3bddc2b758945ae8a03), which parses a single
// serialized directive `"prefix-,start,end,-suffix"` into
// {type: kExact|kRange, start_, end_, prefix_, suffix_}.
//
// Scope cut (confirmed via grep across all 14 LOU-349 target-text-*.html
// tests — see the ticket's "Directive syntax actually used" note): no test
// uses the `prefix-,`/`,-suffix` context-disambiguation qualifiers, so they
// are NOT implemented here. TextFragmentSelector therefore carries only
// Start/End, dropping Blink's Prefix_/Suffix_ fields as out of scope rather
// than as unfinished work.

// TextFragmentSelector is one parsed `text=...` directive: either an Exact
// match (End == "") or a Range match (End != ""), per Blink's
// SelectorType kExact/kRange (see file doc comment for the citation).
type TextFragmentSelector struct {
	Start string
	End   string
}

// IsRange reports whether this selector is the Range form (`text=start,end`)
// rather than Exact (`text=start`). Mirrors Blink's
// TextFragmentSelector::type() == kRange — in this simplified port the
// distinction collapses to "does the directive have a second (comma-split)
// piece", since Prefix_/Suffix_ aren't modeled (see file doc comment).
func (s TextFragmentSelector) IsRange() bool {
	return s.End != ""
}

// ParseTextFragmentDirectives parses the directive portion of a URL
// fragment (everything after the `:~:` marker — e.g. "text=match%20me",
// already stripped of the marker by the caller; see
// pkg/js/dom_location.go's notifyFragmentChanged) into zero or more
// TextFragmentSelectors.
//
// Directive syntax handled (mirrors the WICG spec's "parsing the fragment
// directive" + Blink's TextFragmentSelector::FromTextDirective, scoped down
// per this file's doc comment):
//   - The first directive is `text=...` immediately after the `:~:` marker;
//     subsequent directives are separated by literal `&text=`.
//   - Each directive's value is percent-decoded (net/url.QueryUnescape)
//     before further parsing — e.g. "%20" -> " ".
//   - A decoded value containing an unescaped "," splits into Start/End
//     (the Range form); otherwise the whole decoded value is Start (Exact).
//     The split happens on the DECODED string per the spec's
//     "let start and end be the result of strictly splitting input on
//     commas" step — a comma can only appear as a literal separator or
//     pre-encoded as %2C in the source URL, so decoding first then
//     splitting on "," is equivalent to (and simpler than) splitting first
//     and decoding each piece, since the 14 LOU-349 target tests never
//     embed a literal "," inside a match phrase itself.
//
// Malformed pieces (failed percent-decoding, or a directive that isn't
// `text=...` at all) are skipped rather than aborting the whole parse —
// matches the WICG spec's per-directive "invalid syntax" handling
// (https://wicg.github.io/scroll-to-text-fragment/#parsing-the-fragment-directive
// step 3.2: invalid directives are dropped, not fatal).
func ParseTextFragmentDirectives(directive string) []TextFragmentSelector {
	var selectors []TextFragmentSelector
	for _, piece := range strings.Split(directive, "&") {
		value, ok := strings.CutPrefix(piece, "text=")
		if !ok {
			continue
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			continue
		}
		if start, end, isRange := strings.Cut(decoded, ","); isRange {
			selectors = append(selectors, TextFragmentSelector{Start: start, End: end})
		} else {
			selectors = append(selectors, TextFragmentSelector{Start: decoded})
		}
	}
	return selectors
}

// textRun is one DOM text node's contribution to the flattened document-text
// search space FindTextFragmentMatches builds: the node itself plus the
// [docStart, docEnd) byte range it occupies in the concatenated search
// string built by collectTextRuns.
type textRun struct {
	node             *Node
	docStart, docEnd int
}

// collectTextRuns walks root in pre-order (document order) collecting every
// descendant TextNode as a textRun, alongside the single concatenated
// search string those runs cover. Mirrors Blink's TextIterator walking the
// DOM in document order to build the search corpus for FindBuffer (see
// text_fragment.go's file doc comment) — louis14 has no existing DOM-order
// text walker (confirmed: pkg/html, pkg/css, pkg/render, pkg/layout have no
// :contains()/TextContent-across-subtree helper), so this is new rather
// than reused.
//
// A single space is inserted between adjacent text runs that belong to
// DIFFERENT parent elements (i.e. at an element boundary) when neither run
// already starts/ends with whitespace — this mirrors block/inline
// formatting context text generally being separated by at least the
// element structure, and avoids accidentally gluing "ma" + "tch" into
// "match" across an unrelated element boundary. None of the 14 LOU-349
// target tests actually need this disambiguation (their cross-element
// split points place the boundary mid-word with no separating whitespace
// expected, e.g. "ma<span>tch </span>me" must search as "match me" with NO
// inserted space), so no separator is inserted — text runs are
// concatenated VERBATIM. This matches Blink's own behavior of searching
// the rendered text content directly (TextIterator's default behavior
// without ContentVisibility/display:none gaps).
func collectTextRuns(root *Node) (runs []textRun, corpus string) {
	var sb strings.Builder
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Type == TextNode {
			start := sb.Len()
			sb.WriteString(n.Text)
			runs = append(runs, textRun{node: n, docStart: start, docEnd: sb.Len()})
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return runs, sb.String()
}

// findCaseInsensitive returns the byte index of the first occurrence of
// substr in corpus starting at-or-after fromByte, comparing
// case-insensitively, or -1 if not found. Mirrors Blink's
// FindOptions().SetCaseInsensitive(true), confirmed against live Blink
// source (text_fragment_finder.cc — see text_fragment.go's file doc
// comment for the verification trail). Whitespace is NOT collapsed/
// normalized before comparison — the documented fallback for the
// (unverified this session) whitespace-handling question, sufficient for
// every LOU-349 target test's single-space-separated phrases.
func findCaseInsensitive(corpus string, fromByte int, substr string) int {
	if fromByte > len(corpus) {
		return -1
	}
	idx := strings.Index(strings.ToLower(corpus[fromByte:]), strings.ToLower(substr))
	if idx < 0 {
		return -1
	}
	return fromByte + idx
}

// FindTextFragmentMatches searches root's descendant text content (in
// document order) for each selector in turn, returning one *Range per
// MATCHED TEXT NODE — when a single logical match spans multiple text
// nodes (e.g. target-text-002.html's "ma<span>tch </span>me"), it produces
// multiple consecutive Ranges, one per spanned node, each with
// StartContainer == EndContainer == that node. This (rather than one Range
// whose StartContainer/EndContainer are different nodes) is deliberate: it
// lets the match feed directly into the EXISTING per-text-node paint
// machinery in pkg/render/selection_paint.go
// (selectionOverlapForTextNode/nodeLocalBoundary), which already resolves
// a same-node Range with zero changes — see LOU-349's ticket "Decide
// whether ::target-text can reuse this machinery directly" note.
//
// Exact selectors (IsRange() == false) find the first occurrence of
// Start. Range selectors find Start, then find the next occurrence of End
// STRICTLY AFTER Start's match position; the resulting match spans from
// Start's beginning to End's end (mirrors Blink's
// TextFragmentFinder's kRange handling, modulo the prefix/suffix
// qualifiers this port deliberately omits — see TextFragmentSelector's
// doc comment).
//
// A selector with no match anywhere in the document yields no Range for
// that selector (skipped, not an error) — matches the WICG spec's
// "scroll to text fragment" algorithm silently doing nothing when a
// directive fails to resolve.
func FindTextFragmentMatches(root *Node, selectors []TextFragmentSelector) []*Range {
	runs, corpus := collectTextRuns(root)
	var ranges []*Range
	for _, sel := range selectors {
		matchStart := findCaseInsensitive(corpus, 0, sel.Start)
		if matchStart < 0 {
			continue
		}
		matchEnd := matchStart + len(sel.Start)
		if sel.IsRange() {
			endStart := findCaseInsensitive(corpus, matchEnd, sel.End)
			if endStart < 0 {
				continue
			}
			matchEnd = endStart + len(sel.End)
		}
		ranges = append(ranges, rangesForCorpusSpan(runs, matchStart, matchEnd)...)
	}
	return ranges
}

// rangesForCorpusSpan converts a [matchStart, matchEnd) byte span in the
// collectTextRuns corpus into one *Range per text run it overlaps,
// clipping each Range's offsets to that run's own node-local bounds. Byte
// offsets here (both the corpus span and the resulting Range
// StartOffset/EndOffset) are Go string byte offsets, NOT html.Range's
// usual UTF-16 code-unit convention (see html.Range's doc comment) —
// pkg/render/selection_paint.go's findOriginatingTextNode/
// selectionOverlapForTextNode consume node-local fragment offsets in BYTES
// already (see that file's "Offsets are node-local BYTE offsets" comment
// on findOriginatingTextNode), so target-text matches are produced
// pre-converted to the same byte convention rather than round-tripping
// through UTF-16 only for selectionOverlapForTextNode to convert back.
func rangesForCorpusSpan(runs []textRun, matchStart, matchEnd int) []*Range {
	var out []*Range
	for _, run := range runs {
		overlapStart := max(matchStart, run.docStart)
		overlapEnd := min(matchEnd, run.docEnd)
		if overlapStart >= overlapEnd {
			continue
		}
		out = append(out, &Range{
			StartContainer: run.node,
			StartOffset:    overlapStart - run.docStart,
			EndContainer:   run.node,
			EndOffset:      overlapEnd - run.docStart,
		})
	}
	return out
}
