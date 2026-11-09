package css

import (
	"testing"

	"louis14/pkg/html"
)

// TestPendingSubstitutionShorthands: a shorthand whose value contains var()
// sets each of its longhands to a pending-substitution value at the
// shorthand's own cascade priority and source position, substituted at
// computed-value time (CSSPendingSubstitutionValue; StyleCascade::
// ResolvePendingSubstitution @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// So a later or higher-priority longhand overrides it, and it overrides an
// earlier or lower-priority one.
func TestPendingSubstitutionShorthands(t *testing.T) {
	for _, tc := range []struct {
		name, css, prop, want string
	}{
		{"higher-priority longhand beats a var() shorthand", `.a{--x:1/1/2/2; grid-area:var(--x)} .b{grid-column:3}`, "grid-column-start", "3"},
		{"var() shorthand beats a lower-priority longhand", `.b{grid-column:3} .a.a{--x:1/1/2/2; grid-area:var(--x)}`, "grid-column-start", "1"},
		{"later longhand in the block beats a var() shorthand", `.a{--x:0; margin:var(--x); margin-left:5px}`, "margin-left", "5px"},
		{"later var() shorthand in the block beats a longhand", `.a{--x:0; margin-left:5px; margin:var(--x)}`, "margin-left", "0"},
		{"font: var() then font-size", `.a{--foo:0 Ahem; font:var(--foo); font-size:150px}`, "font-size", "150px"},
		{"invalid at computed-value time resets the longhands", `.a{margin-left:5px; margin:var(--missing)}`, "margin-left", ""},
		{"inline style later longhand beats a var() shorthand", ``, "margin-left", "5px"},
	} {
		src := `<style>` + tc.css + `</style><div class="a b"></div>`
		if tc.css == "" {
			src = `<div class="a b" style="--x:0; margin:var(--x); margin-left:5px"></div>`
		}
		doc, _ := html.Parse(src)
		found := false
		for n, st := range ApplyStylesToDocument(doc, 800, 600) {
			if cls, _ := n.GetAttribute("class"); n.Type != html.ElementNode || cls != "a b" {
				continue
			}
			found = true
			if got, _ := st.Get(tc.prop); got != tc.want {
				t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
			}
		}
		if !found {
			t.Fatalf("%s: element not found", tc.name)
		}
	}
}

// TestPendingSubstitutionMatchesLiteral: once substituted, a var()
// shorthand sets every longhand as the literal shorthand does on an element
// with no other declarations, so the longhands its value omits reset even
// over an earlier rule (vars-font-shorthand-001,
// vars-background-shorthand-001).
func TestPendingSubstitutionMatchesLiteral(t *testing.T) {
	computed := func(css string) *Style {
		doc, _ := html.Parse(`<style>` + css + `</style><div class="a b"></div>`)
		for n, st := range ApplyStylesToDocument(doc, 800, 600) {
			if cls, _ := n.GetAttribute("class"); n.Type == html.ElementNode && cls == "a b" {
				return st
			}
		}
		t.Fatalf("element not found: %s", css)
		return nil
	}
	for _, tc := range []struct{ shorthand, earlier, value string }{
		{"font", "font:italic bold 30px serif; line-height:3", "0 Ahem"},
		{"font", "", "150px/1 Ahem, sans-serif"},
		{"background", "background:red url(x.png) no-repeat", "linear-gradient(green, green)"},
		{"background", "background:red", "green linear-gradient(red, red) center / 0 0"},
		{"border", "border:5px dotted red", "1px solid green"},
		{"list-style", "list-style:square inside", "circle"},
		{"flex", "flex:3 3 30px", "1"},
		{"margin", "margin:9px", "1px 2px"},
		{"grid-area", "grid-area:4/4/5/5", "1/2"},
	} {
		literal := computed(`.b{` + tc.shorthand + `:` + tc.value + `}`)
		viaVar := computed(`.a{` + tc.earlier + `} .b{--v:` + tc.value + `; ` + tc.shorthand + `:var(--v)}`)
		// An absent longhand is its initial value, which the var() form may
		// set explicitly so that an inherited longhand does not inherit.
		value := func(st *Style, lh string) string {
			if v, ok := st.Get(lh); ok {
				return v
			}
			return nonDefaultInitialValues[lh]
		}
		for _, lh := range shorthandLonghands[tc.shorthand] {
			want := value(literal, lh)
			if got := value(viaVar, lh); got != want {
				t.Errorf("%s: var(--v) with --v:%s: %s = %q, want %q (as the literal)", tc.shorthand, tc.value, lh, got, want)
			}
		}
	}
}

