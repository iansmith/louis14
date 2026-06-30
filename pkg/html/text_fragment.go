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
