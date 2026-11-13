package js

import "testing"

// :scope in a selector query is the node the query runs on: SelectorMatches
// sets the checker's scope to it (core/css/selector_query.cc:102-113;
// Matches and Closest pass the target element, :115-143), and
// CheckPseudoScope matches the scope element itself, or the document
// element for a document or fragment scope (core/css/selector_checker.cc:
// 3650-3660 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestScopeIsTheQueriedNode(t *testing.T) {
	doc := parseHTML(t, `<div id="a" class="box"><div id="b" class="box"><p id="p"></p></div><p id="q"></p></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		function check(what, got, want) { if (got !== want) throw new Error(what + ": " + got + " want " + want); }
		function id(e) { return e === null ? "null" : e.id; }
		var a = document.getElementById("a"), b = document.getElementById("b"), p = document.getElementById("p");
		check("el.querySelector(:scope > .box)", id(a.querySelector(":scope > .box")), "b");
		check("el.querySelector(:scope > p)", id(a.querySelector(":scope > p")), "q");
		check("el.querySelectorAll(:scope p)", b.querySelectorAll(":scope p").length, 1);
		check("el.matches(:scope)", b.matches(":scope"), true);
		check("el.matches(:scope > .box)", a.matches(":scope > .box"), false);
		check("el.matches(.box:scope)", a.matches(".box:scope"), true);
		check("closest(:scope) is the element closest was called on", id(p.closest(":scope")), "p");
		check("closest(.box:not(:scope))", id(b.closest(".box:not(:scope)")), "a");
		check("document.querySelector(:scope) is the document element", document.querySelector(":scope"), document.documentElement);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
