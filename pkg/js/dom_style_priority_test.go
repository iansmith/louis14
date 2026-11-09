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

// TestStylePriorityCannotBeInjected: a value with a priority in it is
// invalid for setProperty and the camelCase setter (it must come through
// setProperty's third argument); an inline important declaration beats a
// later normal one of the same name, as in the cascade; and an
// element-level animation write leaves an inline !important declaration.
func TestStylePriorityCannotBeInjected(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="color: red !important; color: blue"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var s = document.getElementById("el").style;
		function check(what, got, want) { if (got !== want) throw new Error(what + ": " + got + " want " + want); }
		check("important beats later normal", s.getPropertyValue("color"), "red");
		check("important beats later normal priority", s.getPropertyPriority("color"), "important");
		check("cssText keeps it", s.cssText, "color: red !important;");
		s.setProperty("margin-left", "5px !important");
		check("setProperty with !important in the value is rejected", s.getPropertyValue("margin-left"), "");
		s.marginRight = "6px !important";
		check("setter with !important in the value is rejected", s.getPropertyValue("margin-right"), "");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	el := getElementById(doc.Root, "el")
	mergeInlineStyle(el, map[string]string{"color": "green"})
	if d := parseDeclarationList(el.Attributes["style"]).get("color"); d.value != "red" || !d.important {
		t.Errorf("after an animation write, color = %q important=%v, want red important", d.value, d.important)
	}
}
