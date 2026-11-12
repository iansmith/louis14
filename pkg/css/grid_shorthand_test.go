package css

import (
	"slices"
	"strings"
	"testing"

	"louis14/pkg/html"
)

// TestGridShorthandSetsItsSixLonghands checks the grid shorthand's three
// forms against Blink's Grid::ParseShorthand
// (core/css/properties/shorthands/shorthands_custom.cc:3773 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a <'grid-template'> sets the
// template longhands and resets the auto ones; an auto-flow form sets the
// auto longhands and resets the template ones it does not name (LOU-849).
// An auto longhand the value does not name is an implicit initial value,
// which Blink adds as IsImplicitProperty::kImplicit and its serializer
// tells apart from a specified one (LOU-852).
func TestGridShorthandSetsItsSixLonghands(t *testing.T) {
	// Each case lists the longhands that differ from the reset values.
	resets := map[string]string{
		"grid-template-rows": "none", "grid-template-columns": "none",
		"grid-template-areas": "none", "grid-auto-flow": "initial",
		"grid-auto-rows": "initial", "grid-auto-columns": "initial",
	}
	tests := []struct {
		decl string
		want map[string]string
	}{
		{"grid: repeat(3, 10px) / repeat(2, 10px)", map[string]string{
			"grid-template-rows": "repeat(3, 10px)", "grid-template-columns": "repeat(2, 10px)"}},
		{"grid: none", nil},
		{"grid: auto-flow dense 20px / 10px 30px", map[string]string{
			"grid-template-columns": "10px 30px", "grid-auto-flow": "dense", "grid-auto-rows": "20px"}},
		{"grid: auto-flow / 10px", map[string]string{"grid-template-columns": "10px", "grid-auto-flow": "row"}},
		{"grid: 10px 20px / dense auto-flow 5px", map[string]string{
			"grid-template-rows": "10px 20px", "grid-auto-flow": "column dense", "grid-auto-columns": "5px"}},
		{"grid: 10px / auto-flow", map[string]string{
			"grid-template-rows": "10px", "grid-auto-flow": "column"}},
		{`grid: "a b" 10px "c d" / 1fr 1fr`, map[string]string{
			"grid-template-areas": `"a b" "c d"`, "grid-template-rows": "10px auto",
			"grid-template-columns": "1fr 1fr"}},
		{`grid: [p] "a" 10px [x] [y] "b"`, map[string]string{
			"grid-template-areas": `"a" "b"`, "grid-template-rows": "[p] 10px [x y] auto"}},
	}
	for _, tt := range tests {
		got := parseDeclarations(tt.decl).Declarations
		for lh, want := range resets {
			if w, ok := tt.want[lh]; ok {
				want = w
			}
			if got[lh] != want {
				t.Errorf("%s: %s = %q, want %q", tt.decl, lh, got[lh], want)
			}
		}
		if _, ok := got["grid"]; ok {
			t.Errorf("%s: the grid shorthand itself is stored: %v", tt.decl, got)
		}
	}
}

// TestGridShorthandsSerialize checks the CSSOM read-back of grid and
// grid-template against Blink's StylePropertySerializer::
// GetShorthandValueForGrid (core/css/style_property_serializer.cc:2822) and
// CSSOMUtils::ComputedValueForGridTemplateShorthand
// (core/css/cssom_utils.cc:139) @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b
// (LOU-852).
func TestGridShorthandsSerialize(t *testing.T) {
	tests := []struct{ decl, property, want string }{
		{"grid: 10px / 20px", "grid", "10px / 20px"},
		{"grid: none", "grid", "none"},
		{"grid: auto-flow dense 20px / 10px 30px", "grid", "auto-flow dense 20px / 10px 30px"},
		// A row auto-flow is the initial one, so this takes the
		// <'grid-template'> path, with grid-template-rows none.
		{"grid: auto-flow / 10px", "grid", "none / 10px"},
		{"grid: 10px / auto-flow", "grid", "10px / auto-flow"},
		{"grid: 10px 20px / dense auto-flow 5px", "grid", "10px 20px / auto-flow dense 5px"},
		{`grid: "a b" 10px / 1fr 1fr`, "grid", `"a b" 10px / 1fr 1fr`},
		{`grid: "a   b" "c d"`, "grid", `"a b" "c d"`},
		{"grid-template: 10px / 20px", "grid-template", "10px / 20px"},
		{"grid-template: none", "grid-template", "none"},
		{`grid-template: [p] "a" 10px [q] "b"`, "grid-template", `[p] "a" 10px [q] "b"`},
		{`grid-template: [] "a" 10px [] "b"`, "grid-template", `"a" 10px "b"`},
		// grid-auto-* set alongside grid-template cannot be expressed by grid.
		{"grid-template: 10px / 20px; grid-auto-flow: column; grid-auto-rows: 5px; grid-auto-columns: auto", "grid", ""},
	}
	for _, tt := range tests {
		got, _ := parseDeclarations(tt.decl).Value(tt.property)
		if got != tt.want {
			t.Errorf("%s: %s reads %q, want %q", tt.decl, tt.property, got, tt.want)
		}
	}
}

