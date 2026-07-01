package css

// Tests for LOU-349: cascade resolution for the ::target-text highlight
// pseudo-element (URL Text Fragments). Mirrors the existing ::selection
// pattern (selection_properties_test.go) and cascade.go's
// ComputePseudoElementStyle — ::target-text is a sibling highlight
// pseudo-element under the same CSS Pseudo-4 §highlight-styling /
// §highlight-painting rules ::selection follows.
//
// Allowed-property set: selectionAllowedProperty's own doc comment already
// claims the set is shared across ALL highlight pseudos (::selection,
// ::target-text, ::spelling-error, ::grammar-error, ::highlight()) per CSS
// Pseudo-4's single `valid_for_highlight` flag in Blink's
// css_properties.json5 — there is no PER-PSEUDO allowed-property variation
// in Blink for this flag, so target-text reuses selectionAllowedProperty
// directly rather than duplicating the property list (see
// targetTextAllowedProperty below).
//
// UA default colors: verified DIFFERENT from ::selection's, fetched fresh
// (2026-06-30) against Blink `main` branch source (exact commit SHA not
// pinned — see cascade.go's targetTextDefaultBackground doc comment for
// the same access-limitation note as text_fragment.go's Checkpoint 3
// citation). HighlightStyleUtils::DefaultBackgroundColor's kPseudoIdTargetText
// case returns Color::FromRGBA32(shared_highlighting::
// kFragmentTextBackgroundColorARGB), defined in
// components/shared_highlighting/core/common/fragment_directives_constants.cc
// as `0xFFE9D2FD` ("Light purple") — NOT ::selection's Highlight/
// HighlightText system colors. None of LOU-349's 14 target tests actually
// exercise the bare UA default (every one sets an explicit author
// ::target-text color/background-color rule), so this is verified for
// correctness/future-proofing rather than to satisfy any specific target
// test's pixels.

import (
	"louis14/pkg/html"
	"testing"
)

// TestTargetTextAllowedProperty mirrors TestSelectionAllowedProperty:
// ::target-text accepts the same highlight-allowed property subset as
// ::selection (see file doc comment), rejecting box-generation/layout
// properties like display/position/font-size.
func TestTargetTextAllowedProperty(t *testing.T) {
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
			got := targetTextAllowedProperty(tc.prop)
			if got != tc.wantOK {
				t.Errorf("targetTextAllowedProperty(%q) = %v, want %v", tc.prop, got, tc.wantOK)
			}
		})
	}
}

// TestTargetTextPseudoStyle_AppliesAllowedAndIgnoresDisallowed mirrors
// target-text-001.html's pattern: p::target-text { color: lime;
// background-color: green } should be picked up, and a disallowed property
// in the same rule must be silently ignored rather than applied.
func TestTargetTextPseudoStyle_AppliesAllowedAndIgnoresDisallowed(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		p { color: black; }
		p::target-text { color: lime; background-color: green; display: block; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "p"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	style := ComputePseudoElementStyle(node, "target-text", stylesheets, 800, 600, originatingStyle)

	if color, ok := style.Get("color"); !ok || color != "lime" {
		t.Errorf("::target-text color = %q, %v; want \"lime\", true", color, ok)
	}
	if bg, ok := style.Get("background-color"); !ok || bg != "green" {
		t.Errorf("::target-text background-color = %q, %v; want \"green\", true", bg, ok)
	}
	if display, ok := style.Get("display"); ok && display == "block" {
		t.Errorf("::target-text display = %q; disallowed property should have been ignored", display)
	}
}

// TestTargetTextPseudoStyle_TransparentOriginatingColorNotInherited mirrors
// target-text-007.html: the originating element sets color:transparent,
// ::target-text sets its own explicit color:black — the resolved
// ::target-text style must use black, not silently inherit/leak the
// originating element's transparent color.
func TestTargetTextPseudoStyle_TransparentOriginatingColorNotInherited(t *testing.T) {
	stylesheet, err := ParseStylesheet(`
		div { color: transparent; }
		::target-text { color: black; }
	`, nil)
	if err != nil {
		t.Fatalf("ParseStylesheet: %v", err)
	}
	stylesheets := []*Stylesheet{stylesheet}

	node := &html.Node{Type: html.ElementNode, TagName: "div"}
	originatingStyle := ComputeStyle(node, stylesheets, 800, 600, nil)

	style := ComputePseudoElementStyle(node, "target-text", stylesheets, 800, 600, originatingStyle)

	if color, ok := style.Get("color"); !ok || color != "black" {
		t.Errorf("::target-text color = %q, %v; want \"black\" (not inherited transparent)", color, ok)
	}
}

// TestTargetTextDefaultColorsDifferFromSelectionDefaults asserts the UA
// default background color for ::target-text is the verified
// 0xFFE9D2FD light-purple constant, distinct from ::selection's
// Highlight/HighlightText system-color pair (see file doc comment for the
// Blink citation). Guards against accidentally reusing
// selectionDefaultForeground/selectionDefaultBackground verbatim.
func TestTargetTextDefaultColorsDifferFromSelectionDefaults(t *testing.T) {
	if targetTextDefaultBackground == selectionDefaultBackground {
		t.Error("targetTextDefaultBackground must NOT equal selectionDefaultBackground — Blink uses a distinct light-purple (0xFFE9D2FD) constant for ::target-text, not the Highlight system color")
	}
	wantR, wantG, wantB := uint8(0xE9), uint8(0xD2), uint8(0xFD)
	if targetTextDefaultBackground.R != wantR || targetTextDefaultBackground.G != wantG || targetTextDefaultBackground.B != wantB {
		t.Errorf("targetTextDefaultBackground = %+v, want RGB(0xE9,0xD2,0xFD)", targetTextDefaultBackground)
	}
}

// TestTargetTextPseudoStyle_NoAuthorRuleUsesUADefaults mirrors
// applySelectionCascade's own paired-default test shape: with no author
// ::target-text rule at all, color/background-color must resolve to the
// UA target-text defaults (not transparent/unset, not ::selection's
// defaults).
func TestTargetTextPseudoStyle_NoAuthorRuleUsesUADefaults(t *testing.T) {
	node := &html.Node{Type: html.ElementNode, TagName: "p"}
	originatingStyle := ComputeStyle(node, nil, 800, 600, nil)

	style := ComputePseudoElementStyle(node, "target-text", nil, 800, 600, originatingStyle)

	bg, ok := style.Get("background-color")
	if !ok {
		t.Fatal("::target-text background-color unset with no author rule; want the UA default")
	}
	if bg == "transparent" {
		t.Error("::target-text background-color = transparent with no author rule; want the UA light-purple default")
	}
}
