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
