package html

import (
	"reflect"
	"testing"
)

// TestParseTextFragmentDirectives covers exactly the 3 directive shapes used
// by LOU-349's 14 target-text-*.html tests (confirmed via grep — see
// text_fragment.go's file doc comment for the full scope note: no
// prefix-/-suffix qualifiers anywhere in the target set).
func TestParseTextFragmentDirectives(t *testing.T) {
	cases := []struct {
		name      string
		directive string
		want      []TextFragmentSelector
	}{
		{
			name:      "single exact phrase, URL-encoded",
			directive: "text=match%20me",
			want: []TextFragmentSelector{
				{Start: "match me"},
			},
		},
		{
			name:      "two independent exact directives",
			directive: "text=match&text=me",
			want: []TextFragmentSelector{
				{Start: "match"},
				{Start: "me"},
			},
		},
		{
			name:      "range directive plus a second exact directive",
			directive: "text=match,me&text=me%20and%20me",
			want: []TextFragmentSelector{
				{Start: "match", End: "me"},
				{Start: "me and me"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTextFragmentDirectives(tc.directive)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseTextFragmentDirectives(%q) = %+v, want %+v", tc.directive, got, tc.want)
			}
		})
	}
}

// TestTextFragmentSelectorIsRange asserts the Range-vs-Exact type
// distinction (mirrors Blink's TextFragmentSelector::SelectorType kExact
// vs kRange — see text_fragment.go's Blink citation) is purely "does this
// selector have an End": kRange when populated, kExact when empty.
func TestTextFragmentSelectorIsRange(t *testing.T) {
	exact := TextFragmentSelector{Start: "match"}
	if exact.IsRange() {
		t.Error("exact selector (no End) reported IsRange() = true")
	}
	rng := TextFragmentSelector{Start: "match", End: "me"}
	if !rng.IsRange() {
		t.Error("range selector (End set) reported IsRange() = false")
	}
}

// buildPara builds a <p> element with the given text-node children's
// content, wiring Parent pointers — mirrors the html.Parse output shape
// FindTextFragmentMatches walks.
func buildPara(texts ...string) *Node {
	p := &Node{Type: ElementNode, TagName: "p"}
	for _, txt := range texts {
		tn := &Node{Type: TextNode, Text: txt, Parent: p}
		p.Children = append(p.Children, tn)
	}
	return p
}

// TestFindTextFragmentMatches_SingleExact covers target-text-001.html's
// shape: one exact selector matching a single whole text node.
func TestFindTextFragmentMatches_SingleExact(t *testing.T) {
	p := buildPara("match me")
	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "match me"}})
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	rng := matches[0]
	if rng.StartContainer != p.Children[0] || rng.StartOffset != 0 {
		t.Errorf("start = (%v, %d), want (text node, 0)", rng.StartContainer, rng.StartOffset)
	}
	if rng.EndContainer != p.Children[0] || rng.EndOffset != len("match me") {
		t.Errorf("end = (%v, %d), want (text node, %d)", rng.EndContainer, rng.EndOffset, len("match me"))
	}
}

// TestFindTextFragmentMatches_CaseInsensitive covers Blink's
// FindOptions().SetCaseInsensitive(true) (verified against live Blink
// source in text_fragment_finder.cc — see text_fragment.go's Blink
// citation): a directive in one case must match document text in another.
func TestFindTextFragmentMatches_CaseInsensitive(t *testing.T) {
	p := buildPara("Match Me")
	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "match me"}})
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1 (case-insensitive match)", len(matches))
	}
}

