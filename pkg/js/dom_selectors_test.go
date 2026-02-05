package js

import "testing"

func TestQuerySelector(t *testing.T) {
	doc := parseHTML(t, `<div><p class="a">first</p><p class="b">second</p></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector(".b");
		if (el === null) throw new Error("not found");
		if (el.textContent !== "second") throw new Error("wrong element: " + el.textContent);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySelectorAll(t *testing.T) {
	doc := parseHTML(t, `<ul><li>a</li><li>b</li><li>c</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var items = document.querySelectorAll("li");
		if (items.length !== 3) throw new Error("expected 3, got: " + items.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySelectorComma(t *testing.T) {
	doc := parseHTML(t, `<p>a</p><div>b</div><span>c</span>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var items = document.querySelectorAll("p, span");
		if (items.length !== 2) throw new Error("expected 2, got: " + items.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySelectorById(t *testing.T) {
	doc := parseHTML(t, `<div id="target">found</div><div>not</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector("#target");
		if (el === null) throw new Error("not found");
		if (el.textContent !== "found") throw new Error("wrong: " + el.textContent);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySelectorNotFound(t *testing.T) {
	doc := parseHTML(t, `<div>content</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector(".nonexistent");
		if (el !== null) throw new Error("should be null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestElementQuerySelector(t *testing.T) {
	doc := parseHTML(t, `<div id="scope"><span class="a">inside</span></div><span class="a">outside</span>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var scope = document.getElementById("scope");
		var el = scope.querySelector(".a");
		if (el.textContent !== "inside") throw new Error("should find inside scope: " + el.textContent);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestElementQuerySelectorAll(t *testing.T) {
	doc := parseHTML(t, `<div id="scope"><span>a</span><span>b</span></div><span>c</span>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var scope = document.getElementById("scope");
		var items = scope.querySelectorAll("span");
		if (items.length !== 2) throw new Error("expected 2, got: " + items.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestMatches(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="foo bar"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		if (!el.matches(".foo")) throw new Error("should match .foo");
		if (!el.matches("div.bar")) throw new Error("should match div.bar");
		if (el.matches(".baz")) throw new Error("should not match .baz");
		if (!el.matches(".foo, .baz")) throw new Error("should match comma group");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClosest(t *testing.T) {
	doc := parseHTML(t, `<div class="outer"><div class="inner"><span id="target">text</span></div></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("target");
		var closest = el.closest(".outer");
		if (closest === null) throw new Error("closest should find .outer");
		if (closest.className !== "outer") throw new Error("wrong: " + closest.className);

		var self = el.closest("span");
		if (self === null) throw new Error("closest should match self");
		if (self.id !== "target") throw new Error("should be self");

		var none = el.closest(".nonexistent");
		if (none !== null) throw new Error("should be null for no match");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestFirstChildPseudoClass(t *testing.T) {
	doc := parseHTML(t, `<ul><li id="first">a</li><li>b</li><li>c</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector("li:first-child");
		if (el === null) throw new Error("not found");
		if (el.id !== "first") throw new Error("wrong element: " + el.id);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestLastChildPseudoClass(t *testing.T) {
	doc := parseHTML(t, `<ul><li>a</li><li>b</li><li id="last">c</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector("li:last-child");
		if (el === null) throw new Error("not found");
		if (el.id !== "last") throw new Error("wrong element: " + el.id);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestNthChildPseudoClass(t *testing.T) {
	doc := parseHTML(t, `<ul><li>1</li><li id="second">2</li><li>3</li><li id="fourth">4</li></ul>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.querySelector("li:nth-child(2)");
		if (el === null) throw new Error("not found");
		if (el.id !== "second") throw new Error("wrong element: " + el.id);

		var even = document.querySelectorAll("li:nth-child(even)");
		if (even.length !== 2) throw new Error("expected 2 even, got: " + even.length);

		var odd = document.querySelectorAll("li:nth-child(odd)");
		if (odd.length !== 2) throw new Error("expected 2 odd, got: " + odd.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
