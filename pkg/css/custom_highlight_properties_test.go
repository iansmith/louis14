package css

// Tests for LOU-354: cascade resolution for the ::highlight(name) custom
// highlight pseudo-element (CSS Custom Highlight API). Mirrors the existing
// ::selection / ::target-text pattern (selection_properties_test.go,
// target_text_properties_test.go) and cascade.go's
// ComputePseudoElementStyle — ::highlight(name) is a sibling highlight
// pseudo-element under the same CSS Pseudo-4 §highlight-styling rules, with
// pseudoElement stored as "highlight(name)" by the selector parser (see
// stylesheet.go's parseSelector, ~line 3668-3700 — pre-existing scaffolding,
// no parser changes needed for this ticket).
//
// Allowed-property set: shared with ::selection/::target-text via
// highlightAllowedProperty delegating to selectionAllowedProperty (see that
// function's doc comment — Blink's `valid_for_highlight` flag is a single,
// pseudo-element-agnostic flag covering ::highlight() too).
//
// UA default colors: UNLIKE ::selection/::target-text, ::highlight() has NO
// UA-forced color pair. Verified against Blink's HighlightStyleUtils
// (third_party/blink/renderer/core/highlight/highlight_style_utils.cc):
// DefaultBackgroundColor's kPseudoIdHighlight case returns
// Color::kTransparent unconditionally; DefaultForegroundColor returns
// std::nullopt. So an unauthored ::highlight(name) rule must leave
// color/background-color UNSET on the computed style (no
// applySelectionCascade-style paired-default forcing) — TestCustomHighlightPseudoStyle_NoAuthorRuleLeavesColorsUnset
// below guards against accidentally copying that machinery.

import (
	"louis14/pkg/html"
	"testing"
)

// TestHighlightAllowedProperty mirrors TestSelectionAllowedProperty /
// TestTargetTextAllowedProperty: ::highlight(name) accepts the same
// highlight-allowed property subset, rejecting box-generation/layout
// properties like display/position/font-size.
func TestHighlightAllowedProperty(t *testing.T) {
	cases := []struct {
		prop   string
		wantOK bool
	}{
		{"color", true},
		{"background-color", true},
		{"background", true},
		{"text-decoration", true},
		{"text-decoration-color", true},
		{"text-decoration-line", true},
		{"text-shadow", true},
		{"display", false},
		{"position", false},
		{"font-size", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.prop, func(t *testing.T) {
			got := highlightAllowedProperty(tc.prop)
			if got != tc.wantOK {
				t.Errorf("highlightAllowedProperty(%q) = %v, want %v", tc.prop, got, tc.wantOK)
			}
		})
	}
}

// TestCustomHighlightPseudoStyle_AppliesAllowedAndIgnoresDisallowed mirrors
// highlight-painting-currentcolor-001.html's pattern:
// p::highlight(a) { color: yellow; background-color: blue; display: block }
// should pick up color/background-color, and the disallowed display
// property must be silently ignored.
func TestCustomHighlightPseudoStyle_AppliesAllowedAndIgnoresDisallowed(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		p { color: black; }
		p::highlight(a) { color: yellow; background-color: blue; display: block; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "p"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	style := ComputePseudoElementStyle(node, "highlight(a)", stylesheets, 800, 600, originatingStyle)

	if color, ok := style.Get("color"); !ok || color != "yellow" {
		t.Errorf("::highlight(a) color = %q, %v; want \"yellow\", true", color, ok)
	}
	if bg, ok := style.Get("background-color"); !ok || bg != "blue" {
		t.Errorf("::highlight(a) background-color = %q, %v; want \"blue\", true", bg, ok)
	}
	if display, ok := style.Get("display"); ok && display == "block" {
		t.Errorf("::highlight(a) display = %q; disallowed property should have been ignored", display)
	}
}

// TestCustomHighlightPseudoStyle_NoAuthorRuleLeavesColorsUnset is the
// critical divergence-from-::selection/::target-text test: with NO author
// ::highlight(name) rule at all, color/background-color must be UNSET
// (absent) on the computed style — NOT forced to a UA highlight-color pair
// the way applySelectionCascade/applyTargetTextCascade force ::selection/
// ::target-text. Blink's HighlightStyleUtils::DefaultForegroundColor
// returns std::nullopt and DefaultBackgroundColor returns kTransparent
// (unconditionally, not as a cascade "default" that authoring can override
// the pairing of) for kPseudoIdHighlight — i.e. "leave unset" IS the
// correct unauthored behavior for ::highlight(). This guards against
// accidentally wiring isCustomHighlight into applySelectionCascade/
// applyHighlightPairedColorCascade the way isSelection/isTargetText are.
func TestCustomHighlightPseudoStyle_NoAuthorRuleLeavesColorsUnset(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, TagName: "p"}
	originatingStyle := ComputeStyle(node, nil, 800, 600, nil)

	style := ComputePseudoElementStyle(node, "highlight(foo)", nil, 800, 600, originatingStyle)

	if color, ok := style.Get("color"); ok {
		t.Errorf("::highlight(foo) color = %q with no author rule; want unset (Blink DefaultForegroundColor = nullopt)", color)
	}
	if bg, ok := style.Get("background-color"); ok {
		t.Errorf("::highlight(foo) background-color = %q with no author rule; want unset (Blink DefaultBackgroundColor = transparent, not a UA pair default)", bg)
	}
}

// TestCustomHighlightPseudoStyle_DistinctNamesAreIndependent guards that
// "highlight(a)" and "highlight(b)" resolve independently — a rule
// targeting one name must not leak onto the other, since both share the
// isCustomHighlight dispatch branch (unlike ::selection/::target-text,
// which are each a single fixed pseudoElement string).
func TestCustomHighlightPseudoStyle_DistinctNamesAreIndependent(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		::highlight(a) { color: yellow; }
		::highlight(b) { color: lime; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "div"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	styleA := ComputePseudoElementStyle(node, "highlight(a)", stylesheets, 800, 600, originatingStyle)
	styleB := ComputePseudoElementStyle(node, "highlight(b)", stylesheets, 800, 600, originatingStyle)

	if color, ok := styleA.Get("color"); !ok || color != "yellow" {
		t.Errorf("::highlight(a) color = %q, %v; want \"yellow\", true", color, ok)
	}
	if color, ok := styleB.Get("color"); !ok || color != "lime" {
		t.Errorf("::highlight(b) color = %q, %v; want \"lime\", true", color, ok)
	}
}
