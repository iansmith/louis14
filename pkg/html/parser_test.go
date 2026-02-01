package html

import "testing"

func TestParser_SingleElement(t *testing.T) {
	doc, err := Parse("<div></div>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doc.Root.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(doc.Root.Children))
	}
	if doc.Root.Children[0].TagName != "div" {
		t.Errorf("expected tag 'div', got '%s'", doc.Root.Children[0].TagName)
	}
}

func TestParser_MultipleElements(t *testing.T) {
	doc, err := Parse("<div></div><p></p>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doc.Root.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(doc.Root.Children))
	}
}

func TestParser_WithAttributes(t *testing.T) {
	doc, err := Parse(`<div style="color: red"></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	style, ok := doc.Root.Children[0].GetAttribute("style")
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

	// Should have one child (div)
	if len(doc.Root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(doc.Root.Children))
	}

	div := doc.Root.Children[0]
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

	// Navigate down the tree
	div := doc.Root.Children[0]
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

	div := doc.Root.Children[0]
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

	div := doc.Root.Children[0]
	p := div.Children[0]

	// Check parent references
	if p.Parent != div {
		t.Error("p's parent should be div")
	}

	if div.Parent != doc.Root {
		t.Error("div's parent should be root")
	}
}

// Phase 3 tests: Style tag parsing

func TestParser_StyleTag(t *testing.T) {
	doc, err := Parse(`<style>div { color: red; }</style><div></div>`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Style tag should not appear in DOM tree
	if len(doc.Root.Children) != 1 {
		t.Fatalf("expected 1 child (div), got %d", len(doc.Root.Children))
	}

	if doc.Root.Children[0].TagName != "div" {
		t.Errorf("expected div, got %s", doc.Root.Children[0].TagName)
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
