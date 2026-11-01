package html

import (
	"slices"
	"testing"
)

// sheetTexts returns the Text of each derived author sheet, in order.
func sheetTexts(doc *Document) []string {
	out := make([]string, len(doc.Stylesheets))
	for i, s := range doc.Stylesheets {
		out[i] = s.Text
	}
	return out
}

// TestParse_StyleElementIsInDOM pins that <style> is an HTMLStyleElement in
// the DOM (in <head>, carrying its CSS as a text child) and that the author
// sheet is derived from it — Blink HTMLStyleElement::InsertedInto →
// StyleElement::ProcessStyleSheet (core/html/html_style_element.cc:71-82 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestParse_StyleElementIsInDOM(t *testing.T) {
	doc, err := Parse(`<!DOCTYPE html><style id="s">div { color: red; }</style><div></div>`)
	if err != nil {
		t.Fatal(err)
	}
	styles := elementsNamed(doc.Root, "style")
	if len(styles) != 1 {
		t.Fatalf("got %d <style> elements in the DOM, want 1", len(styles))
	}
	s := styles[0]
	if id, _ := s.GetAttribute("id"); id != "s" {
		t.Errorf("style id = %q, want s", id)
	}
	if s.Parent == nil || s.Parent.TagName != "head" {
		t.Errorf("style parent = %v, want <head>", s.Parent)
	}
	if got := s.TextFromChildren(); got != "div { color: red; }" {
		t.Errorf("TextFromChildren = %q", got)
	}
	if got := sheetTexts(doc); !slices.Equal(got, []string{"div { color: red; }"}) {
		t.Errorf("Stylesheets = %q", got)
	}
	if body := bodyOf(doc); body == nil || len(body.Children) != 1 || body.Children[0].TagName != "div" {
		t.Errorf("body children should be just the div")
	}
}

// TestUpdateActiveStyleSheets_TreeOrder pins that <style> and <link
// rel=stylesheet> sheets are collected in document tree order (Blink's
// StyleEngine candidate list is tree-ordered: StyleEngine::
// AddStyleSheetCandidateNode, core/css/style_engine.cc:448 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestUpdateActiveStyleSheets_TreeOrder(t *testing.T) {
	doc, err := Parse(`<!DOCTYPE html><style>a{}</style>` +
		`<link rel="stylesheet" href="data:text/css,b%7B%7D">` +
		`<style>c{}</style><div></div><style>d{}</style>`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a{}", "b{}", "c{}", "d{}"}
	if got := sheetTexts(doc); !slices.Equal(got, want) {
		t.Fatalf("Stylesheets = %q, want %q", got, want)
	}
	if doc.Stylesheets[1].Href != "data:text/css,b%7B%7D" {
		t.Errorf("link sheet Href = %q", doc.Stylesheets[1].Href)
	}
}

// TestUpdateActiveStyleSheets_RederivesFromDOM pins that after DOM
// mutations the sheets are re-derived: only child Text nodes form the sheet
// (Element::TextFromChildren, core/dom/element.cc:9983 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), a removed <style> drops its
// sheet (StyleElement::RemovedFrom, core/css/style_element.cc:125), and a
// non-CSS type creates none (IsCSS, style_element.cc:60).
func TestUpdateActiveStyleSheets_RederivesFromDOM(t *testing.T) {
	doc, err := Parse(`<!DOCTYPE html><style id="a">a{}</style><style id="b">b{}</style>` +
		`<style type="text/plain">x{}</style><div></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if got := sheetTexts(doc); !slices.Equal(got, []string{"a{}", "b{}"}) {
		t.Fatalf("initial Stylesheets = %q", got)
	}
	styles := elementsNamed(doc.Root, "style")
	a, b := styles[0], styles[1]
	// An element child contributes nothing; a text child is appended.
	a.AddChild(&Node{Type: ElementNode, TagName: "div", Children: []*Node{{Type: TextNode, Text: "junk"}}})
	a.AddChild(&Node{Type: TextNode, Text: " p{}"})
	b.Parent.RemoveChild(b)
	doc.UpdateActiveStyleSheets()
	if got := sheetTexts(doc); !slices.Equal(got, []string{"a{} p{}"}) {
		t.Errorf("after mutation Stylesheets = %q, want [\"a{} p{}\"]", got)
	}
}

// TestUpdateActiveStyleSheets_SkipsTemplateContent pins that a <style>
// inside <template> is inert: its content lives in the template's content
// DocumentFragment, which is not connected (HTMLTemplateElement::content),
// so StyleElement::ProcessStyleSheet never runs for it.
func TestUpdateActiveStyleSheets_SkipsTemplateContent(t *testing.T) {
	doc, err := Parse(`<!DOCTYPE html><div></div><template><style>t{}</style></template>`)
	if err != nil {
		t.Fatal(err)
	}
	if got := sheetTexts(doc); len(got) != 0 {
		t.Errorf("Stylesheets = %q, want none", got)
	}
}

// TestParseFragment_StyleIsRawText pins that a fragment-parsed <style> reads
// its content as raw text (no tokenization of `>` as markup) and that the
// serializer emits style text unescaped (MarkupFormatter::EntityMaskForText
// → kEntityMaskInCdata, core/editing/serializers/markup_formatter.cc:394-426
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), so innerHTML round-trips.
func TestParseFragment_StyleIsRawText(t *testing.T) {
	const src = `<style>a > b { color: red }</style>`
	nodes, err := ParseFragment(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].TagName != "style" {
		t.Fatalf("got %d nodes, want one <style>", len(nodes))
	}
	if got := nodes[0].TextFromChildren(); got != "a > b { color: red }" {
		t.Errorf("TextFromChildren = %q", got)
	}
	if got := nodes[0].SerializeOuter(); got != src {
		t.Errorf("SerializeOuter = %q, want %q", got, src)
	}
}
