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
