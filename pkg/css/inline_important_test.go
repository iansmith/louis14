package css

import (
	"testing"

	"louis14/pkg/html"
)

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

// TestRulePriorityWithoutValueIsDropped: in a rule block too, a priority
// with no value before it is an invalid declaration.
func TestRulePriorityWithoutValueIsDropped(t *testing.T) {
	st := styleByID(t, cascadeHTML(t, `<style>#t{margin-left:3px} #t{margin-left: !important}</style><div id="t"></div>`, nil), "t")
	if got, _ := st.Get("margin-left"); got != "3px" {
		t.Errorf("margin-left: !important in a rule: margin-left = %q, want 3px", got)
	}
}

// TestLogicalLonghandsResolveAtTheirCascadePosition: a logical longhand
// cascades only as itself and maps onto its physical longhand at computed
// time for the element's writing-mode and direction, where the later of the
// two in cascade order (origin, importance, then order) wins
// (CSSProperty::ResolveDirectionAwareProperty; StyleCascade @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). It never sets the
// horizontal-tb LTR side as a declaration of its own.
func TestLogicalLonghandsResolveAtTheirCascadePosition(t *testing.T) {
	initial := func(prop string) string {
		v, _ := styleByID(t, cascadeHTML(t, `<div id="f"></div>`, nil), "f").Get(prop)
		return v
	}
	type want map[string]string
	for _, tc := range []struct {
		name, css, inline string
		want              want
	}{
		{"rtl: inline important logical leaves author important margin-left", `#t{direction:rtl; margin-left:1px !important}`, `margin-inline-start:5px !important`, want{"margin-left": "1px", "margin-right": "5px"}},
		{"vertical-rl: inline important logical leaves author important margin-left", `#t{writing-mode:vertical-rl; margin-left:1px !important}`, `margin-inline-start:5px !important`, want{"margin-left": "1px", "margin-top": "5px"}},
		{"vertical-rl: inline normal logical leaves author normal margin-left", `#t{writing-mode:vertical-rl; margin-left:1px}`, `margin-inline-start:5px`, want{"margin-left": "1px", "margin-top": "5px"}},
		{"later logical beats earlier physical", `#t{margin-left:1px} #t.t{margin-inline-start:5px}`, ``, want{"margin-left": "5px"}},
		{"later physical beats earlier logical", `#t.t{margin-left:1px} #t{margin-inline-start:5px}`, ``, want{"margin-left": "1px"}},
		{"later physical beats earlier logical size", `#t{writing-mode:vertical-rl; inline-size:50px; height:7px}`, ``, want{"height": "7px"}},
		{"inset-inline-start under rtl", `#t{direction:rtl; position:absolute; inset-inline-start:5px}`, ``, want{"right": "5px", "left": initial("left")}},
		{"logical initial resets the physical side", `#t{margin-left:5px; margin-inline-start:initial}`, ``, want{"margin-left": initial("margin-left")}},
		{"logical inherit takes the parent's physical side", `#p{margin-left:7px} #t{margin-inline-start:inherit}`, ``, want{"margin-left": "7px"}},
	} {
		st := styleByID(t, cascadeHTML(t, `<style>`+tc.css+`</style><div id="p"><div id="t" class="t" style="`+tc.inline+`"></div></div>`, nil), "t")
		for prop, w := range tc.want {
			if got, _ := st.Get(prop); got != w {
				t.Errorf("%s: %s = %q, want %q", tc.name, prop, got, w)
			}
		}
	}

	// A pseudo-element maps its logical longhands too.
	doc, _ := html.Parse(`<style>#t::before{content:"x"; direction:rtl; margin-inline-start:5px}</style><div id="t">y</div>`)
	styles := ApplyStylesToDocument(doc, 800, 600)
	for n, st := range styles {
		if id, _ := n.GetAttribute("id"); id == "t" {
			before := ComputePseudoElementStyle(n, "before", ParseDocumentStylesheets(doc), 800, 600, st)
			if got, _ := before.Get("margin-right"); got != "5px" {
				t.Errorf("::before rtl margin-inline-start: margin-right = %q, want 5px", got)
			}
		}
	}
}

// TestInlineStyleComments: comments in a style attribute are stripped as in
// a stylesheet, so a `!` or `;` inside one is not syntax.
func TestInlineStyleComments(t *testing.T) {
	for _, tc := range []struct{ inline, prop, want string }{
		{`color: green /* ! */`, "color", "green"},
		{`margin-left: 4px /* ; x */`, "margin-left", "4px"},
	} {
		st := styleByID(t, cascadeHTML(t, `<div id="t" style="`+tc.inline+`"></div>`, nil), "t")
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("style=%q: %s = %q, want %q", tc.inline, tc.prop, got, tc.want)
		}
	}
}
