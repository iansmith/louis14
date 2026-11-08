package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestCSSWideKeywordASCIICaseInsensitive: CSS-wide keywords match ASCII
// case-insensitively (CSS Syntax 3 §4.3, identifiers; Blink's
// css_parsing_utils::IsCSSWideKeyword @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). "İnitial" (U+0130) is not
// `initial`: substituted into color it is invalid at computed-value time,
// so color is unset and inherits the parent's green (WPT
// css-variables/variable-declaration-60). Only CSS whitespace trims, so a
// no-break space is part of the token.
func TestCSSWideKeywordASCIICaseInsensitive(t *testing.T) {
	for _, v := range []string{"İnitial", "ınitial", "İNHERIT", "unſet", "\u00a0initial"} {
		if _, wide := cssWideKeyword(v); wide {
			t.Errorf("cssWideKeyword(%q) = wide, want not a CSS-wide keyword", v)
		}
	}
	for _, v := range []string{"INITIAL", " Inherit ", "Revert-Layer"} {
		if _, wide := cssWideKeyword(v); !wide {
			t.Errorf("cssWideKeyword(%q) = not wide, want a CSS-wide keyword", v)
		}
	}
	doc, _ := html.Parse(`<style>.p { color: green } .c { --c: İnitial; color: var(--c) }</style><div class="p"><div class="c"></div></div>`)
	for node, style := range ApplyStylesToDocument(doc, 800, 600) {
		if cls, _ := node.GetAttribute("class"); node.Type == html.ElementNode && cls == "c" {
			if got, _ := style.Get("color"); got != "green" {
				t.Errorf(`--c: İnitial; color: var(--c): color = %q, want "green" (invalid at computed-value time, inherited)`, got)
			}
			return
		}
	}
	t.Fatal(".c not found")
}
