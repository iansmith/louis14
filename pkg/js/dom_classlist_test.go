package js

import "testing"

func TestClassListAdd(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.classList.add("b", "c");
		if (el.className !== "a b c") throw new Error("className: " + el.className);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListAddNoDuplicate(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a b"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.classList.add("a");
		if (el.className !== "a b") throw new Error("should not duplicate: " + el.className);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListRemove(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a b c"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.classList.remove("b");
		if (el.className !== "a c") throw new Error("className: " + el.className);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListToggle(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		var result1 = el.classList.toggle("b");
		if (!result1) throw new Error("toggle add should return true");
		if (el.className !== "a b") throw new Error("after add: " + el.className);

		var result2 = el.classList.toggle("a");
		if (result2) throw new Error("toggle remove should return false");
		if (el.className !== "b") throw new Error("after remove: " + el.className);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListToggleForce(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.classList.toggle("a", true);
		if (el.className !== "a") throw new Error("force true should keep: " + el.className);

		el.classList.toggle("b", false);
		if (el.className !== "a") throw new Error("force false should not add: " + el.className);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListContains(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a b"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		if (!el.classList.contains("a")) throw new Error("should contain a");
		if (!el.classList.contains("b")) throw new Error("should contain b");
		if (el.classList.contains("c")) throw new Error("should not contain c");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListReplace(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a b c"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		var result = el.classList.replace("b", "x");
		if (!result) throw new Error("replace should return true");
		if (el.className !== "a x c") throw new Error("className: " + el.className);

		var result2 = el.classList.replace("nonexistent", "y");
		if (result2) throw new Error("replace of nonexistent should return false");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestClassListLength(t *testing.T) {
	doc := parseHTML(t, `<div id="el" class="a b c"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		if (el.classList.length !== 3) throw new Error("length: " + el.classList.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestElementRemove(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="child">text</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var child = document.getElementById("child");
		child.remove();
		var parent = document.getElementById("parent");
		if (parent.children.length !== 0) throw new Error("parent should be empty");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestAppendMultiple(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var a = document.createElement("span");
		var b = document.createElement("em");
		parent.append(a, b, "text");
		if (parent.children.length !== 2) throw new Error("children: " + parent.children.length);
		if (parent.textContent !== "text") throw new Error("text: " + parent.textContent);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestPrepend(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span>existing</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var newEl = document.createElement("em");
		parent.prepend(newEl);
		if (parent.children[0].tagName !== "EM") throw new Error("first child: " + parent.children[0].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceChildren(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span>a</span><span>b</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var p = document.createElement("p");
		parent.replaceChildren(p, "text");
		if (parent.children.length !== 1) throw new Error("children: " + parent.children.length);
		if (parent.children[0].tagName !== "P") throw new Error("child: " + parent.children[0].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestBeforeAfter(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="ref">ref</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var ref = document.getElementById("ref");
		var before = document.createElement("em");
		ref.before(before);
		var after = document.createElement("strong");
		ref.after(after);

		var parent = document.getElementById("parent");
		if (parent.children.length !== 3) throw new Error("children: " + parent.children.length);
		if (parent.children[0].tagName !== "EM") throw new Error("first: " + parent.children[0].tagName);
		if (parent.children[1].tagName !== "SPAN") throw new Error("second: " + parent.children[1].tagName);
		if (parent.children[2].tagName !== "STRONG") throw new Error("third: " + parent.children[2].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceWith(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="old">old</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var old = document.getElementById("old");
		var replacement = document.createElement("em");
		old.replaceWith(replacement);

		var parent = document.getElementById("parent");
		if (parent.children.length !== 1) throw new Error("children: " + parent.children.length);
		if (parent.children[0].tagName !== "EM") throw new Error("child: " + parent.children[0].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
