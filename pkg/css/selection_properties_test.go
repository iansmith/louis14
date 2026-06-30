package css

// Tests for LOU-344: cascade resolution for the ::selection highlight
// pseudo-element. Mirrors the existing ::marker pattern in
// marker_properties_test.go and cascade.go's ComputePseudoElementStyle.
//
// Allowed-property list verified fresh (2026-06-30) against Blink
// css_properties.json5 `valid_for_highlight: true` flags, fetched at
// chromium/chromium GitHub mirror commit
// 15bace522cdc76c59f8dec8cea1aeb6fe25a8e36 (blob
// 8525eaca74436165e8403db0557ba6d8a2ec35f for css_properties.json5):
// color, background-color, fill, stroke, stroke-width,
// text-decoration-color/-line/-skip-ink/-skip-spaces/-style/-thickness,
// text-underline-offset, text-shadow, text-emphasis-color (plus the
// UA-internal -internal-visited-* counterparts, not author-facing). Notably
// NOT in the verified set despite being in LOU-344's conservative fallback
// list: caret-color, fill-opacity, stroke-opacity, -webkit-text-stroke-color,
// -webkit-text-stroke-width — none carry valid_for_highlight in current
// Blink, so they are excluded here.

import (
	"louis14/pkg/html"
	"testing"
)

// TestSelectionAllowedProperty asserts the highlight-allowed property set
// matches the verified Blink valid_for_highlight flags (see file doc
// comment for the SHA). One representative case per property plus a
// disallowed control (display, explicitly excluded per CSS Pseudo-4
// §highlight-styling — ::selection cannot change box generation).
func TestSelectionAllowedProperty(t *testing.T) {
	cases := []struct {
		prop   string
		wantOK bool
	}{
		{"color", true},
		{"background-color", true},
		{"fill", true},
		{"stroke", true},
		{"stroke-width", true},
		{"text-decoration-color", true},
		{"text-decoration-line", true},
		{"text-decoration-skip-ink", true},
		{"text-decoration-skip-spaces", true},
		{"text-decoration-style", true},
		{"text-decoration-thickness", true},
		{"text-underline-offset", true},
		{"text-shadow", true},
		{"text-emphasis-color", true},
		// Disallowed: not in Blink's valid_for_highlight set.
		{"display", false},
		{"caret-color", false},
		{"fill-opacity", false},
		{"stroke-opacity", false},
		{"-webkit-text-stroke-color", false},
		{"-webkit-text-stroke-width", false},
		{"position", false},
		{"font-size", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.prop, func(t *testing.T) {
			got := selectionAllowedProperty(tc.prop)
			if got != tc.wantOK {
				t.Errorf("selectionAllowedProperty(%q) = %v, want %v", tc.prop, got, tc.wantOK)
			}
		})
	}
}

// TestSelectionPseudoStyle_AppliesAllowedAndIgnoresDisallowed mirrors
// active-selection-011.html's pattern: div::selection { color: green }
// should be picked up, and a disallowed property in the same rule must be
// silently ignored rather than applied.
func TestSelectionPseudoStyle_AppliesAllowedAndIgnoresDisallowed(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		div { color: red; }
		div::selection { color: green; display: block; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "div"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	selStyle := ComputePseudoElementStyle(node, "selection", stylesheets, 800, 600, originatingStyle)

	if color, ok := selStyle.Get("color"); !ok || color != "green" {
		t.Errorf("::selection color = %q, %v; want \"green\", true", color, ok)
	}
	if display, ok := selStyle.Get("display"); ok && display == "block" {
		t.Errorf("::selection display = %q; disallowed property should have been ignored", display)
	}
}

// TestSelectionPseudoStyle_DoesNotInheritOriginatingColor verifies
// ::selection cascades against its own UA baseline rather than inheriting
// the originating element's computed color/background-color — mirrors how
// ::marker/::first-line don't inherit color from the originating element's
// *used* color either, only via the explicit inheritableProps allowlist in
// ComputePseudoElementStyle. The originating div sets color:transparent;
// ::selection (no author rule) must NOT pick up "transparent" as its
// color — that would defeat the highlight entirely, contradicting CSS
// Pseudo-4 §highlight-styling's "cascade against the UA default" model
// (see HighlightStyleUtils::DefaultForegroundColor /
// UseDefaultHighlightColors in Blink's
// third_party/blink/renderer/core/highlight/highlight_style_utils.cc,
// fetched at the SHA in this file's doc comment).
func TestSelectionPseudoStyle_DoesNotInheritOriginatingColor(t *testing.T) {
	stylesheet, err := ParseStylesheet(`div { color: transparent; }`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "div"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	selStyle := ComputePseudoElementStyle(node, "selection", stylesheets, 800, 600, originatingStyle)

	if color, ok := selStyle.Get("color"); ok && color == "transparent" {
		t.Errorf("::selection color = %q; should not inherit originating element's color, want a UA default", color)
	}
}
