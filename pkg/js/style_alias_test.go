package js

import "testing"

// The CSSOM resolves an alias to its target before it reads or writes a
// declaration, so grid-column-gap is column-gap through element.style and
// getComputedStyle alike (AbstractPropertySetCSSStyleDeclaration::
// getPropertyValue / getPropertyPriority / removeProperty, abstract_property_
// set_css_style_declaration.cc:76-191; CSSComputedStyleDeclaration::
// getPropertyValue, css_computed_style_declaration.cc:492-495 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b: CssPropertyID resolves).
func TestStyleAliasesAreTheirTarget(t *testing.T) {
	runWithLayout(t, `<!DOCTYPE html><div id="el" style="display: grid; grid-row-gap: 4px"></div>`, `
		function check(what, got, want) { if (got !== want) throw new Error(what + ": " + JSON.stringify(got) + " want " + JSON.stringify(want)); }
		var el = document.getElementById("el"), s = el.style;
		check("attribute alias read as target", s.getPropertyValue("row-gap"), "4px");
		s.setProperty("grid-column-gap", "3px", "important");
		check("set through alias, read target", s.getPropertyValue("column-gap"), "3px");
		check("read through alias", s.getPropertyValue("grid-column-gap"), "3px");
		check("priority through alias", s.getPropertyPriority("grid-column-gap"), "important");
		s.setProperty("column-gap", "5px");
		check("one declaration, not two", s.cssText, "display: grid; row-gap: 4px; column-gap: 5px;");
		s.gridColumnGap = "6px";
		check("camel-case alias", s.getPropertyValue("column-gap"), "6px");
		check("removeProperty returns the old value", s.removeProperty("grid-column-gap"), "6px");
		check("removed through alias", s.getPropertyValue("column-gap"), "");
		check("computed alias", getComputedStyle(el).getPropertyValue("grid-row-gap"), "4px");
	`)
}
