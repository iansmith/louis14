package js

import (
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/layout"
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

func TestIframeContentDocumentCrossDocAppendChild(t *testing.T) {
	// Mirrors the WPT pattern from svg-relative-urls-001.html:
	//   var iframe = document.querySelector('iframe');
	//   var div_from_iframe = iframe.contentDocument.querySelector('div');
	//   iframe.remove();
	//   document.body.appendChild(div_from_iframe);
	// Verifies that contentDocument exposes the nested doc's querySelector,
	// that iframe.remove() detaches the iframe, and that cross-doc appendChild
	// adopts the node into the main document.
	//
	// Per LOU-137 v2: appendChild does NOT mutate the moved node's
	// inline style. The cross-document URL re-resolution semantic
	// (`Element::DidMoveToNewDocument` in Blink) is achieved by the
	// next cascade pass — the moved node, now in the outer document,
	// inherits the outer Document.BaseDir via Style.BaseDir, and
	// parseFilterList resolves its `url()` against that base on the
	// re-cascade. The DOM string stays verbatim.
	doc := parseHTML(t, `<html><body><iframe id="frame"></iframe></body></html>`)

	// Build the nested document the way openFrameOwnerURL would: the
	// inline style carries the ORIGINAL relative URL — no pre-baking.
	nested, err := html.Parse(`<html><body><div id="inner" style="filter: url(support/hueRotate.svg#f)">moved</div></body></html>`)
	if err != nil {
		t.Fatalf("nested parse: %v", err)
	}
	nested.BaseDir = "support"

	iframe := getElementById(doc.Root, "frame")
	if iframe == nil {
		t.Fatal("iframe not found in main doc")
	}
	iframe.NestedDocument = nested

	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var iframe = document.querySelector('iframe');
		if (!iframe) throw new Error("iframe not found");
		var nestedDoc = iframe.contentDocument;
		if (!nestedDoc) throw new Error("contentDocument is null");
		var div = nestedDoc.querySelector('div');
		if (!div) throw new Error("nested div not found via contentDocument.querySelector");
		if (div.id !== "inner") throw new Error("wrong div id: " + div.id);
		iframe.remove();
		document.body.appendChild(div);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}

	if iframe.Parent != nil {
		t.Errorf("iframe still has parent %q after remove()", iframe.Parent.TagName)
	}

	if movedFromNested := getElementById(nested.Root, "inner"); movedFromNested != nil {
		t.Errorf("inner div still found in nested doc after appendChild")
	}
	body := findBodyNode(doc.Root)
	if body == nil {
		t.Fatal("main body not found")
	}
	movedDiv := getElementById(doc.Root, "inner")
	if movedDiv == nil {
		t.Fatal("inner div not found in main doc after appendChild")
	}
	if movedDiv.Parent != body {
		t.Errorf("moved div parent = %v, want main body", movedDiv.Parent)
	}
	// The inline style is preserved verbatim across the move. The next
	// cascade pass will see this string and parseFilterList will
	// resolve it against the OUTER document's BaseDir (empty), leaving
	// the URL relative for the renderer's fetcher to resolve against
	// the outer basePath — which is exactly the post-move semantic
	// Blink's DidMoveToNewDocument hook produces.
	wantStyle := `filter: url(support/hueRotate.svg#f)`
	if got := movedDiv.Attributes["style"]; got != wantStyle {
		t.Errorf("moved div style:\n  got  = %q\n  want = %q", got, wantStyle)
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

// TestCreateDocumentFragment pins Document::createDocumentFragment
// (core/dom/document.cc:1571 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b;
// document.idl:54): a DocumentFragment node (nodeType 11, nodeName
// "#document-fragment") that accepts children through ParentNode.append.
func TestCreateDocumentFragment(t *testing.T) {
	doc := parseHTML(t, `<div id="root"></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		var frag = document.createDocumentFragment();
		if (frag.nodeType !== 11) throw new Error("nodeType: " + frag.nodeType);
		if (frag.nodeName !== "#document-fragment") throw new Error("nodeName: " + frag.nodeName);
		frag.append(document.createElement("a"), "t");
		if (frag.childNodes.length !== 2) throw new Error("fragment children: " + frag.childNodes.length);
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestInsertDocumentFragmentMovesChildren pins that inserting a
// DocumentFragment inserts its children, in order, and leaves the
// fragment empty — CollectChildrenAndRemoveFromOldParent
// (core/dom/container_node.cc:168-184 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) behind appendChild (:1199)
// and insertBefore (:515), and Node::ConvertNodeUnionsIntoNodes
// (core/dom/node.cc:915-997 @ the same SHA) behind append / prepend /
// replaceChildren.
func TestInsertDocumentFragmentMovesChildren(t *testing.T) {
	doc := parseHTML(t, `<div id="a"></div><div id="b"><i id="ref"></i></div><div id="c"></div><div id="d"><s></s></div>`)
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		function frag() {
			var f = document.createDocumentFragment();
			var x = document.createElement("x"); x.id = "x";
			var y = document.createElement("y"); y.id = "y";
			f.append(x, y);
			return f;
		}
		function tags(el) {
			var s = [];
			for (var i = 0; i < el.childNodes.length; i++) s.push(el.childNodes[i].nodeName);
			return s.join(",");
		}
		var f = frag();
		var r = document.getElementById("a").appendChild(f);
		if (r !== f) throw new Error("appendChild should return the fragment");
		if (f.childNodes.length !== 0) throw new Error("fragment not emptied");
		if (tags(document.getElementById("a")) !== "X,Y") throw new Error("appendChild: " + tags(document.getElementById("a")));

		var b = document.getElementById("b");
		b.insertBefore(frag(), document.getElementById("ref"));
		if (tags(b) !== "X,Y,I") throw new Error("insertBefore: " + tags(b));

		var c = document.getElementById("c");
		c.append("t", frag());
		if (tags(c) !== "#text,X,Y") throw new Error("append: " + tags(c));

		var d = document.getElementById("d");
		d.replaceChildren(frag());
		if (tags(d) !== "X,Y") throw new Error("replaceChildren: " + tags(d));
		if (d.firstChild.parentNode !== d) throw new Error("moved child has wrong parent");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestIframeContentWindow pins HTMLFrameOwnerElement::contentWindow
// (core/html/html_frame_owner_element.cc:402 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; html_iframe_element.idl:37,
// html_object_element.idl:41): the content frame's window, whose
// .document is the same object contentDocument returns; null when the
// element has no content frame.
func TestIframeContentWindow(t *testing.T) {
	doc := parseHTML(t, `<html><body><iframe id="frame"></iframe><object id="obj"></object><iframe id="empty"></iframe></body></html>`)
	for _, id := range []string{"frame", "obj"} {
		nested, err := html.Parse(`<html><body><div id="inner">x</div></body></html>`)
		if err != nil {
			t.Fatalf("nested parse: %v", err)
		}
		getElementById(doc.Root, id).NestedDocument = nested
	}
	engine := New()
	doc.Scripts = append(doc.Scripts, `
		["frame", "obj"].forEach(function (id) {
			var el = document.getElementById(id);
			var win = el.contentWindow;
			if (!win) throw new Error(id + ": contentWindow is null");
			if (win.document !== el.contentDocument) throw new Error(id + ": contentWindow.document !== contentDocument");
			if (el.contentWindow !== win) throw new Error(id + ": contentWindow not [SameObject]");
			if (win.document.getElementById("inner") === null) throw new Error(id + ": nested lookup failed");
			if (typeof win.document.createDocumentFragment !== "function") throw new Error(id + ": no createDocumentFragment");
		});
		if (document.getElementById("empty").contentWindow !== null)
			throw new Error("contentWindow without a content frame should be null");
	`)
	if err := engine.Execute(doc); err != nil {
		t.Fatal(err)
	}
}

// TestIframeContentWindowStableAcrossLayout pins that contentWindow /
// contentDocument identity survives a forced layout: the owner keeps one
// content frame, which later layouts re-use rather than reload
// (HTMLFrameElementBase::AttachLayoutTree,
// core/html/html_frame_element_base.cc:198-203;
// HTMLPlugInElement::AttachLayoutTree, core/html/html_plugin_element.cc:209-237
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so the documentProxies /
// windowProxies caches (keyed by *html.Document) hand back the same
// objects. getClientRects forces the layout, as the visualtest harness's
// layout updater does with a fresh engine.
func TestIframeContentWindowStableAcrossLayout(t *testing.T) {
	doc := parseHTML(t, `<html><body><iframe id="frame" src="child.html"></iframe><object id="obj" data="child.html"></object></body></html>`)
	layoutPass := func() map[*html.Node]*layout.Box {
		e := layout.NewLayoutEngine(400, 400)
		e.SetDocumentFetcher(func(uri string) (string, error) {
			return `<div id="inner">x</div>`, nil
		})
		e.Layout(doc)
		return e.NodeBoxes()
	}
	layoutPass()
	eng := New()
	eng.SetLayoutUpdater(layoutPass)
	doc.Scripts = append(doc.Scripts, `
		["frame", "obj"].forEach(function (id) {
			var el = document.getElementById(id);
			var win = el.contentWindow, cd = el.contentDocument;
			if (!win) throw new Error(id + ": contentWindow is null");
			cd.getElementById("inner").id = "mutated";
			el.getClientRects();
			if (el.contentWindow !== win) throw new Error(id + ": contentWindow changed across layout");
			if (el.contentDocument !== cd) throw new Error(id + ": contentDocument changed across layout");
			if (el.contentDocument.getElementById("mutated") === null) throw new Error(id + ": content document mutation lost across layout");
		});
	`)
	if err := eng.Execute(doc); err != nil {
		t.Fatal(err)
	}
}