// TestFindTextFragmentMatches_AcrossElementBoundary covers
// target-text-002.html's shape: "ma<span>tch </span>me" — the match phrase
// "match me" spans 3 text nodes across an element boundary. Each spanned
// text node must produce its own Range (StartContainer == EndContainer ==
// that node) so the existing per-text-node paint machinery
// (selectionOverlapForTextNode) can drive the highlight across all three
// without modification.
func TestFindTextFragmentMatches_AcrossElementBoundary(t *testing.T) {
	p := &Node{Type: ElementNode, TagName: "p"}
	a := &Node{Type: TextNode, Text: "ma", Parent: p}
	span := &Node{Type: ElementNode, TagName: "span", Parent: p}
	mid := &Node{Type: TextNode, Text: "tch ", Parent: span}
	c := &Node{Type: TextNode, Text: "me", Parent: p}
	p.Children = []*Node{a, span, c}
	span.Children = []*Node{mid}

	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "match me"}})
	if len(matches) != 3 {
		t.Fatalf("got %d ranges, want 3 (one per spanned text node), matches=%+v", len(matches), matches)
	}
	wantNodes := []*Node{a, mid, c}
	for i, rng := range matches {
		if rng.StartContainer != wantNodes[i] || rng.EndContainer != wantNodes[i] {
			t.Errorf("range %d: StartContainer=%v EndContainer=%v, want both = %v", i, rng.StartContainer, rng.EndContainer, wantNodes[i])
		}
	}
	// First node ("ma") fully covered: [0,2). Middle node ("tch ") fully
	// covered: [0,4). Last node ("me") fully covered: [0,2).
	if matches[0].StartOffset != 0 || matches[0].EndOffset != 2 {
		t.Errorf("first range = [%d,%d), want [0,2)", matches[0].StartOffset, matches[0].EndOffset)
	}
	if matches[1].StartOffset != 0 || matches[1].EndOffset != 4 {
		t.Errorf("middle range = [%d,%d), want [0,4)", matches[1].StartOffset, matches[1].EndOffset)
	}
	if matches[2].StartOffset != 0 || matches[2].EndOffset != 2 {
		t.Errorf("last range = [%d,%d), want [0,2)", matches[2].StartOffset, matches[2].EndOffset)
	}
}

// TestFindTextFragmentMatches_MultipleDirectives covers target-text-003's
// shape: two independent `&text=` directives each producing their own,
// non-contiguous match.
func TestFindTextFragmentMatches_MultipleDirectives(t *testing.T) {
	p := buildPara("match me and me")
	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "match"}, {Start: "me"}})
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	if matches[0].StartOffset != 0 || matches[0].EndOffset != len("match") {
		t.Errorf("first match = [%d,%d), want [0,%d) (\"match\")", matches[0].StartOffset, matches[0].EndOffset, len("match"))
	}
	wantStart := len("match ")
	wantEnd := wantStart + len("me")
	if matches[1].StartOffset != wantStart || matches[1].EndOffset != wantEnd {
		t.Errorf("second match = [%d,%d), want [%d,%d) (first \"me\" after \"match\")", matches[1].StartOffset, matches[1].EndOffset, wantStart, wantEnd)
	}
}

// TestFindTextFragmentMatches_Range covers target-text-010.html's shape: a
// Range selector "match,me" against "match me and me" must span from the
// start of "match" to the end of the NEXT "me" occurrence strictly after
// "match"'s own match position — i.e. "match me" (the first "me", not the
// second). Combined with the second exact directive "me and me" (not
// exercised by this test directly — see TestFindTextFragmentMatches_
// MultipleDirectives for the multi-directive shape), the two together cover
// the whole "match me and me" phrase per target-text-010's reference file.
func TestFindTextFragmentMatches_Range(t *testing.T) {
	p := buildPara("match me and me")
	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "match", End: "me"}})
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	want := "match me"
	if matches[0].StartOffset != 0 || matches[0].EndOffset != len(want) {
		t.Errorf("range match = [%d,%d), want [0,%d) (%q)", matches[0].StartOffset, matches[0].EndOffset, len(want), want)
	}
}

// TestFindTextFragmentMatches_NoMatch asserts a selector with no match in
// the document text yields no Range (rather than panicking or returning a
// bogus zero-length range) — mirrors target-text directives that simply
// fail to resolve (the Text Fragment spec treats this as silently
// unhighlighted, not an error).
func TestFindTextFragmentMatches_NoMatch(t *testing.T) {
	p := buildPara("nothing here")
	matches := FindTextFragmentMatches(p, []TextFragmentSelector{{Start: "absent"}})
	if len(matches) != 0 {
		t.Errorf("got %d matches, want 0", len(matches))
	}
}
