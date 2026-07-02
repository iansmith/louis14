package css

// Indicator tests for LOU-213: marker-allowed property set for hyphens,
// overflow-wrap, tab-size, word-break, text-emphasis, and text-shadow
// inheritance.
//
// Mirrors Blink css_properties.json5 valid_for_marker flags at SHA
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f.

import (
	"louis14/pkg/html"
	"testing"
)

// TestMarkerAllowedProperty_MissingProperties asserts that each property the
// ::marker pseudo-element must accept per CSS Pseudo-4 §4.4 is accepted by
// markerAllowedProperty().
//
// Named after the WPT test files they correspond to:
//
//	marker-hyphens.html
//	marker-overflow-wrap.html
//	marker-tab-size.html
//	marker-word-break.html
//	marker-text-shadow.html   (already allowed; inheritable-props gap)
//	marker-text-emphasis.html
func TestMarkerAllowedProperty_MissingProperties(t *testing.T) {
	cases := []struct {
		name   string // WPT test name
		prop   string
		wantOK bool
	}{
		// marker-hyphens.html — hyphens is not in the switch
		{"marker-hyphens.html", "hyphens", true},
		// marker-overflow-wrap.html
		{"marker-overflow-wrap.html", "overflow-wrap", true},
		// marker-tab-size.html
		{"marker-tab-size.html", "tab-size", true},
		// marker-word-break.html
		{"marker-word-break.html", "word-break", true},
		// marker-text-emphasis.html — shorthand and longhands
		{"marker-text-emphasis.html/shorthand", "text-emphasis", true},
		{"marker-text-emphasis.html/style", "text-emphasis-style", true},
		{"marker-text-emphasis.html/color", "text-emphasis-color", true},
		{"marker-text-emphasis.html/position", "text-emphasis-position", true},
		// text-shadow is already in the switch — verify it stays allowed
		{"marker-text-shadow.html", "text-shadow", true},
		// LOU-358: line-break, text-decoration-skip-ink and
		// text-decoration-skip-spaces all carry valid_for_marker: true in
		// css_properties.json5 @ 43cee02dc59fdad798675a735737510ecf0c9064.
		{"marker-line-break.html", "line-break", true},
		{"marker-text-decoration-skip-ink.html", "text-decoration-skip-ink", true},
		{"marker-text-decoration-skip-ink.html/skip-spaces", "text-decoration-skip-spaces", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := markerAllowedProperty(tc.prop)
			if got != tc.wantOK {
				t.Errorf("markerAllowedProperty(%q) = %v, want %v", tc.prop, got, tc.wantOK)
			}
		})
	}
}

// TestMarkerInheritedProperties_InheritFromParent asserts that
// ComputePseudoElementStyle propagates the six text-formatting properties
// from the originating element (parent) style onto the ::marker computed
// style. This models the "inherit" test cases in each WPT reftest.
//
// Named after the WPT test files they correspond to.
func TestMarkerInheritedProperties_InheritFromParent(t *testing.T) {
	// Build a minimal li node with a parent ol.
	olNode := &html.Node{Type: html.ElementNode, TagName: "ol"}
	liNode := &html.Node{Type: html.ElementNode, TagName: "li", Parent: olNode}
	olNode.Children = []*html.Node{liNode}

	// Build a parent style with all six properties set.
	parentStyle := NewStyle()
	parentStyle.Set("hyphens", "manual")
	parentStyle.Set("overflow-wrap", "anywhere")
	parentStyle.Set("tab-size", "1")
	parentStyle.Set("word-break", "break-all")
	parentStyle.Set("text-shadow", "#0f0 1px 2px 3px")
	parentStyle.Set("text-emphasis", "circle green")
	parentStyle.Set("text-emphasis-style", "circle")
	parentStyle.Set("text-emphasis-color", "green")
	parentStyle.Set("text-emphasis-position", "under right")
	parentStyle.Set("line-break", "anywhere")

	markerStyle := ComputePseudoElementStyle(liNode, "marker", nil, 800, 600, parentStyle)

	cases := []struct {
		testName string
		prop     string
		want     string
	}{
		{"marker-hyphens.html/inherit", "hyphens", "manual"},
		{"marker-overflow-wrap.html/inherit", "overflow-wrap", "anywhere"},
		{"marker-tab-size.html/inherit", "tab-size", "1"},
		{"marker-word-break.html/inherit", "word-break", "break-all"},
		{"marker-text-shadow.html/inherit", "text-shadow", "#0f0 1px 2px 3px"},
		{"marker-text-emphasis.html/inherit/shorthand", "text-emphasis", "circle green"},
		{"marker-text-emphasis.html/inherit/style", "text-emphasis-style", "circle"},
		{"marker-text-emphasis.html/inherit/color", "text-emphasis-color", "green"},
		{"marker-text-emphasis.html/inherit/position", "text-emphasis-position", "under right"},
		// LOU-358: CSS Text 3 §5.5 line-break is inherited.
		{"marker-line-break.html/inherit", "line-break", "anywhere"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.testName, func(t *testing.T) {
			got, ok := markerStyle.Get(tc.prop)
			if !ok {
				t.Errorf("marker style missing %q (want %q); property not inherited from parent", tc.prop, tc.want)
				return
			}
			if got != tc.want {
				t.Errorf("marker style %q = %q, want %q", tc.prop, got, tc.want)
			}
		})
	}
}

// TestGetLineBreak covers the line-break getter added for LOU-358 (mirrors
// GetHyphens): CSS Text 3 §5.5 values auto | loose | normal | strict |
// anywhere, defaulting to auto. Consumed by the line breaker's char-break
// dispatch (Blink LineBreaker::SetCurrentStyleForce maps LineBreak::kAnywhere
// to LineBreakType::kBreakCharacter, line_breaker.cc:4559-4563 @
// 43cee02dc59fdad798675a735737510ecf0c9064).
func TestGetLineBreak(t *testing.T) {
	s := NewStyle()
	if got := s.GetLineBreak(); got != "auto" {
		t.Errorf("GetLineBreak() default = %q, want %q", got, "auto")
	}
	for _, v := range []string{"auto", "loose", "normal", "strict", "anywhere"} {
		s.Set("line-break", v)
		if got := s.GetLineBreak(); got != v {
			t.Errorf("GetLineBreak() with %q = %q, want %q", v, got, v)
		}
	}
	s.Set("line-break", "bogus")
	if got := s.GetLineBreak(); got != "auto" {
		t.Errorf("GetLineBreak() with invalid value = %q, want %q", got, "auto")
	}
}
