package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestFontShorthandCSSWideKeyword pins that a CSS-wide keyword on the `font`
// shorthand applies to every font longhand, overriding a less specific
// `font` declaration. Blink's font shorthand parser accepts the keyword for
// all its longhands (css_properties.json5:9642-9650 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Without it `font: inherit` was
// dropped and the child kept the `div` rule's 1.25em (WPT
// multicol-nested-margin-002's reference). Chrome 159: the child's em is
// 20px, so 2em x 3em = 40x60.
func TestFontShorthandCSSWideKeyword(t *testing.T) {
	doc, _ := html.Parse(`<style>
		div { font: 1.25em/1 serif; }
		div > div { font: inherit; }
	</style><body style="margin:0"><div><div class="child"></div></div></body>`)
	styles := ApplyStylesToDocument(doc, 800, 600)
	found := false
	for node, style := range styles {
		if cls, _ := node.GetAttribute("class"); cls == "child" {
			found = true
			if got := style.GetFontSize(); got != 20 {
				t.Errorf("child font-size = %v, want 20 (inherited)", got)
			}
			if got := style.GetLineHeight(); got != 20 {
				t.Errorf("child line-height = %v, want 20 (inherited)", got)
			}
		}
	}
	if !found {
		t.Fatal("child not styled")
	}
}
