package css

import "testing"

// TestTableRulesAttribute checks the <table rules> presentational hints of
// Blink's HTMLTableElement (core/html/html_table_element.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a valid rules value collapses
// borders (:373-379) and, with no border attribute, hides the table's own
// border (AdditionalPresentationAttributeStyle, :488-501); rules=groups
// gives each row group thin solid top and bottom borders
// (AdditionalGroupStyle, :612-643, via HTMLTableSectionElement::
// AdditionalPresentationAttributeStyle) and each column group thin solid
// left and right borders (HTMLTableColElement::
// AdditionalPresentationAttributeStyle, html_table_col_element.cc:90-97);
// author CSS beats every hint; rows / cols / all give the cells
// their borders (CreateSharedCellStyle, :536-595, via GetCellBorders,
// :515-534) (LOU-904).
func TestTableRulesAttribute(t *testing.T) {
	// Cell borders take border-color: inherit; the rows carry a colour.
	styles := cascadeHTML(t, `<style>tr { border-color: red } #ov { border-top: 3px dashed blue }</style>
		<table id="g" rules="GROUPS"><colgroup id="cg"><col id="col"></colgroup>
			<thead id="gh"><tr><td>x</td></tr></thead><tbody id="gb"><tr><td id="gd">x</td></tr></tbody>
			<tfoot id="gf"><tr><td>x</td></tr></tfoot><tbody id="ov"><tr><td>x</td></tr></tbody></table>
		<table id="gbord" rules="groups" border="2"><tr><td>x</td></tr></table>
		<table id="r" rules="rows"><tr><td id="rd">x</td></tr></table>
		<table id="c" rules="cols"><tr><td id="cd">x</td></tr></table>
		<table id="a" rules="all"><tr><td id="ad">x</td></tr></table>
		<table id="fr" rules="groups" frame="VOID"><tr><td>x</td></tr></table>
		<table id="n" rules="none"><tr><td id="nd">x</td></tr></table>
		<table id="bad" rules="bogus"><tbody id="badb"><tr><td>x</td></tr></tbody></table>`, nil)
	for _, tt := range []struct {
		id   string
		want map[string]string // "" = unset
	}{
		{"g", map[string]string{"border-collapse": "collapse", "border-top-style": "hidden", "border-left-style": "hidden"}},
		{"gb", map[string]string{"border-top-width": "1px", "border-top-style": "solid", "border-bottom-width": "1px", "border-bottom-style": "solid", "border-left-style": ""}},
		{"gh", map[string]string{"border-top-style": "solid", "border-bottom-width": "1px"}},
		{"gf", map[string]string{"border-top-style": "solid", "border-bottom-width": "1px"}},
		{"ov", map[string]string{"border-top-style": "dashed", "border-top-width": "3px", "border-bottom-style": "solid"}},
		{"cg", map[string]string{"border-left-width": "1px", "border-left-style": "solid", "border-right-style": "solid", "border-top-style": ""}},
		{"col", map[string]string{"border-left-style": ""}},
		{"gd", map[string]string{"border-top-style": "", "border-left-style": ""}},
		{"gbord", map[string]string{"border-collapse": "collapse"}},
		{"rd", map[string]string{"border-top-width": "1px", "border-top-style": "solid", "border-bottom-style": "solid", "border-top-color": "red", "border-left-style": ""}},
		{"cd", map[string]string{"border-left-width": "1px", "border-left-style": "solid", "border-right-style": "solid", "border-left-color": "red", "border-top-style": ""}},
		{"ad", map[string]string{"border-top-width": "1px", "border-left-style": "solid", "border-bottom-color": "red"}},
		{"fr", map[string]string{"border-collapse": "collapse", "border-top-style": ""}},
		{"n", map[string]string{"border-collapse": "collapse", "border-top-style": "hidden"}},
		{"nd", map[string]string{"border-top-style": ""}},
		{"bad", map[string]string{"border-collapse": "separate", "border-top-style": ""}},
		{"badb", map[string]string{"border-top-style": ""}},
	} {
		s := styleByID(t, styles, tt.id)
		if tt.id == "gbord" {
			// With a border attribute the table's own border is not hidden.
			if got, _ := s.Get("border-top-style"); got == "hidden" {
				t.Errorf("#gbord border-top-style = hidden, want the border attribute's")
			}
		}
		for prop, want := range tt.want {
			if got, _ := s.Get(prop); got != want {
				t.Errorf("#%s %s = %q, want %q", tt.id, prop, got, want)
			}
		}
	}
}
