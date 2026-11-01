package layout

import (
	"strings"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/text"
)

// TestLayout_LineHeightFontMetricUnitsUseCascadeFont (LOU-428): the layout
// engine hands the cascade its font selector, so an ex / ch / cap line-height
// computes to px against the element's real font (Ahem: x-height and
// cap-height 0.8em, "0" advance 1em) and a 40px child inherits that length.
// Blink: StyleResolverState::UpdateFont builds the font before line-height is
// converted (core/css/resolver/style_resolver_state.cc:553,
// core/css/resolver/style_cascade.cc:310-315 @
// 5992657f556bcd9d207a4aca187488462f027cab).
func TestLayout_LineHeightFontMetricUnitsUseCascadeFont(t *testing.T) {
	for _, tc := range []struct {
		lineHeight string
		want       float64 // div: font 10px Ahem
	}{
		{"calc(1em + 1ex)", 18},
		{"calc(1em + 1ch)", 20},
		{"calc(1em + 1cap)", 18},
		{"2ex", 16},
	} {
		doc, _ := html.Parse(`<style>div { font: 10px Ahem; line-height: ` + tc.lineHeight +
			`; } span { font-size: 40px; }</style><div><span>x</span></div>`)
		le := NewLayoutEngine(800, 600)
		le.Layout(doc)
		for node, style := range le.ComputedStyles() {
			switch node.TagName {
			case "div":
				if got := style.GetLineHeight(); got != tc.want {
					t.Errorf("line-height %s: div = %v, want %v", tc.lineHeight, got, tc.want)
				}
				if raw := style.Properties["line-height"]; !strings.HasSuffix(raw, "px") {
					t.Errorf("line-height %s: div computed value %q, want a px length", tc.lineHeight, raw)
				}
			case "span":
				if got := style.GetLineHeight(); got != tc.want {
					t.Errorf("line-height %s: span = %v, want the div's %v", tc.lineHeight, got, tc.want)
				}
			}
		}
	}
}

// TestLayoutNestedDocument_LineHeightFontMetricUnitsUseCascadeFont (LOU-428):
// a nested (iframe / object) document's cascade gets the same font selector,
// so an Ahem `line-height: 2ex` computes against Ahem's 0.8em x-height (16px
// at 10px) rather than the 0.5em fallback (10px). Blink resolves a child
// frame's styles through that document's own StyleResolver, which builds fonts
// the same way (StyleResolverState::UpdateFont,
// core/css/resolver/style_resolver_state.cc:553 @
// 5992657f556bcd9d207a4aca187488462f027cab). Guards engine.go's
// layoutNestedDocument selector: passing nil there gives 10 / 10.
func TestLayoutNestedDocument_LineHeightFontMetricUnitsUseCascadeFont(t *testing.T) {
	ctx := &LayoutContext{ViewportWidth: 800, ViewportHeight: 600, FontConfig: text.DefaultFontConfig()}
	doc, err := html.Parse(`<style>html, body { margin: 0; } div { font: 10px Ahem; line-height: 2ex; }</style><div>x</div>`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := layoutNestedDocument(ctx, doc, 300, 150)
	if res == nil || res.Result == nil || res.Result.Fragment == nil {
		t.Fatal("layoutNestedDocument returned no fragment")
	}
	var div *PhysicalFragment
	var find func(f *PhysicalFragment)
	find = func(f *PhysicalFragment) {
		if f.Node != nil && f.Node.TagName == "div" && div == nil {
			div = f
		}
		for _, c := range f.Children {
			find(c.Fragment)
		}
	}
	find(res.Result.Fragment)
	if div == nil || div.Style == nil {
		t.Fatal("nested document has no styled div fragment")
	}
	if got := div.Style.GetLineHeight(); got != 16 {
		t.Errorf("nested div line-height = %v, want 16 (2 × Ahem's 0.8em x-height at 10px)", got)
	}
	if got := div.Size.Height.Float64(); got != 16 {
		t.Errorf("nested div height = %v, want one 16px line", got)
	}
}
