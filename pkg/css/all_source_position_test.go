package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestAllAppliesAtSourcePosition: within one declaration block, `all`
// applies at its own source position. It replaces the declarations of the
// properties it covers that precede it, and later ones override it
// (declarations kept in source order in CSSPropertyValueSet; All::
// ParseShorthand sets every longhand but direction and unicode-bidi @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). An earlier !important
// declaration still beats a later normal `all` (CSS Cascade 4 §6.4).
func TestAllAppliesAtSourcePosition(t *testing.T) {
	for _, tc := range []struct{ decl, prop, want string }{
		{"margin-left:5px; all:initial", "margin-left", ""},
		{"all:initial; margin-left:5px", "margin-left", "5px"},
		{"color:red; all:inherit", "color", "green"},
		{"all:inherit; color:red", "color", "red"},
		{"margin-left:5px !important; all:initial", "margin-left", "5px"},
		{"margin-left:5px; all:initial !important", "margin-left", ""},
		{"margin-left:5px !important; all:initial !important", "margin-left", ""},
		{"direction:rtl; all:initial", "direction", "rtl"},
		{"--x:1; all:initial", "--x", "1"},
	} {
		doc, _ := html.Parse(`<style>.p{color:green} .t{` + tc.decl + `}</style><div class=p><div class=t></div></div>`)
		found := false
		for n, st := range ApplyStylesToDocument(doc, 800, 600) {
			if cls, _ := n.GetAttribute("class"); n.Type == html.ElementNode && cls == "t" {
				found = true
				if got, _ := st.Get(tc.prop); got != tc.want {
					t.Errorf("%s: %s = %q, want %q", tc.decl, tc.prop, got, tc.want)
				}
			}
		}
		if !found {
			t.Fatalf("%s: element not found", tc.decl)
		}
	}
}

// TestAllCoversEveryAffectedLonghand: `all` sets every longhand except
// direction and unicode-bidi (affected_by_all: false,
// css_properties.json5:1393, :6686; ParseCSSWideKeyword expands it through
// AddExpandedPropertyForValue, css_property_parser.cc:447-469,
// css_parsing_utils.cc:4347-4358, all @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the logical longhands, and the
// longhands of a var() shorthand, too, within a block, across rules, in a
// style attribute and in an @container rule.
func TestAllCoversEveryAffectedLonghand(t *testing.T) {
	fresh := func(prop string) string {
		v, _ := styleByID(t, cascadeHTML(t, `<div id="f"></div>`, nil), "f").Get(prop)
		return v
	}
	get := func(src, prop string) string {
		v, _ := styleByID(t, cascadeHTML(t, `<div id="p">`+src+`</div>`, nil), "t").Get(prop)
		return v
	}
	for _, tc := range []struct{ name, src, prop, want string }{
		// A later vertical writing-mode maps a surviving inline-size onto
		// height.
		{"logical size in a block", `<style>#t{inline-size:50px; all:initial; writing-mode:vertical-rl}</style><div id="t"></div>`, "height", fresh("height")},
		{"logical size across rules", `<style>#t{inline-size:50px} #t.t{all:initial; writing-mode:vertical-rl}</style><div id="t" class="t"></div>`, "height", fresh("height")},
		{"logical margin in a block", `<style>#t{margin-inline-start:5px; all:initial}</style><div id="t"></div>`, "margin-left", fresh("margin-left")},
		{"logical border across rules", `<style>#t{border-inline-start-width:5px; border-inline-start-style:solid} #t.t{all:initial}</style><div id="t" class="t"></div>`, "border-left-width", fresh("border-left-width")},
		{"var() shorthand in a block", `<style>#t{--m:3px; margin:var(--m); all:initial}</style><div id="t"></div>`, "margin-left", fresh("margin-left")},
		{"var() shorthand across rules", `<style>#t{--m:3px; margin:var(--m)} #t.t{all:initial}</style><div id="t" class="t"></div>`, "margin-left", fresh("margin-left")},
		{"style attribute logical size", `<div id="t" style="inline-size:50px; all:initial; writing-mode:vertical-rl"></div>`, "height", fresh("height")},
		{"style attribute later longhand", `<div id="t" style="all:initial; margin-left:5px"></div>`, "margin-left", "5px"},
		{"unset inherits an inherited longhand", `<style>#p{color:green} #t{color:red; all:unset}</style><div id="t"></div>`, "color", "green"},
		{"unset resets a non-inherited longhand", `<style>#t{margin-left:5px; all:unset}</style><div id="t"></div>`, "margin-left", fresh("margin-left")},
		{"revert rolls back to the UA", `<style>#t{display:inline; all:revert}</style><div id="t"></div>`, "display", fresh("display")},
		{"revert-layer rolls back to the layer", `<style>@layer a { #t{margin-left:3px} } #t{margin-left:5px; all:revert-layer}</style><div id="t"></div>`, "margin-left", "3px"},
		{"important all beats a later normal longhand", `<style>#t{all:initial !important; margin-left:5px}</style><div id="t"></div>`, "margin-left", fresh("margin-left")},
	} {
		if got := get(tc.src, tc.prop); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
		}
	}

	// An @container rule's `all` applies before the rule's other
	// declarations, as in a rule block; repeated so a map-order dependence
	// cannot pass by chance.
	src := `<style>#p{container-type:inline-size; width:500px}
		@container (min-width: 100px) { #t{all:initial; margin-left:5px; margin-right:6px; padding-left:7px; padding-right:8px} }</style>
		<div id="t"></div>`
	for i := 0; i < 20; i++ {
		st := styleByID(t, cascadeHTML(t, `<div id="p">`+src+`</div>`, nil), "t")
		for prop, want := range map[string]string{"margin-left": "5px", "margin-right": "6px", "padding-left": "7px", "padding-right": "8px"} {
			if got, _ := st.Get(prop); got != want {
				t.Fatalf("@container all then %s (run %d): %s = %q, want %q", prop, i, prop, got, want)
			}
		}
	}
}