// TestPendingSubstitutionResolvedBeforeKeywordsAndInheritance: pending
// substitutions resolve inside the element's (and a pseudo-element's)
// cascade, after custom properties and before CSS-wide keywords and
// inheritance (StyleCascade::Apply resolves the pending value as it applies
// the longhand, then CSS-wide keywords and inheritance follow, @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestPendingSubstitutionResolvedBeforeKeywordsAndInheritance(t *testing.T) {
	initial := func(prop string) string {
		v, _ := styleByID(t, cascadeHTML(t, `<div id="f"></div>`, nil), "f").Get(prop)
		return v
	}
	for _, tc := range []struct{ name, css, prop, want string }{
		// A CSS-wide keyword from a var() fallback is applied, not stored.
		{"fallback inherit (font)", `#p{font-size:30px} #t{font:var(--nope, inherit)}`, "font-size", "30px"},
		{"fallback inherit (margin)", `#p{margin-left:7px} #t{margin:var(--nope, inherit)}`, "margin-left", "7px"},
		{"fallback initial", `#t{margin-left:5px} #t.t{margin:var(--nope, initial)}`, "margin-left", initial("margin-left")},
		{"fallback unset", `#t{margin-left:5px} #t.t{margin:var(--nope, unset)}`, "margin-left", initial("margin-left")},
		// Invalid at computed-value time is unset: inherited properties
		// inherit, others take their initial value.
		{"IACVT inherits an inherited longhand", `#p{font-size:30px} #t{font:var(--nope)}`, "font-size", "30px"},
		{"IACVT resets a non-inherited longhand", `#t{margin-left:5px} #t.t{margin:var(--nope)}`, "margin-left", initial("margin-left")},
		// A cycle (--a ↔ --b) leaves var() unresolved: IACVT, never a fresh
		// pending value.
		{"cycle is IACVT", `#p{font-size:30px} #t{--a:var(--b); --b:var(--a); font:var(--a)}`, "font-size", "30px"},
		// A substituted longhand is validated as a declared value: an
		// invalid color makes the whole declaration IACVT.
		{"invalid substituted color is IACVT", `#t{border:5px solid red} #t.t{--c:1px solid notacolor; border:var(--c)}`, "border-left-width", initial("border-left-width")},
		{"invalid substituted color resets the color too", `#t{border:5px solid red} #t.t{--c:1px solid notacolor; border:var(--c)}`, "border-left-color", initial("border-left-color")},
		// The @container second pass follows the same rules.
		{"@container IACVT inherits", `#p{font-size:30px; container-type:inline-size; width:500px} #t{font-size:10px} @container (min-width: 100px) { #t{font:var(--nope)} }`, "font-size", "30px"},
		{"@container fallback inherit", `#p{font-size:30px; container-type:inline-size; width:500px} #t{font-size:10px} @container (min-width: 100px) { #t{font:var(--nope, inherit)} }`, "font-size", "30px"},
	} {
		st := styleByID(t, cascadeHTML(t, `<style>`+tc.css+`</style><div id="p"><div id="t" class="t"></div></div>`, nil), "t")
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, tc.prop, got, tc.want)
		}
	}

	// The cascaded-property record sees the resolved state: an IACVT
	// inherited longhand is inherited, not cascaded, so it does not block
	// transition propagation.
	st := styleByID(t, cascadeHTML(t, `<style>#p{font-size:30px} #t{font:var(--nope)}</style><div id="p"><div id="t"></div></div>`, nil), "t")
	if st.CascadedProps["font-size"] {
		t.Errorf("IACVT font: CascadedProps has font-size, want it inherited (not cascaded)")
	}
}

// TestPendingSubstitutionPseudoElements: pseudo-elements resolve pending
// substitutions in their own cascade: ::before, ::marker, ::first-line and
// the highlight ::selection.
func TestPendingSubstitutionPseudoElements(t *testing.T) {
	src := `<style>#c{--c:green; --f:italic 20px serif; display:list-item}
		#c::before{content:"x"; background:var(--c)}
		#c::selection{background:var(--c)}
		#c::marker{font:var(--f)}
		#c::first-line{font:var(--f)}</style><div id="c">y</div>`
	doc, _ := html.Parse(src)
	styles := ApplyStylesToDocument(doc, 800, 600)
	var c *html.Node
	for n := range styles {
		if id, _ := n.GetAttribute("id"); id == "c" {
			c = n
		}
	}
	sheets := ParseDocumentStylesheets(doc)
	for _, tc := range []struct{ pseudo, prop, want string }{
		{"before", "background-color", "green"},
		{"selection", "background-color", "green"},
		{"marker", "font-style", "italic"},
		{"first-line", "font-style", "italic"},
	} {
		st := ComputePseudoElementStyle(c, tc.pseudo, sheets, 800, 600, styles[c])
		if got, _ := st.Get(tc.prop); got != tc.want {
			t.Errorf("::%s %s = %q, want %q", tc.pseudo, tc.prop, got, tc.want)
		}
	}
}
