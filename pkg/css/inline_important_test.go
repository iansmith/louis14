package css

import "testing"

// TestInlineStyleImportant: a style attribute's !important flags are
// honoured. Within the attribute an important declaration beats a later
// normal one, and inline important declarations sort above author important
// ones and animations; inline normal ones stay below author important ones
// (CascadePriority with is_inline_style, StyleCascade @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; CSS Cascade 5 §6.4).
func TestInlineStyleImportant(t *testing.T) {
	for _, tc := range []struct{ name, css, inline, want string }{
		{"in-block important beats later normal", ``, `margin-left:5px !important; margin-left:7px`, "5px"},
		{"in-block important shorthand beats later longhand", ``, `margin:0 !important; margin-left:5px`, "0"},
		{"in-block later important wins", ``, `margin-left:5px; margin-left:7px !important`, "7px"},
		{"inline important beats author important", `#t{margin-left:1px !important}`, `margin-left:5px !important`, "5px"},
		{"author important beats inline normal", `#t{margin-left:1px !important}`, `margin-left:5px`, "1px"},
		{"inline important beats an animation", `@keyframes k{from,to{margin-left:9px}} #t{animation:k 1s}`, `margin-left:5px !important`, "5px"},
	} {
		st := styleByID(t, cascadeHTML(t, `<style>`+tc.css+`</style><div id="t" style="`+tc.inline+`"></div>`, nil), "t")
		if got, _ := st.Get("margin-left"); got != tc.want {
			t.Errorf("%s: margin-left = %q, want %q", tc.name, got, tc.want)
		}
	}
}
