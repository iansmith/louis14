package js

import "testing"

// TestStylePropertyPriority: CSSStyleDeclaration keeps a declaration's
// priority. setProperty's third argument is "important" (ASCII
// case-insensitive) or empty, anything else ignoring the call;
// getPropertyPriority reports "important"; cssText serialises " !important"
// (AbstractPropertySetCSSStyleDeclaration::setProperty / getPropertyPriority,
// CSSPropertyValueSet::AsText @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestStylePropertyPriority(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="color: red !important; margin-left: 1px"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("el").style;
		function check(what, got, want) { if (got !== want) throw new Error(what + ": " + got + " want " + want); }
		check("parsed priority", s.getPropertyPriority("color"), "important");
		check("parsed value", s.getPropertyValue("color"), "red");
		check("normal priority", s.getPropertyPriority("margin-left"), "");
		s.setProperty("margin-left", "5px", "IMPORTANT");
		check("set priority", s.getPropertyPriority("margin-left"), "important");
		check("set value", s.getPropertyValue("margin-left"), "5px");
		s.setProperty("margin-left", "7px", "bogus");
		check("bogus priority ignored", s.getPropertyValue("margin-left"), "5px");
		s.setProperty("margin-left", "8px");
		check("no priority is normal", s.getPropertyPriority("margin-left"), "");
		s.cssText = "padding-left: 2px !important";
		check("cssText priority", s.getPropertyPriority("padding-left"), "important");
		check("cssText round trip", s.cssText, "padding-left: 2px !important;");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
