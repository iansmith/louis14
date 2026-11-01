package html

import "strings"

// Author style sheets are derived from the document's style sheet candidate
// nodes — <style> elements and <link rel=stylesheet> elements — in tree
// order, mirroring Blink's StyleEngine candidate collection (StyleEngine::
// AddStyleSheetCandidateNode, core/css/style_engine.cc:448 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b, which StyleElement::
// ProcessStyleSheet (core/css/style_element.cc:87) and
// HTMLLinkElement::InsertedInto (core/html/html_link_element.cc:322) call
// when the element is connected).
//
// Blink processes each candidate eagerly from its InsertedInto / RemovedFrom
// / ChildrenChanged hooks and gathers the active list lazily in
// StyleEngine::UpdateActiveStyleSheets (style_engine.cc:674) before style
// recalc. louis14 keeps only the second half: UpdateActiveStyleSheets
// re-walks the tree and rebuilds Document.Stylesheets, and is called at the
// end of parsing and by pkg/js before every layout flush, so the derived
// list is current whenever the cascade reads it.

// TextFromChildren concatenates the data of n's direct Text children,
// skipping every other child (elements, comments). Mirrors
// Element::TextFromChildren (core/dom/element.cc:9983 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), the text StyleElement::Process
// (core/css/style_element.cc:163-168) builds the sheet from.
func (n *Node) TextFromChildren() string {
	// Fast path (as Blink's, element.cc:10009-10012): a single Text child's
	// data is returned as-is, which also keeps the pkg/css sheet-cache
	// Text comparison a same-pointer check.
	if len(n.Children) == 1 && n.Children[0].Type == TextNode {
		return n.Children[0].Text
	}
	var sb strings.Builder
	for _, c := range n.Children {
		if c.Type == TextNode {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// isCSSStyleType mirrors style_element.cc:60 IsCSS: a <style> creates a
// CSS sheet only when its type attribute is absent/empty or "text/css".
func isCSSStyleType(n *Node) bool {
	t, _ := n.GetAttribute("type")
	return t == "" || strings.EqualFold(t, "text/css")
}

// UpdateActiveStyleSheets rebuilds d.Stylesheets from the connected style
// sheet candidates in tree order: each <style> element's TextFromChildren
// (StyleElement::CreateSheetOrModule, core/css/style_element.cc:190-283)
// and each <link rel=stylesheet>'s loaded sheet (Node.LinkStyle). Mirrors
// StyleEngine::UpdateActiveStyleSheets (core/css/style_engine.cc:674 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// Whitespace-only <style> sheets are omitted, as louis14's parser always
// has (they contribute no rules). <template> subtrees are skipped: their
// content belongs to the template's unconnected content fragment
// (HTMLTemplateElement::content), so no candidate inside one is connected.
func (d *Document) UpdateActiveStyleSheets() {
	sheets := make([]StylesheetSource, 0, len(d.Stylesheets))
	var collect func(n *Node)
	collect = func(n *Node) {
		if n.Type == ElementNode {
			switch n.TagName {
			case "template":
				return
			case "style":
				if text := n.TextFromChildren(); isCSSStyleType(n) && strings.TrimSpace(text) != "" {
					sheets = append(sheets, StylesheetSource{Text: text})
				}
			case "link":
				if n.LinkStyle != nil {
					sheets = append(sheets, *n.LinkStyle)
				}
			}
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(d.Root)
	d.Stylesheets = sheets
}
