package js

// Tests for the CSSStyleDeclaration method surface on element.style
// (LOU-352 follow-up): highlight-styling-005.html mutates a root custom
// property via `document.querySelector(':root').style.setProperty('--bg',
// 'green')` — before this fix, styleAccessor.Get fell through to the
// camelToKebab attribute lookup and returned "" (a string, not a function),
// so the call threw and the mutation silently never landed. CSSOM §6.7.3
// (CSSStyleDeclaration.setProperty / removeProperty / getPropertyValue);
// mirrors Blink's AbstractPropertySetCSSStyleDeclaration::setProperty in
// core/css/abstract_property_set_css_style_declaration.cc (@ Chromium main
// 72259ecfb56eacf259e21783dd2b31608ac951a3).

import (
	"testing"

	"louis14/pkg/html"
)

// findElementByTag walks the parsed tree for the first element with the
// given tag name — test-local helper for asserting on nodes (like the html
// root) that carry no id.
func findElementByTag(t *testing.T, node *html.Node, tag string) *html.Node {
	t.Helper()
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && node.TagName == tag {
		return node
	}
	for _, child := range node.Children {
		if found := findElementByTag(t, child, tag); found != nil {
			return found
		}
	}
	return nil
}

func TestStyleSetPropertyUpdatesInlineStyle(t *testing.T) {
	doc := parseHTML(t, `<div id="el"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.style.setProperty("background-color", "green");
		if (el.style.getPropertyValue("background-color") !== "green")
			throw new Error("getPropertyValue: " + el.style.getPropertyValue("background-color"));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	el := getElementById(doc.Root, "el")
	if el == nil {
		t.Fatalf("could not find #el after script execution")
	}
	if got := el.Attributes["style"]; got != "background-color: green" {
		t.Errorf("style attribute = %q, want %q", got, "background-color: green")
	}
}

func TestStyleSetPropertyCustomPropertyOnRoot(t *testing.T) {
	// highlight-styling-005.html's exact idiom: mutate a custom property on
	// :root. Pins both querySelector(':root') resolving the root element and
	// setProperty accepting a custom property name verbatim (CSSOM: custom
	// properties are not case-normalized or camelized).
	doc := parseHTML(t, `<div id="el"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.querySelector(":root");
		if (!root) throw new Error("querySelector(':root') returned nothing");
		root.style.setProperty("--bg", "green");
		if (root.style.getPropertyValue("--bg") !== "green")
			throw new Error("getPropertyValue('--bg'): " + root.style.getPropertyValue("--bg"));
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	root := findElementByTag(t, doc.Root, "html")
	if root == nil {
		t.Fatalf("could not locate html root element")
	}
	if got := root.Attributes["style"]; got != "--bg: green" {
		t.Errorf("root style attribute = %q, want %q", got, "--bg: green")
	}
}

func TestStyleRemovePropertyClearsDeclaration(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="color: red; background-color: green"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		var old = el.style.removeProperty("color");
		if (old !== "red") throw new Error("removeProperty should return the old value, got: " + old);
		if (el.style.getPropertyValue("color") !== "")
			throw new Error("color should be removed, got: " + el.style.getPropertyValue("color"));
		if (el.style.getPropertyValue("background-color") !== "green")
			throw new Error("background-color should survive");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestStyleSetPropertyEmptyValueRemoves(t *testing.T) {
	// CSSOM §6.7.3 step 4: setProperty with an empty value behaves as
	// removeProperty.
	doc := parseHTML(t, `<div id="el" style="color: red"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.style.setProperty("color", "");
		if (el.style.getPropertyValue("color") !== "")
			throw new Error("empty value should remove the declaration");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestStyleCSSTextGetterSerializesDeclarationBlock pins the cssText getter
// (AbstractPropertySetCSSStyleDeclaration::cssText,
// core/css/abstract_property_set_css_style_declaration.cc:50 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b → CSSPropertyValueSet::AsText):
// the declarations serialise in source order as "name: value;" joined by
// a space.
func TestStyleCSSTextGetterSerializesDeclarationBlock(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="color:red;  display : contents"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		var got = el.style.cssText;
		if (got !== "color: red; display: contents;")
			throw new Error("cssText = " + JSON.stringify(got));
		if (document.createElement("p").style.cssText !== "")
			throw new Error("empty block cssText should be the empty string");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestStyleCSSTextSetterReplacesDeclarationBlock pins the cssText setter
// (AbstractPropertySetCSSStyleDeclaration::setCSSText,
// core/css/abstract_property_set_css_style_declaration.cc:54 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): it re-parses the whole
// declaration list, replacing every existing declaration, and "" empties
// the block. It must not create a "css-text" property.
func TestStyleCSSTextSetterReplacesDeclarationBlock(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="color: red; display: contents"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.style.cssText = "width: 10px";
		if (el.style.color !== "") throw new Error("color survived cssText: " + el.style.color);
		if (el.style.width !== "10px") throw new Error("width = " + el.style.width);
		el.style.cssText = "";
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	el := getElementById(doc.Root, "el")
	if got := el.Attributes["style"]; got != "" {
		t.Errorf("style attribute after cssText = \"\" is %q, want empty", got)
	}
}

// An invalid float or clear value leaves the existing declaration alone: the
// setters go through the parser, and a parse error changes nothing
// (AbstractPropertySetCSSStyleDeclaration::SetPropertyInternal,
// core/css/abstract_property_set_css_style_declaration.cc:278-279 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Reads and rewrites of the style
// attribute see the block the parser would build from it.
func TestStyleSettersDropInvalidFloatClearValues(t *testing.T) {
	cases := []struct {
		name, attr, script, want string
	}{
		{"property setter", `clear:left`, `el.style.clear = "inline";`, "clear:left"},
		{"setProperty", `clear:left`, `el.style.setProperty("clear", "bogus");`, "clear:left"},
		{"setProperty, upper-case name", `clear:left`, `el.style.setProperty("CLEAR", "bogus");`, "clear:left"},
		{"cssFloat", `float:left`, `el.style.cssFloat = "both";`, "float:left"},
		{"empty value removes", `clear:left`, `el.style.clear = "";`, ""},
		{"setProperty, empty value removes", `clear:left`, `el.style.setProperty("clear", "");`, ""},
		{"valid value replaces", `clear:left`, `el.style.clear = "both";`, "clear: both"},
		{"env() is accepted", `clear:left`, `el.style.clear = "env(a,both)";`, "clear: env(a,both)"},
		{"rewrite keeps the earlier valid value", `clear:left;clear:inline`, `el.style.removeProperty("color");`, "clear: left"},
		{"!important stays valid in the attribute", `clear:left !important`, `el.style.removeProperty("color");`, "clear: left !important"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := parseHTML(t, `<div id="el" style="`+c.attr+`"></div>`)
			doc.Scripts = append(doc.Scripts, `var el = document.getElementById("el"); `+c.script)
			if err := New().Execute(doc); err != nil {
				t.Fatal(err)
			}
			if got := getElementById(doc.Root, "el").Attributes["style"]; got != c.want {
				t.Errorf("style attribute = %q, want %q", got, c.want)
			}
		})
	}
}

func TestStyleReadsSkipInvalidFloatClearValues(t *testing.T) {
	doc := parseHTML(t, `<div id="el" style="clear:left;clear:inline"></div>`)
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		if (el.style.clear !== "left") throw new Error("clear: " + el.style.clear);
		if (el.style.cssText !== "clear: left;") throw new Error("cssText: " + el.style.cssText);
	`)
	if err := New().Execute(doc); err != nil {
		t.Fatal(err)
	}
}
