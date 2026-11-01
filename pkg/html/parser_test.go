package html

import (
	"strings"
	"testing"
)

// bodyOf returns the <body> element from a document's implicit <html>/<body>
// structure. HTML5 §8.2.6: block-level elements at the document root trigger
// implicit <html> and <body> creation.
func bodyOf(doc *Document) *Node {
	for _, child := range doc.Root.Children {
		if child.TagName == "html" {
			for _, c := range child.Children {
				if c.TagName == "body" {
					return c
				}
			}
		}
	}
	return nil
}

func TestParser_SingleElement(t *testing.T) {
	doc, err := Parse("<div></div>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	if len(body.Children) != 1 {
		t.Errorf("expected 1 child in body, got %d", len(body.Children))
	}
	if body.Children[0].TagName != "div" {
		t.Errorf("expected tag 'div', got '%s'", body.Children[0].TagName)
	}
}

func TestParser_MultipleElements(t *testing.T) {
	doc, err := Parse("<div></div><p></p>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	if len(body.Children) != 2 {
		t.Errorf("expected 2 children in body, got %d", len(body.Children))
	}
}

func TestParser_WithAttributes(t *testing.T) {
	doc, err := Parse(`<div style="color: red"></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	if len(body.Children) == 0 {
		t.Fatal("expected at least 1 child in body")
	}
	style, ok := body.Children[0].GetAttribute("style")
	if !ok || style != "color: red" {
		t.Error("expected style attribute 'color: red'")
	}
}

// Phase 2 tests: Nested elements
func TestParser_NestedElements(t *testing.T) {
	doc, err := Parse(`<div><p>Hello</p></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	if len(body.Children) != 1 {
		t.Fatalf("expected 1 child in body, got %d", len(body.Children))
	}

	div := body.Children[0]
	if div.TagName != "div" {
		t.Errorf("expected 'div', got '%s'", div.TagName)
	}

	// Div should have one child (p)
	if len(div.Children) != 1 {
		t.Fatalf("expected div to have 1 child, got %d", len(div.Children))
	}

	p := div.Children[0]
	if p.TagName != "p" {
		t.Errorf("expected 'p', got '%s'", p.TagName)
	}

	// P should have one text child
	if len(p.Children) != 1 {
		t.Fatalf("expected p to have 1 text child, got %d", len(p.Children))
	}

	if p.Children[0].Type != TextNode || p.Children[0].Text != "Hello" {
		t.Error("expected text node with 'Hello'")
	}
}

func TestParser_DeeplyNestedElements(t *testing.T) {
	doc, err := Parse(`<div><section><article><p>Deep</p></article></section></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}

	div := body.Children[0]
	if div.TagName != "div" || len(div.Children) != 1 {
		t.Error("expected div with 1 child")
	}

	section := div.Children[0]
	if section.TagName != "section" || len(section.Children) != 1 {
		t.Error("expected section with 1 child")
	}

	article := section.Children[0]
	if article.TagName != "article" || len(article.Children) != 1 {
		t.Error("expected article with 1 child")
	}

	p := article.Children[0]
	if p.TagName != "p" || len(p.Children) != 1 {
		t.Error("expected p with 1 text child")
	}

	if p.Children[0].Text != "Deep" {
		t.Errorf("expected text 'Deep', got '%s'", p.Children[0].Text)
	}
}

func TestParser_SiblingElements(t *testing.T) {
	doc, err := Parse(`<div><p>First</p><p>Second</p></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}

	div := body.Children[0]
	if len(div.Children) != 2 {
		t.Fatalf("expected div to have 2 children, got %d", len(div.Children))
	}

	if div.Children[0].TagName != "p" || div.Children[1].TagName != "p" {
		t.Error("expected two p elements")
	}

	if div.Children[0].Children[0].Text != "First" {
		t.Error("expected first p to contain 'First'")
	}

	if div.Children[1].Children[0].Text != "Second" {
		t.Error("expected second p to contain 'Second'")
	}
}

func TestParser_ParentReferences(t *testing.T) {
	doc, err := Parse(`<div><p>Text</p></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	div := body.Children[0]
	if div.TagName != "div" {
		t.Fatalf("expected div, got %s", div.TagName)
	}
	p := div.Children[0]
	if p.TagName != "p" {
		t.Fatalf("expected p, got %s", p.TagName)
	}

	// Check parent references
	if p.Parent != div {
		t.Error("p's parent should be div")
	}

	if div.Parent != body {
		t.Error("div's parent should be body")
	}

	htmlNode := doc.Root.Children[0]
	if body.Parent != htmlNode {
		t.Error("body's parent should be html")
	}

	if htmlNode.Parent != doc.Root {
		t.Error("html's parent should be document root")
	}
}

// Phase 3 tests: Style tag parsing

func TestParser_StyleTag(t *testing.T) {
	doc, err := Parse(`<style>div { color: red; }</style><div></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Style tag should not appear in DOM tree; <div> goes into implicit body
	body := bodyOf(doc)
	if body == nil {
		t.Fatal("expected implicit <body> element")
	}
	if len(body.Children) != 1 {
		t.Fatalf("expected 1 child (div) in body, got %d", len(body.Children))
	}

	if body.Children[0].TagName != "div" {
		t.Errorf("expected div, got %s", body.Children[0].TagName)
	}

	// CSS should be extracted into Stylesheets
	if len(doc.Stylesheets) != 1 {
		t.Fatalf("expected 1 stylesheet, got %d", len(doc.Stylesheets))
	}

	if doc.Stylesheets[0].Text != "div { color: red; }" {
		t.Errorf("expected CSS 'div { color: red; }', got '%s'", doc.Stylesheets[0].Text)
	}
	if doc.Stylesheets[0].Href != "" {
		t.Errorf("<style>-block Href should be empty, got %q", doc.Stylesheets[0].Href)
	}
}

func TestParser_MultipleStyleTags(t *testing.T) {
	doc, err := Parse(`
		<style>div { color: red; }</style>
		<div></div>
		<style>p { color: blue; }</style>
		<p></p>
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 stylesheets
	if len(doc.Stylesheets) != 2 {
		t.Fatalf("expected 2 stylesheets, got %d", len(doc.Stylesheets))
	}

	// Whitespace is preserved in stylesheet content
	if doc.Stylesheets[0].Text != "div { color: red; }" {
		t.Errorf("first stylesheet incorrect: '%s'", doc.Stylesheets[0].Text)
	}

	if doc.Stylesheets[1].Text != "p { color: blue; }" {
		t.Errorf("second stylesheet incorrect: '%s'", doc.Stylesheets[1].Text)
	}
}

// elementsNamed returns the elements under root named tag, in document order.
func elementsNamed(root *Node, tag string) []*Node {
	var out []*Node
	if root.Type == ElementNode && root.TagName == tag {
		out = append(out, root)
	}
	for _, c := range root.Children {
		out = append(out, elementsNamed(c, tag)...)
	}
	return out
}

// TestParse_ElementNamespaceURI pins the namespace the parser gives each
// element: <svg> and <math> start SVG / MathML subtrees whose descendants
// inherit the namespace, except at HTML integration points (SVG
// foreignObject/desc/title; MathML annotation-xml with an HTML encoding)
// and MathML text integration points (mi/mo/mn/ms/mtext, except
// mglyph/malignmark children), whose start-tag children are HTML; an <svg>
// child of annotation-xml is SVG. Expected values from Blink
// HTMLTreeBuilder (html_tree_builder.cc:1198 math, :1203 svg,
// :2998-3027 ShouldProcessTokenInForeignContent, :3128
// InsertForeignElement(token, current_namespace)) and HTMLElementStack
// (html_element_stack.cc:262-290) @ d4ecdfed88f962439247c2ad36b8fe47805b1520.
func TestParse_ElementNamespaceURI(t *testing.T) {
	doc, err := Parse(`<div><svg><g><math></math><foreignObject><span></span></foreignObject>` +
		`<desc><em></em></desc></g></svg>` +
		`<math><mi><i></i><mglyph></mglyph></mi><mrow></mrow>` +
		`<annotation-xml encoding="text/html"><p></p></annotation-xml>` +
		`<annotation-xml><svg></svg></annotation-xml></math>` +
		`<svg><title><b></b></title></svg>` +
		`<math><annotation-xml encoding="Application/XHTML+XML"><u></u></annotation-xml>` +
		`<mi><malignmark></malignmark></mi><mo><s></s></mo>` +
		`<mn><kbd></kbd></mn><ms><var></var></ms><mtext><samp></samp></mtext></math></div>`)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tag  string
		idx  int // occurrence of tag, in document order
		want string
	}{
		{"html", 0, XHTMLNamespaceURI},
		{"body", 0, XHTMLNamespaceURI},
		{"div", 0, XHTMLNamespaceURI},
		{"svg", 0, SVGNamespaceURI},
		{"g", 0, SVGNamespaceURI},
		{"math", 0, SVGNamespaceURI}, // <math> inside SVG content stays SVG
		{"foreignobject", 0, SVGNamespaceURI},
		{"span", 0, XHTMLNamespaceURI}, // foreignObject is an HTML integration point
		{"desc", 0, SVGNamespaceURI},
		{"em", 0, XHTMLNamespaceURI}, // desc is an HTML integration point
		{"math", 1, MathMLNamespaceURI},
		{"mi", 0, MathMLNamespaceURI},
		{"i", 0, XHTMLNamespaceURI},       // mi is a MathML text integration point
		{"mglyph", 0, MathMLNamespaceURI}, // ... except for mglyph
		{"mrow", 0, MathMLNamespaceURI},
		{"annotation-xml", 0, MathMLNamespaceURI},
		{"p", 0, XHTMLNamespaceURI}, // annotation-xml encoding=text/html
		{"annotation-xml", 1, MathMLNamespaceURI},
		{"svg", 1, SVGNamespaceURI}, // annotation-xml > svg
		{"title", 0, SVGNamespaceURI},
		{"b", 0, XHTMLNamespaceURI},           // SVG title is an HTML integration point
		{"u", 0, XHTMLNamespaceURI},           // annotation-xml encoding=application/xhtml+xml (case-insensitive)
		{"malignmark", 0, MathMLNamespaceURI}, // mi > malignmark stays MathML
		{"mo", 0, MathMLNamespaceURI},
		{"s", 0, XHTMLNamespaceURI},    // mo, mn, ms and mtext are MathML text integration points
		{"kbd", 0, XHTMLNamespaceURI},  // mn
		{"var", 0, XHTMLNamespaceURI},  // ms
		{"samp", 0, XHTMLNamespaceURI}, // mtext
	}
	for _, tc := range cases {
		els := elementsNamed(doc.Root, tc.tag)
		if len(els) <= tc.idx {
			t.Errorf("<%s>#%d: not found (%d parsed)", tc.tag, tc.idx, len(els))
			continue
		}
		if got := els[tc.idx].NamespaceURI(); got != tc.want {
			t.Errorf("<%s>#%d: NamespaceURI() = %q, want %q", tc.tag, tc.idx, got, tc.want)
		}
	}
	if got := (&Node{Type: ElementNode, TagName: "div"}).NamespaceURI(); got != XHTMLNamespaceURI {
		t.Errorf("unset Node: NamespaceURI() = %q, want XHTML", got)
	}
}

// TestParse_ScriptDataBlockNamespaceURI pins that a non-JS (data-block)
// <script> takes its namespace the same way as every other element: inside
// <svg> it is inserted with InsertForeignElement(token, current_namespace)
// (core/html/parser/html_tree_builder.cc:3128 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), so it is an SVG element.
func TestParse_ScriptDataBlockNamespaceURI(t *testing.T) {
	doc, err := Parse(`<div><script type="text/x">a</script><svg><script type="text/x">b</script></svg></div>`)
	if err != nil {
		t.Fatal(err)
	}
	scripts := elementsNamed(doc.Root, "script")
	if len(scripts) != 2 {
		t.Fatalf("parsed %d <script> elements, want 2", len(scripts))
	}
	for i, want := range []string{XHTMLNamespaceURI, SVGNamespaceURI} {
		if got := scripts[i].NamespaceURI(); got != want {
			t.Errorf("<script>#%d: NamespaceURI() = %q, want %q", i, got, want)
		}
	}
}

// treeShape serializes the element structure under n as a compact
// s-expression: elements as `tag(children...)`, non-whitespace text as
// `"text"`. Whitespace-only text is dropped so markup indentation does not
// affect the assertion.
func treeShape(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n.Type == TextNode {
			if s := strings.TrimSpace(n.Text); s != "" {
				b.WriteString(`"` + s + `"`)
			}
			return
		}
		b.WriteString(n.TagName)
		if id := n.Attributes["id"]; id != "" {
			b.WriteString("#" + id)
		}
		if len(n.Children) == 0 {
			return
		}
		b.WriteString("(")
		first := true
		for _, c := range n.Children {
			if c.Type == TextNode && strings.TrimSpace(c.Text) == "" {
				continue
			}
			if !first {
				b.WriteString(" ")
			}
			first = false
			walk(c)
		}
		b.WriteString(")")
	}
	walk(n)
	return b.String()
}

// firstTable returns the first <table> under n in document order.
func firstTable(n *Node) *Node {
	if n.Type == ElementNode && n.TagName == "table" {
		return n
	}
	for _, c := range n.Children {
		if t := firstTable(c); t != nil {
			return t
		}
	}
	return nil
}

// TestParser_TableInsertionModes asserts the HTML-spec / Blink
// HTMLTreeBuilder tree for unclosed table structure: "in row" and "in
// cell" insertion modes close an open cell/row (CloseTheCell,
// ProcessTrEndTagForInRow), and "in table" / "in table body" insert the
// implied <tbody> / <tr> (html_tree_builder.cc:1342-1347, :1525-1530 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Expected trees match
// Chrome's DOM for the same markup.
func TestParser_TableInsertionModes(t *testing.T) {
	cases := []struct {
		name, markup, want string
	}{
		{
			// css-backgrounds/reference/border-image-006-ref.html
			name: "border-image-006-ref unclosed td/tr",
			markup: `<!DOCTYPE html><p>x<table>
    <tr><td class="green-square"><td><td class="green-square">
    <tr><td><td id="blue-square"><td>
    <tr><td class="green-square"><td><td class="green-square">
  </table>`,
			want: `table(tbody(tr(td td td) tr(td td#blue-square td) tr(td td td)))`,
		},
		{
			// css-backgrounds/box-shadow-table-border-collapse-001.html
			name: "box-shadow-table-border-collapse-001 unclosed td/tr",
			markup: `<!DOCTYPE html><table>
    <tr id="top-row"><td>1<td>2
    <tr id="bottom-row"><td>3<td>4
  </table>`,
			want: `table(tbody(tr#top-row(td("1") td("2")) tr#bottom-row(td("3") td("4"))))`,
		},
		{
			name:   "td directly in table gets implied tbody and tr",
			markup: `<table><td>a<td>b</table>`,
			want:   `table(tbody(tr(td("a") td("b"))))`,
		},
		{
			name:   "th closes an open td",
			markup: `<table><tr><td>a<th>b</table>`,
			want:   `table(tbody(tr(td("a") th("b"))))`,
		},
		{
			name:   "new cell closes elements open inside the cell",
			markup: `<table><tr><td><div><span>a<td>b</table>`,
			want:   `table(tbody(tr(td(div(span("a"))) td("b"))))`,
		},
		{
			name:   "tbody start tag closes open cell, row and row group",
			markup: `<table><tbody><tr><td>a<tbody><tr><td>b</table>`,
			want:   `table(tbody(tr(td("a"))) tbody(tr(td("b"))))`,
		},
		{
			name:   "thead after open cell",
			markup: `<table><thead><tr><th>h<tbody><tr><td>b</table>`,
			want:   `table(thead(tr(th("h"))) tbody(tr(td("b"))))`,
		},
		{
			name:   "nested table: inner cells do not close outer cell",
			markup: `<table><tr><td><table><tr><td>in<td>in2</table>after<td>2</table>`,
			want:   `table(tbody(tr(td(table(tbody(tr(td("in") td("in2")))) "after") td("2"))))`,
		},
		{
			name:   "explicitly closed table unchanged apart from tbody",
			markup: `<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>`,
			want:   `table(tbody(tr(td("a") td("b")) tr(td("c"))))`,
		},
		{
			name:   "end tag for a cell outside table scope is ignored",
			markup: `<table><tr><td><table><tr><td>x</td></tr></td></table>z</td></tr></table>`,
			want:   `table(tbody(tr(td(table(tbody(tr(td("x")))) "z"))))`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse(tc.markup)
			if err != nil {
				t.Fatal(err)
			}
			tbl := firstTable(doc.Root)
			if tbl == nil {
				t.Fatal("no <table> in parsed document")
			}
			if got := treeShape(tbl); got != tc.want {
				t.Errorf("tree mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestParser_XHTMLTableNoImpliedTbody: XML documents are not parsed by
// the HTML tree builder, so no implied <tbody> appears (Chrome's DOM for
// an .xht table has <tr> directly under <table>).
func TestParser_XHTMLTableNoImpliedTbody(t *testing.T) {
	doc, err := Parse(`<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><table><tr><td>a</td></tr></table></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	tbl := firstTable(doc.Root)
	if tbl == nil {
		t.Fatal("no <table>")
	}
	if got, want := treeShape(tbl), `table(tr(td("a")))`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
