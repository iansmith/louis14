package css

import "testing"

// The UA bidi rules (html.css:1801-1817 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) isolate the listed block-level
// elements and any element with a valid dir, give <bdo> isolate-override
// with or without dir, and give pre / textarea with dir=auto plaintext.
// The dir presentational hint sets unicode-bidi only for dir=auto on other
// elements: plaintext for input type search/tel/url/email, else isolate
// (HTMLElement::CollectStyleForPresentationAttribute and
// UnicodeBidiAttributeForDirAuto, core/html/html_element.cc:295-315,
// 424-445 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestUABidiRendering(t *testing.T) {
	for _, tc := range []struct{ html, want string }{
		{`<div id="t"></div>`, "isolate"},
		{`<ul><li id="t"></li></ul>`, "isolate"},
		{`<table><tr><td id="t"></td></tr></table>`, "isolate"},
		{`<bdi id="t"></bdi>`, "isolate"},
		{`<span id="t" dir="RTL"></span>`, "isolate"},
		{`<span id="t" dir="auto"></span>`, "isolate"},
		{`<bdo id="t"></bdo>`, "isolate-override"},
		{`<bdo id="t" dir="rtl"></bdo>`, "isolate-override"},
		{`<pre id="t" dir="auto"></pre>`, "plaintext"},
		{`<textarea id="t" dir="auto"></textarea>`, "plaintext"},
		{`<input id="t" type="search" dir="auto">`, "plaintext"},
		{`<input id="t" type="text" dir="auto">`, "isolate"},
		{`<input id="t" type="SEARCH" dir="auto">`, "plaintext"},
		// The type keyword matches ASCII case-insensitively only: U+0130
		// is no `i`.
		{`<input id="t" type="EMAİL" dir="auto">`, "isolate"},
		{`<bdo id="t" dir="auto"></bdo>`, "isolate-override"},
		{`<pre id="t" dir="AUTO"></pre>`, "plaintext"},
		{`<textarea id="t" dir="Auto"></textarea>`, "plaintext"},
		{`<style>#t { unicode-bidi: normal }</style><div id="t"></div>`, "normal"},
		// Only HTML elements map dir to unicode-bidi: MathMLElement maps
		// ltr|rtl to direction alone (mathml_element.cc:35-38, 87-91 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), SVGElement nothing.
		{`<math><mi id="t" dir="auto">x</mi></math>`, "normal"},
		{`<svg><g id="t" dir="auto"></g></svg>`, "normal"},
		{`<span id="t"></span>`, "normal"},
		{`<span id="t" dir="bogus"></span>`, "normal"},
	} {
		got, ok := styleByID(t, cascadeHTML(t, tc.html, nil), "t").Get("unicode-bidi")
		if !ok {
			got = "normal"
		}
		if got != tc.want {
			t.Errorf("%s unicode-bidi = %q, want %q", tc.html, got, tc.want)
		}
	}
}

// A dir value is valid only as exactly ltr, rtl or auto, ASCII
// case-insensitively: a padded value is invalid, so it gets no isolate
// (IsValidDirAttribute, core/html/html_element.cc:357-361 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Its direction:
// TestDirHintPaddedValueInherits.
func TestDirHintExactValue(t *testing.T) {
	st := styleByID(t, cascadeHTML(t, `<span id="t" dir=" rtl "></span>`, nil), "t")
	if got, ok := st.Get("unicode-bidi"); ok && got != "normal" {
		t.Errorf(`dir=" rtl " unicode-bidi = %q, want normal`, got)
	}
}

// dir maps to direction on HTML and MathML elements only, and on HTML
// <body> an invalid value is ltr rather than inherited
// (HTMLElement::CollectStyleForPresentationAttribute,
// core/html/html_element.cc:438-443; MathMLElement, core/mathml/
// mathml_element.cc:87-91; none on SVGElement; all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestDirHintDirectionByNamespace(t *testing.T) {
	for _, tc := range []struct {
		html, id string
		want     Direction
	}{
		{`<div dir="rtl"><math><mi id="t" dir="ltr">x</mi></math></div>`, "t", DirectionLTR},
		{`<div dir="rtl"><svg><g id="t" dir="ltr"></g></svg></div>`, "t", DirectionRTL},
		{`<html dir="rtl"><body id="t" dir="bogus"></body></html>`, "t", DirectionLTR},
	} {
		if got := styleByID(t, cascadeHTML(t, tc.html, nil), tc.id).GetDirection(); got != tc.want {
			t.Errorf("%s: direction = %v, want %v", tc.html, got, tc.want)
		}
	}
}
