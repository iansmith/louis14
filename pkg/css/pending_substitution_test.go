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
		for _, lh := range shorthandLonghands[tc.shorthand] {
			want, _ := literal.Get(lh)
			if got, _ := viaVar.Get(lh); got != want {
				t.Errorf("%s: var(--v) with --v:%s: %s = %q, want %q (as the literal)", tc.shorthand, tc.value, lh, got, want)
			}
		}
	}
}