// TestInvalidGridDeclarationsAreDropped checks that a value Blink's grid
// parsers reject sets nothing: Grid::ParseShorthand
// (shorthands_custom.cc:3773), ConsumeGridTemplateShorthand,
// ConsumeGridTrackList and ParseGridTemplateAreasRow
// (css_parsing_utils.cc:7598, :7379, :7475) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (LOU-852).
func TestInvalidGridDeclarationsAreDropped(t *testing.T) {
	invalid := []string{
		"grid: foo / bar",
		`grid: "a b" "c" / 1fr`,
		`grid: "a" foo`,
		`grid: "a" / auto-flow`,
		"grid: auto-flow dense dense / 1fr",
		"grid: 1fr / auto-flow 1fr auto-flow",
		`grid: "a" / repeat(auto-fill, 10px)`,
		`grid-template: "a b" "b a"`,
		"grid-template: 10px / 20px / 30px",
		"grid-template-rows: foo",
		"grid-template-rows: -10px",
		"grid-template-columns: repeat(auto-fill, 1fr)",
		"grid-template-columns: repeat(auto-fill, 10px) repeat(auto-fit, 10px)",
		"grid-template-columns: [a] [b] 10px",
		"grid-template-columns: minmax(1fr, 10px)",
		"grid-auto-rows: [a] 10px",
		"grid-auto-columns: repeat(2, 10px)",
		`grid-template-areas: "a b" "c"`,
		`grid-template-areas: "a b" "b a"`,
	}
	for _, decl := range invalid {
		if got := parseDeclarations(decl).Declarations; len(got) != 0 {
			t.Errorf("%s: want it dropped, got %v", decl, got)
		}
	}
	valid := []string{
		"grid-template-columns: repeat(auto-fill, minmax(10px, 1fr))",
		"grid-template-columns: [a] repeat(2, [b] 10px) fit-content(20%) [c d]",
		"grid-template-rows: subgrid [a] [] repeat(auto-fill, [b])",
		"grid-auto-rows: 10px minmax(auto, 1fr)",
		`grid-template-areas: "a a ." "b b ."`,
		"grid-template-rows: var(--x)",
		"grid-auto-columns: inherit",
		// An empty <line-names> is consumed and dropped, not a parse error
		// (ConsumeGridLineNames, css_parsing_utils.cc:7041-7076).
		"grid-template-columns: 1fr [] 1fr",
		"grid-template-columns: [] 1fr",
		`grid-template: [] "a" 10px [] / 1fr`,
		"grid-template-columns: repeat(2, [] 1fr [])",
		"grid-template-columns: subgrid [] [a]",
		`grid-template: "a" / [] 1fr []`,
	}
	for _, decl := range valid {
		prop, _, _ := strings.Cut(decl, ":")
		got := parseDeclarations(decl).Declarations
		// A shorthand is stored as its longhands.
		stored := func(p string) bool { _, ok := got[p]; return ok }
		if !stored(prop) && !slices.ContainsFunc(shorthandLonghands[prop], stored) {
			t.Errorf("%s: want it kept", decl)
		}
	}
}

// TestGridImplicitResetsComputeToInitial checks that the auto longhands a
// grid template value resets implicitly compute to their initial values:
// row flow and auto implicit tracks, replacing an earlier rule's values
// (LOU-852).
func TestGridImplicitResetsComputeToInitial(t *testing.T) {
	doc, _ := html.Parse(`<style>
		div { grid-auto-flow: column dense; grid-auto-rows: 7px; grid-auto-columns: 9px }
		.g { grid: 10px / 20px }
	</style><body><div class="g"></div></body>`)
	for node, style := range ApplyStylesToDocument(doc, 800, 600) {
		if cls, _ := node.GetAttribute("class"); cls != "g" {
			continue
		}
		if got := style.GetGridAutoFlow(); got != "row" {
			t.Errorf("grid-auto-flow computes to %q, want row", got)
		}
		if got := style.GetGridAutoRows(); got != nil && !got.Auto {
			t.Errorf("grid-auto-rows computes to %+v, want auto", *got)
		}
		if got := style.GetGridAutoColumns(); got != nil && !got.Auto {
			t.Errorf("grid-auto-columns computes to %+v, want auto", *got)
		}
		return
	}
	t.Fatal("grid element not styled")
}

// TestGridGettersReadWhatTheParserAccepts checks that the layout getters
// tokenize like the parser: a bracketed list of several line names, and
// area cells split by ParseGridTemplateAreasColumnNames, where a run of '.'
// is one null cell (LOU-852).
func TestGridGettersReadWhatTheParserAccepts(t *testing.T) {
	s := NewStyle()
	s.Set("grid-template-columns", "[a b] 10px [c] 20px")
	_, names := s.GetGridTemplateColumnsWithNames()
	for name, want := range map[string]int{"a": 1, "b": 1, "c": 2} {
		if names[name] != want {
			t.Errorf("line %q = %d, want %d (all: %v)", name, names[name], want, names)
		}
	}
	s.Set("grid-template-areas", `"a..b" "a..b"`)
	areas := s.GetGridTemplateAreas()
	want := map[string]GridAreaInfo{"a": {1, 3, 1, 2}, "b": {1, 3, 3, 4}}
	for name, w := range want {
		if areas[name] != w {
			t.Errorf("area %q = %+v, want %+v (all: %v)", name, areas[name], w, areas)
		}
	}
}
