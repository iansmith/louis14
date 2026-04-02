package html

import "testing"

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

	if doc.Stylesheets[0] != "div { color: red; }" {
		t.Errorf("expected CSS 'div { color: red; }', got '%s'", doc.Stylesheets[0])
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
	if doc.Stylesheets[0] != "div { color: red; }" {
		t.Errorf("first stylesheet incorrect: '%s'", doc.Stylesheets[0])
	}

	if doc.Stylesheets[1] != "p { color: blue; }" {
		t.Errorf("second stylesheet incorrect: '%s'", doc.Stylesheets[1])
	}
}
