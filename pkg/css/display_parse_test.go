package css

import (
	"testing"

	"louis14/pkg/html"
)

// display is validated at parse time like Blink's Display::ParseSingleValue
// (core/css/properties/longhands/longhands_custom.cc:3851-3876 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an invalid value drops the
// declaration, so an earlier one stays in effect. The ruby-internal values
// removed from Blink (ruby-base, ruby-base-container, ruby-text-container)
// are invalid; ruby and ruby-text are valid.
func TestDisplayParseValidation(t *testing.T) {
	invalid := []string{
		"ruby-base", "ruby-base-container", "ruby-text-container",
		"run-in", "inline-list-item", "grid-lanes", "inline-grid-lanes",
		"bogus", "inline inline", "flex grid", "block inline flex",
		"list-item flex", "inline list-item table", "block flow list-item extra",
		"layout(foo)", "",
	}
	valid := []string{
		"block", "inline", "flow", "flow-root", "inline-block", "flex",
		"inline-flex", "grid", "inline-grid", "table", "inline-table",
		"table-row-group", "table-header-group", "table-footer-group",
		"table-row", "table-column-group", "table-column", "table-cell",
		"table-caption", "ruby", "ruby-text", "math", "none", "contents",
		"list-item", "-webkit-box", "-webkit-inline-box", "-webkit-flex",
		"-webkit-inline-flex", "BLOCK", "inline flex", "block ruby",
		"inline flow-root", "flex inline", "inline list-item",
		"inline flow list-item", "list-item flow-root block",
		"inherit", "unset", "var(--d)",
	}
	parse := func(t *testing.T, decl string) string {
		t.Helper()
		ss, err := ParseStylesheet("rb { display: inline; display: "+decl+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		return ss.Rules[0].Declarations["display"]
	}
	for _, v := range invalid {
		if got := parse(t, v); got != "inline" {
			t.Errorf("display: %q should be invalid (keep the earlier inline); got %q", v, got)
		}
		if got, _ := ParseInlineStyle("display: inline; display: "+v, nil).Get("display"); got != "inline" {
			t.Errorf("inline style display: %q should be invalid; got %q", v, got)
		}
	}
	for _, v := range valid {
		if got := parse(t, v); got == "inline" && v != "inline" {
			t.Errorf("display: %q should be valid; the declaration was dropped", v)
		}
	}
}

// resolvedDisplay returns the DisplayType an element with the declaration
// `display: <value>` resolves to, through the stylesheet path and through
// the inline-style path.
func resolvedDisplay(t *testing.T, value string) (sheet, inline DisplayType) {
	t.Helper()
	ss, err := ParseStylesheet("x-el { display: "+value+"; }", nil)
	if err != nil {
		t.Fatal(err)
	}
	el := &html.Node{Type: html.ElementNode, TagName: "x-el"}
	sheet = ComputeStyle(el, []*Stylesheet{ss}, 800, 600, nil).GetDisplay()
	inline = ParseInlineStyle("display: "+value, nil).GetDisplay()
	return sheet, inline
}

// Valid values resolve to their display type through both parse paths.
// Keywords match ASCII case-insensitively, as Blink's CSS parser does, so
// `Inline-Block` is inline-block (LOU-675 folds keyword getters the same way).
func TestDisplayResolvesThroughBothParsePaths(t *testing.T) {
	cases := []struct {
		value string
		want  DisplayType
	}{
		{"block", DisplayBlock}, {"flow", DisplayBlock}, {"inline", DisplayInline},
		{"flow-root", DisplayFlowRoot}, {"inline-block", DisplayInlineBlock},
		{"flex", DisplayFlex}, {"inline-flex", DisplayInlineFlex},
		{"grid", DisplayGrid}, {"inline-grid", DisplayInlineGrid},
		{"table", DisplayTable}, {"inline-table", DisplayInlineTable},
		{"table-row-group", DisplayTableRowGroup}, {"table-header-group", DisplayTableHeaderGroup},
		{"table-footer-group", DisplayTableFooterGroup}, {"table-row", DisplayTableRow},
		{"table-cell", DisplayTableCell}, {"table-caption", DisplayTableCaption},
		{"ruby", DisplayRuby}, {"ruby-text", DisplayRubyText}, {"math", DisplayMath},
		{"none", DisplayNone}, {"contents", DisplayContents}, {"list-item", DisplayListItem},
		{"-webkit-box", DisplayFlex}, {"-webkit-inline-box", DisplayInlineFlex},
		{"-webkit-flex", DisplayFlex}, {"-webkit-inline-flex", DisplayInlineFlex},
		{"inline flex", DisplayInlineFlex}, {"flex inline", DisplayInlineFlex},
		{"block ruby", DisplayBlockRuby}, {"inline flow-root", DisplayInlineBlock},
		{"inline list-item", DisplayInlineListItem},
		{"Inline-Block", DisplayInlineBlock}, {"INLINE", DisplayInline},
		{"Inline Flex", DisplayInlineFlex}, {"BLOCK RUBY", DisplayBlockRuby},
	}
	for _, tc := range cases {
		sheet, inline := resolvedDisplay(t, tc.value)
		if sheet != tc.want {
			t.Errorf("stylesheet display: %q resolves to %q, want %q", tc.value, sheet, tc.want)
		}
		if inline != tc.want {
			t.Errorf("inline style display: %q resolves to %q, want %q", tc.value, inline, tc.want)
		}
	}
}

// The removed ruby-internal displays are excluded from layout, paint and
// size containment, case-insensitively: they are invalid at parse time, but
// var() substitution can still deliver them to the style.
func TestContainmentExcludesRubyInternalDisplays(t *testing.T) {
	for _, v := range []string{"ruby-base", "ruby-base-container", "ruby-text-container", "ruby-text", "RUBY-BASE", "Ruby-Text-Container"} {
		st := NewStyle()
		st.Set("display", v)
		if isEligibleForLayoutOrPaintContainment(st) {
			t.Errorf("display %q should not be eligible for layout/paint containment", v)
		}
		if isEligibleForSizeContainment(st) {
			t.Errorf("display %q should not be eligible for size containment", v)
		}
	}
}
