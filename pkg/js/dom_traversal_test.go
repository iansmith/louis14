package js

import "testing"

func TestFirstLastChild(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span>a</span><em>b</em><p>c</p></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("parent");
		if (el.firstChild.tagName !== "SPAN") throw new Error("firstChild: " + el.firstChild.tagName);
		if (el.lastChild.tagName !== "P") throw new Error("lastChild: " + el.lastChild.tagName);
		if (el.firstElementChild.tagName !== "SPAN") throw new Error("firstElementChild: " + el.firstElementChild.tagName);
		if (el.lastElementChild.tagName !== "P") throw new Error("lastElementChild: " + el.lastElementChild.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestFirstLastChildWithText(t *testing.T) {
	doc := parseHTML(t, `<div id="parent">text<span>el</span>more</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("parent");
		// firstChild should be the text node
		if (el.firstChild.nodeType !== 3) throw new Error("firstChild should be text: " + el.firstChild.nodeType);
		if (el.firstChild.nodeValue !== "text") throw new Error("firstChild text: " + el.firstChild.nodeValue);
		// firstElementChild should skip text nodes
		if (el.firstElementChild.tagName !== "SPAN") throw new Error("firstElementChild: " + el.firstElementChild.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestChildNodes(t *testing.T) {
	doc := parseHTML(t, `<div id="parent">text<span>el</span>more</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("parent");
		var cn = el.childNodes;
		if (cn.length !== 3) throw new Error("childNodes.length: " + cn.length);
		if (cn[0].nodeType !== 3) throw new Error("childNodes[0] should be text");
		if (cn[1].nodeType !== 1) throw new Error("childNodes[1] should be element");
		if (cn[2].nodeType !== 3) throw new Error("childNodes[2] should be text");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestSiblings(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="first">a</span><em id="mid">b</em><p id="last">c</p></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var mid = document.getElementById("mid");
		if (mid.nextSibling.tagName !== "P") throw new Error("nextSibling: " + mid.nextSibling.tagName);
		if (mid.previousSibling.tagName !== "SPAN") throw new Error("previousSibling: " + mid.previousSibling.tagName);
		if (mid.nextElementSibling.tagName !== "P") throw new Error("nextElementSibling: " + mid.nextElementSibling.tagName);
		if (mid.previousElementSibling.tagName !== "SPAN") throw new Error("previousElementSibling: " + mid.previousElementSibling.tagName);

		var first = document.getElementById("first");
		if (first.previousSibling !== null) throw new Error("first.previousSibling should be null");
		if (first.previousElementSibling !== null) throw new Error("first.previousElementSibling should be null");

		var last = document.getElementById("last");
		if (last.nextSibling !== null) throw new Error("last.nextSibling should be null");
		if (last.nextElementSibling !== null) throw new Error("last.nextElementSibling should be null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestElementSiblingsWithText(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="a">a</span>text<em id="b">b</em></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var a = document.getElementById("a");
		// nextSibling should be the text node
		if (a.nextSibling.nodeType !== 3) throw new Error("a.nextSibling should be text node");
		// nextElementSibling should skip text and find em
		if (a.nextElementSibling.tagName !== "EM") throw new Error("a.nextElementSibling: " + a.nextElementSibling.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestChildElementCount(t *testing.T) {
	doc := parseHTML(t, `<div id="parent">text<span>a</span>more<em>b</em></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("parent");
		if (el.childElementCount !== 2) throw new Error("childElementCount: " + el.childElementCount);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestHasAttribute(t *testing.T) {
	doc := parseHTML(t, `<div id="el" data-x="1"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		if (!el.hasAttribute("data-x")) throw new Error("should have data-x");
		if (el.hasAttribute("data-y")) throw new Error("should not have data-y");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveAttribute(t *testing.T) {
	doc := parseHTML(t, `<div id="el" data-x="1"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("el");
		el.removeAttribute("data-x");
		if (el.hasAttribute("data-x")) throw new Error("should be removed");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestCloneNode(t *testing.T) {
	doc := parseHTML(t, `<div id="original" class="cls"><span>child</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var orig = document.getElementById("original");
		var shallow = orig.cloneNode(false);
		if (shallow.tagName !== "DIV") throw new Error("shallow tagName: " + shallow.tagName);
		if (shallow.className !== "cls") throw new Error("shallow class: " + shallow.className);
		if (shallow.children.length !== 0) throw new Error("shallow should have no children");

		var deep = orig.cloneNode(true);
		if (deep.children.length !== 1) throw new Error("deep children: " + deep.children.length);
		if (deep.children[0].tagName !== "SPAN") throw new Error("deep child: " + deep.children[0].tagName);
		// Should be different objects
		if (deep === orig) throw new Error("deep clone should be a different object");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestContainsMethod(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="child"><em id="grandchild">text</em></span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var child = document.getElementById("child");
		var gc = document.getElementById("grandchild");

		if (!parent.contains(child)) throw new Error("parent should contain child");
		if (!parent.contains(gc)) throw new Error("parent should contain grandchild");
		if (!parent.contains(parent)) throw new Error("node should contain itself");
		if (child.contains(parent)) throw new Error("child should not contain parent");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestHasChildNodes(t *testing.T) {
	doc := parseHTML(t, `<div id="a"><span>text</span></div><div id="b"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		if (!document.getElementById("a").hasChildNodes()) throw new Error("a should have children");
		if (document.getElementById("b").hasChildNodes()) throw new Error("b should not have children");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentBody(t *testing.T) {
	doc := parseHTML(t, `<html><head><title>test</title></head><body><p>text</p></body></html>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		if (document.body === null) throw new Error("body should not be null");
		if (document.body.tagName !== "BODY") throw new Error("body tagName: " + document.body.tagName);
		if (document.head === null) throw new Error("head should not be null");
		if (document.head.tagName !== "HEAD") throw new Error("head tagName: " + document.head.tagName);
		if (document.documentElement === null) throw new Error("documentElement should not be null");
		if (document.documentElement.tagName !== "HTML") throw new Error("documentElement tagName: " + document.documentElement.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestElementGetElementsByTagName(t *testing.T) {
	doc := parseHTML(t, `<div id="scope"><p>a</p><p>b</p></div><p>c</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var scope = document.getElementById("scope");
		var ps = scope.getElementsByTagName("p");
		if (ps.length !== 2) throw new Error("expected 2, got: " + ps.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestParentNode(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="child">text</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var child = document.getElementById("child");
		if (child.parentNode === null) throw new Error("parentNode should not be null");
		if (child.parentNode.tagName !== "DIV") throw new Error("parentNode: " + child.parentNode.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestNodeValue(t *testing.T) {
	doc := parseHTML(t, `<div id="parent">text content</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		// Element nodeValue should be null
		if (parent.nodeValue !== null) throw new Error("element nodeValue should be null");
		// Text node nodeValue should be the text
		var text = parent.firstChild;
		if (text.nodeValue !== "text content") throw new Error("text nodeValue: " + text.nodeValue);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestFirstLastChildEmpty(t *testing.T) {
	doc := parseHTML(t, `<div id="empty"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("empty");
		if (el.firstChild !== null) throw new Error("firstChild should be null");
		if (el.lastChild !== null) throw new Error("lastChild should be null");
		if (el.firstElementChild !== null) throw new Error("firstElementChild should be null");
		if (el.lastElementChild !== null) throw new Error("lastElementChild should be null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
