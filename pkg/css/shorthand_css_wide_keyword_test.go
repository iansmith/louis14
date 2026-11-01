package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestShorthandCSSWideKeywordInherit pins that a bare CSS-wide keyword on a
// shorthand applies to every one of its longhands (CSS Cascade 4 §7.3: a
// shorthand set to a CSS-wide keyword sets each longhand to that keyword).
// Blink's shorthand parser handles this generically for every shorthand,
// before any per-shorthand grammar runs (CSSPropertyParser::ParseValueStart
// -> ConsumeCSSWideKeyword over shorthandForProperty's longhands,
// css_properties.json5 `longhands` @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Each case gives the parent explicit longhand values, has a less specific
// rule set the shorthand to something else on the child, and has the child's
// own rule set the shorthand to `inherit`. Every listed (physical) longhand
// must come out equal to the parent's.
func TestShorthandCSSWideKeywordInherit(t *testing.T) {
	cases := []struct {
		shorthand string
		parent    string // parent declarations
		other     string // competing less specific value for the shorthand
		want      map[string]string
	}{
		{"border", "border: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"border-top-width": "7px", "border-left-style": "dashed", "border-bottom-color": "rgb(1,2,3)"}},
		{"border-top", "border-top: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"border-top-width": "7px", "border-top-style": "dashed", "border-top-color": "rgb(1,2,3)"}},
		{"border-left", "border-left: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"border-left-width": "7px", "border-left-style": "dashed", "border-left-color": "rgb(1,2,3)"}},
		{"border-width", "border-width: 1px 2px 3px 4px", "9px",
			map[string]string{"border-top-width": "1px", "border-right-width": "2px", "border-bottom-width": "3px", "border-left-width": "4px"}},
		{"border-style", "border-style: dotted dashed solid double", "groove",
			map[string]string{"border-top-style": "dotted", "border-right-style": "dashed", "border-bottom-style": "solid", "border-left-style": "double"}},
		{"border-color", "border-color: red lime blue yellow", "green",
			map[string]string{"border-top-color": "red", "border-left-color": "yellow"}},
		{"column-rule", "column-rule: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"column-rule-width": "7px", "column-rule-style": "dashed", "column-rule-color": "rgb(1,2,3)"}},
		{"border-inline-start", "border-left: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"border-left-width": "7px", "border-left-style": "dashed", "border-left-color": "rgb(1,2,3)"}},
		{"border-inline", "border-left: 7px dashed rgb(1,2,3); border-right: 5px dotted rgb(4,5,6)", "1px solid green",
			map[string]string{"border-left-width": "7px", "border-right-style": "dotted", "border-right-color": "rgb(4,5,6)"}},
		{"border-block-end", "border-bottom: 7px dashed rgb(1,2,3)", "1px solid green",
			map[string]string{"border-bottom-width": "7px", "border-bottom-style": "dashed", "border-bottom-color": "rgb(1,2,3)"}},
		{"flex", "flex: 3 4 50px", "1 1 0",
			map[string]string{"flex-grow": "3", "flex-shrink": "4", "flex-basis": "50px"}},
		{"flex-flow", "flex-flow: column wrap", "row nowrap",
			map[string]string{"flex-direction": "column", "flex-wrap": "wrap"}},
		{"list-style", "list-style: inside square", "outside decimal",
			map[string]string{"list-style-position": "inside", "list-style-type": "square"}},
		{"text-emphasis", "text-emphasis: open dot blue", "filled circle green",
			map[string]string{"text-emphasis-style": "open dot", "text-emphasis-color": "blue"}},
		{"columns", "columns: 120px 3", "50px 2",
			map[string]string{"column-width": "120px", "column-count": "3"}},
	}
	for _, tc := range cases {
		t.Run(tc.shorthand, func(t *testing.T) {
			doc, _ := html.Parse(`<style>
				.parent { ` + tc.parent + ` }
				div div { ` + tc.shorthand + `: ` + tc.other + ` }
				div .child { ` + tc.shorthand + `: inherit }
			</style><body><div class="parent"><div class="child"></div></div></body>`)
			styles := ApplyStylesToDocument(doc, 800, 600)
			var child *Style
			for node, style := range styles {
				if cls, _ := node.GetAttribute("class"); cls == "child" {
					child = style
				}
			}
			if child == nil {
				t.Fatal("child not styled")
			}
			for prop, want := range tc.want {
				if got, _ := child.Get(prop); got != want {
					t.Errorf("%s: inherit → child %s = %q, want %q (parent's)", tc.shorthand, prop, got, want)
				}
			}
		})
	}
}
