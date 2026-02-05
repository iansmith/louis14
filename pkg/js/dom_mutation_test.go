package js

import (
	"testing"

	"louis14/pkg/html"
)

func TestCreateElement(t *testing.T) {
	doc := parseHTML(t, `<div id="root"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.createElement("span");
		if (el.tagName !== "SPAN") throw new Error("tagName: " + el.tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTextNode(t *testing.T) {
	doc := parseHTML(t, `<div id="root"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var text = document.createTextNode("hello");
		if (text.nodeType !== 3) throw new Error("nodeType: " + text.nodeType);
		if (text.nodeValue !== "hello") throw new Error("nodeValue: " + text.nodeValue);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestAppendChild(t *testing.T) {
	doc := parseHTML(t, `<div id="root"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		var child = document.createElement("p");
		root.appendChild(child);
		if (root.children.length !== 1) throw new Error("children.length: " + root.children.length);
		if (root.children[0].tagName !== "P") throw new Error("child tagName: " + root.children[0].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
	// Verify Go-side
	root := getElementById(doc.Root, "root")
	if len(root.Children) != 1 || root.Children[0].TagName != "p" {
		t.Error("appendChild failed on Go side")
	}
}

func TestAppendChildReparent(t *testing.T) {
	doc := parseHTML(t, `<div id="a"><span id="child">text</span></div><div id="b"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var a = document.getElementById("a");
		var b = document.getElementById("b");
		var child = document.getElementById("child");
		b.appendChild(child);
		if (a.children.length !== 0) throw new Error("a should be empty, got: " + a.children.length);
		if (b.children.length !== 1) throw new Error("b should have 1 child, got: " + b.children.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveChild(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="child">text</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var child = document.getElementById("child");
		var removed = parent.removeChild(child);
		if (parent.children.length !== 0) throw new Error("parent should be empty");
		if (removed.tagName !== "SPAN") throw new Error("removed should be SPAN");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestInsertBefore(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span id="ref">ref</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var ref = document.getElementById("ref");
		var newEl = document.createElement("p");
		parent.insertBefore(newEl, ref);
		if (parent.children.length !== 2) throw new Error("expected 2 children, got: " + parent.children.length);
		if (parent.children[0].tagName !== "P") throw new Error("first child should be P");
		if (parent.children[1].tagName !== "SPAN") throw new Error("second child should be SPAN");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestInsertBeforeNull(t *testing.T) {
	doc := parseHTML(t, `<div id="parent"><span>existing</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var parent = document.getElementById("parent");
		var newEl = document.createElement("p");
		parent.insertBefore(newEl, null);
		if (parent.children[parent.children.length - 1].tagName !== "P") throw new Error("should append when ref is null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestInnerHTMLGet(t *testing.T) {
	doc := parseHTML(t, `<div id="root"><span>hello</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		var html = root.innerHTML;
		if (html !== "<span>hello</span>") throw new Error("innerHTML: " + html);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestInnerHTMLSet(t *testing.T) {
	doc := parseHTML(t, `<div id="root">old content</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		root.innerHTML = "<p>new</p><span>content</span>";
		if (root.children.length !== 2) throw new Error("expected 2 children, got: " + root.children.length);
		if (root.children[0].tagName !== "P") throw new Error("first child: " + root.children[0].tagName);
		if (root.children[1].tagName !== "SPAN") throw new Error("second child: " + root.children[1].tagName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestInnerHTMLSetEmpty(t *testing.T) {
	doc := parseHTML(t, `<div id="root"><p>child</p></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		root.innerHTML = "";
		if (root.children.length !== 0) throw new Error("should be empty, got: " + root.children.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestNodeIdentityCache(t *testing.T) {
	doc := parseHTML(t, `<div id="root"><span id="child">text</span></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el1 = document.getElementById("child");
		var el2 = document.getElementById("child");
		if (el1 !== el2) throw new Error("same node should return same proxy");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestNodeType(t *testing.T) {
	doc := parseHTML(t, `<div id="root">text</div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		if (root.nodeType !== 1) throw new Error("element nodeType: " + root.nodeType);
		if (root.nodeName !== "DIV") throw new Error("nodeName: " + root.nodeName);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestOuterHTML(t *testing.T) {
	doc := parseHTML(t, `<p id="target">hello</p>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var el = document.getElementById("target");
		var html = el.outerHTML;
		if (html !== '<p id="target">hello</p>') throw new Error("outerHTML: " + html);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestAppendChildTextNode(t *testing.T) {
	doc := parseHTML(t, `<div id="root"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var root = document.getElementById("root");
		var text = document.createTextNode("hello world");
		root.appendChild(text);
		if (root.textContent !== "hello world") throw new Error("textContent: " + root.textContent);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

func TestParseFragmentKeepsScripts(t *testing.T) {
	nodes, err := html.ParseFragment(`<p>text</p><script>var x = 1;</script>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[1].TagName != "script" {
		t.Errorf("second node should be script, got %s", nodes[1].TagName)
	}
}
