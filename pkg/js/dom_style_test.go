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
