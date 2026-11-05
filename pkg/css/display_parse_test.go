package css

import "testing"

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
