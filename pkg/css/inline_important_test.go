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

// TestInlineStyleImportantRound1: the style attribute is parsed by the same
// declaration-list parser as a rule and cascaded as an inline entry in both
// the normal and the important passes (CascadePriority is_inline_style,
// StyleCascade @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInlineStyleImportantRound1(t *testing.T) {
	initial := func(prop string) string {
		v, _ := styleByID(t, cascadeHTML(t, `<div id="f"></div>`, nil), "f").Get(prop)
		return v
	}
	for _, tc := range []struct{ name, css, inline, prop, want string }{
		// An inline `all !important` overrides author !important and
		// animations, deleted longhands included.
		{"inline all important beats author important", `#t{margin-left:1px !important}`, `all:initial !important`, "margin-left", initial("margin-left")},
		{"inline all important beats an animation", `@keyframes k{from,to{margin-left:9px}} #t{animation:k 1s}`, `all:initial !important`, "margin-left", initial("margin-left")},
		// Malformed priorities drop the declaration.
		{"bare !important is dropped", `#t{margin-left:3px}`, `margin-left: !important`, "margin-left", "3px"},
		{"doubled !important is dropped", `#t{margin-left:3px}`, `margin-left:5px !important !important`, "margin-left", "3px"},
		// `! important` with whitespace is an important declaration.
		{"spaced ! important is honoured", `#t{margin-left:1px !important}`, `margin-left:5px ! important`, "margin-left", "5px"},
		// An author `all !important` shields every longhand it covers.
		{"author all important beats inline normal", `#t{all:initial !important}`, `margin-left:5px`, "margin-left", initial("margin-left")},
		{"author all important beats an animation", `@keyframes k{from,to{margin-left:9px}} #t{animation:k 1s} #t.t{all:initial !important}`, ``, "margin-left", initial("margin-left")},
		// The logical-to-physical remap respects importance.
		{"inline important logical beats author important physical", `#t{direction:rtl; margin-right:1px !important}`, `margin-inline-start:5px !important`, "margin-right", "5px"},
		{"author important physical beats inline normal logical", `#t{direction:rtl; margin-right:1px !important}`, `margin-inline-start:5px`, "margin-right", "1px"},
	} {
		st := styleByID(t, cascadeHTML(t, `<style>`+tc.css+`</style><div id="t" class="t" style="`+tc.inline+`"></div>`, nil), "t")
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
		}
	}
}
