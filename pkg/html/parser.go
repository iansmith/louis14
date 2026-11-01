package html

import (
	"fmt"
	"net/url"
	"strings"
)

// CSSFetcher is a function that fetches CSS content from a URI.
// Used to support network-based stylesheet loading.
type CSSFetcher func(uri string) (string, error)

// ScriptFetcher is a function that fetches JavaScript source from a URI,
// used to support `<script src="...">` external scripts. Same shape as
// CSSFetcher since both resolve a same-origin-relative URI to file/network
// content; kept as a distinct named type (rather than reusing CSSFetcher)
// because the two resources are conceptually different fetch lists in
// Blink (ResourceFetcher::FetchCSSStyleSheet vs FetchScript /
// third_party/blink/renderer/core/loader/resource/script_resource.h).
type ScriptFetcher func(uri string) (string, error)

type Parser struct {
	tokenizer        *Tokenizer
	doc              *Document
	stack            []*Node       // Phase 2: Stack for tracking nested elements
	cssFetcher       CSSFetcher    // Optional fetcher for external stylesheets
	scriptFetcher    ScriptFetcher // Optional fetcher for external <script src> content
	fragmentMode     bool          // When true, <script> becomes a DOM node
	commentSeenInPre bool          // True when a comment has been seen inside a <pre> element
	isXML            bool          // True for XML/XHTML documents (governs <style> content rules)
}

func NewParser(html string) *Parser {
	return &Parser{
		tokenizer: NewTokenizer(html),
		doc:       NewDocument(),
		isXML:     looksLikeXML(html),
	}
}

func (p *Parser) Parse() (*Document, error) {
	// Phase 2: Initialize stack with root node
	p.stack = []*Node{p.doc.Root}

	for {
		token, err := p.tokenizer.NextToken()
		if err != nil {
			return nil, fmt.Errorf("tokenizer error: %w", err)
		}
		if token.Type == TokenEOF {
			break
		}

		switch token.Type {
		case TokenStartTag:
			// Special handling for <script> tags in normal mode: extract
			// raw content. In fragment mode, treat them as DOM nodes.
			if !p.fragmentMode {
				if token.TagName == "script" {
					content := p.tokenizer.ReadRawUntil("script")
					// HTML §4.12.1: only `type=""`, `type="text/javascript"`,
					// or a JavaScript MIME-type subset cause the script to
					// be executed. Other types (`text/html`,
					// `application/json`, …) are DATA blocks — they remain
					// in the DOM as inert `<script>` elements whose
					// textContent can be read by other scripts. The
					// effect-reference-feimage-003.html WPT reftest uses
					// `<script type="text/html" id="source">` as a template
					// container and copies its textContent via
					// document.getElementById; treating it as executable JS
					// drops the markup entirely and silently breaks the
					// test.
					typ := ""
					if t, ok := token.Attributes["type"]; ok {
						typ = strings.ToLower(strings.TrimSpace(t))
					}
					isExecutableJS := typ == "" || typ == "text/javascript" ||
						typ == "application/javascript" || typ == "application/ecmascript" ||
						typ == "text/ecmascript" || typ == "module"
					if isExecutableJS {
						// HTML5 §4.12.1: a `src` attribute makes this an
						// external script — Blink's HTMLScriptElement
						// fetches and runs the external resource INSTEAD OF
						// any inline text (third_party/blink/renderer/core/
						// html/script_runner.cc). Mirror that precedence:
						// src wins over inline content when both are
						// present (malformed markup, but match Blink).
						if src, ok := token.Attributes["src"]; ok && strings.TrimSpace(src) != "" {
							// src wins over inline content even when the
							// fetch fails (scriptFetcher nil, or the fetch
							// itself errors) — matching the comment above:
							// Blink never falls back to inline text for an
							// external script that 404s/fails to load.
							if p.scriptFetcher != nil {
								if fetched, err := p.scriptFetcher(strings.TrimSpace(src)); err == nil && strings.TrimSpace(fetched) != "" {
									p.doc.Scripts = append(p.doc.Scripts, fetched)
								}
							}
							continue
						}
						if strings.TrimSpace(content) != "" {
							p.doc.Scripts = append(p.doc.Scripts, content)
						}
						continue
					}
					// Non-JS `<script>` — preserve as a DOM element with
					// its content as a text-node child so the JS engine can
					// read `.textContent`.
					node := &Node{
						Type:       ElementNode,
						TagName:    "script",
						Attributes: token.Attributes,
						Children:   make([]*Node, 0),
					}
					if content != "" {
						node.Children = append(node.Children, &Node{
							Type:    TextNode,
							Text:    content,
							RawText: content,
							Parent:  node,
						})
					}
					insertElement(node, p.currentParent())
					continue
				}
			}

			// Auto-close <p> when a block-level element is encountered inside it
			if p.isBlockElement(token.TagName) {
				p.autoCloseP()
			}

			// HTML5 implied end tags: <li> closes an open <li>;
			// <dt>/<dd> close an open <dt>/<dd>.
			p.autoCloseListItem(token.TagName)

			// Table insertion modes: close an open cell/row/row group and
			// insert implied <tbody>/<tr>/<colgroup> (Blink HTMLTreeBuilder).
			p.processStartTagForTable(token.TagName)

			// Create new element node
			node := &Node{
				Type:       ElementNode,
				TagName:    token.TagName,
				Attributes: token.Attributes,
				Children:   make([]*Node, 0),
			}

			// Add to current parent (top of stack)
			parent := p.currentParent()

			// HTML5 §13.2.6.4.7 "in body" insertion mode: a second <body> start
			// tag is a parse error and is ignored as an element (attributes are
			// merged into the existing body, but that's not load-bearing for
			// layout). Skip creating a new element if a body is already on the
			// stack — important when content before <body> (e.g. <p>text</p>)
			// triggered an implicit body, and an explicit <body> follows.
			if token.TagName == "body" {
				alreadyHaveBody := false
				for _, n := range p.stack {
					if n.TagName == "body" {
						alreadyHaveBody = true
						break
					}
				}
				if alreadyHaveBody {
					continue
				}
			}

			// HTML5 §12.2.6.4: Ensure proper html > body structure.
			// Block-level content and <body> tags must be inside <body>.
			// This applies both at document root AND inside <html> (after </head>).
			if parent.TagName == "document" {
				if token.TagName == "body" {
					p.ensureHTML()
					parent = p.currentParent()
					if parent.TagName == "body" {
						continue // already have body on stack
					}
				} else if token.TagName == "html" {
					p.adoptOrphanedChildren(node)
					parent = p.currentParent()
				} else if !isHeadElement(token.TagName) {
					// HTML5 §12.2.6.4: Any body-content element (block or inline)
					// at the document root must be placed in an implicit <body>.
					p.ensureBody()
					parent = p.currentParent()
				}
			} else if parent.TagName == "html" && token.TagName != "head" && token.TagName != "body" {
				// Content inside <html> but outside <body>/<head>:
				// create implicit <body> (HTML5 §12.2.6.4.7).
				if token.TagName == "head" {
					// <head> goes directly into <html>, no body needed.
				} else if p.isBlockElement(token.TagName) || !isHeadElement(token.TagName) {
					p.ensureBody()
					parent = p.currentParent()
				}
			} else if parent.TagName == "head" && !isHeadElement(token.TagName) {
				// HTML5 "in head" insertion mode: a non-head-content element
				// (e.g. <div> immediately after </style> with no </head>) closes
				// <head> implicitly and transitions to body content.
				p.closeTag("head")
				p.ensureBody()
				parent = p.currentParent()
			}

			insertElement(node, parent)

			if token.TagName == "style" {
				// <style> is a raw-text element (HTML §13.2.6.4.4 "in
				// head": generic raw text element parsing algorithm): its
				// content is one Text child, never tokenized as markup,
				// and the end tag is consumed with it, so the element is
				// never pushed. It stays in the DOM as an HTMLStyleElement
				// (placed above as head content, see isHeadElement) and
				// the author sheet is derived from it by
				// Document.UpdateActiveStyleSheets at the end of Parse —
				// Blink HTMLStyleElement::InsertedInto →
				// StyleElement::ProcessStyleSheet (core/html/
				// html_style_element.cc:71-82 @
				// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
				styleText := p.tokenizer.ReadRawUntil("style")
				if p.isXML {
					// In XML/XHTML, <style> content is element text, so the
					// XML processor strips whole comments — delimiters AND
					// the prose inside them — before the CSS parser ever
					// sees it. (In HTML, <style> is rawtext and only the
					// CDO/CDC `<!--`/`-->` delimiters are stripped by the
					// CSS tokenizer, leaving any prose between them.) Mirror
					// the XML processor so a `<!-- note -->` between two
					// rules can't contaminate the following selector.
					styleText = stripXMLComments(styleText)
				}
				styleText = stripCDATA(styleText)
				node.AppendTextWithRaw(styleText, styleText)
				continue
			}

			// Handle <meta name="viewport" content="width=...">
			if token.TagName == "meta" {
				if name, ok := token.Attributes["name"]; ok && strings.EqualFold(name, "viewport") {
					if content, ok := token.Attributes["content"]; ok {
						p.doc.ViewportWidth = parseViewportWidth(content)
					}
				}
			}

			// Handle <link rel="stylesheet">.
			if token.TagName == "link" {
				if rel, ok := token.Attributes["rel"]; ok {
					if strings.Contains(rel, "stylesheet") {
						if href, ok := token.Attributes["href"]; ok {
							if css := p.loadLinkStylesheet(href); css != "" {
								// External sheet — capture the href so the
								// CSS layer can derive per-sheet BaseDir
								// against the (possibly later-set)
								// doc.BaseDir, so url() refs inside resolve
								// against the sheet's own location rather
								// than the document's. The sheet hangs off
								// the element (HTMLLinkElement's LinkStyle)
								// and joins doc.Stylesheets in tree order
								// via UpdateActiveStyleSheets.
								node.LinkStyle = &StylesheetSource{
									Text: css,
									Href: href,
								}
							}
						}
					}
				}
			}

			// Check if this is a self-closing/void element
			// In XHTML, any element can be self-closing with /> syntax
			if !p.isSelfClosing(token.TagName) && !token.SelfClosing {
				// Push onto stack to become new parent
				p.push(node)
			}

		case TokenComment:
			// HTML comment inside a <pre>: treat it as a child for the purpose of
			// the leading-newline strip rule. HTML5 §13.2.6.4.1 says to strip only
			// the first newline of a <pre> text node when the <pre> has no prior
			// children; a comment before the text counts as such a child.
			if p.isInsidePreformatted() {
				p.commentSeenInPre = true
			}

		case TokenText:
			// Add text to current parent
			// Use raw text (preserving whitespace) inside preformatted elements
			parent := p.currentParent()
			text := token.Text
			rawText := token.RawText
			if rawText != "" && p.isInsidePreformatted() {
				text = rawText
				// HTML spec: strip a single leading newline in <pre> elements.
				// This applies to the first text node immediately after the <pre> tag,
				// but only when no comment preceded the text (a comment acts as a
				// child, suppressing the strip — matching browsers and the WPT tests).
				if parent.TagName == "pre" && len(parent.Children) == 0 && !p.commentSeenInPre && len(text) > 0 && text[0] == '\n' {
					text = text[1:]
					rawText = text
				}
			}
			if text != "" {
				// HTML5 §12.2.6.4: bare non-whitespace text outside <body> must be
				// placed in an implicit <body>. This applies both at document root
				// (no <html>/<body>) and inside <html> after </head> (no explicit
				// <body>). HTML5 "after head" insertion mode treats a non-whitespace
				// character as an implicit <body> start tag.
				if (parent.TagName == "document" || parent.TagName == "html") && strings.TrimSpace(text) != "" {
					p.ensureBody()
					parent = p.currentParent()
				}
				// Always store rawText so that CSS white-space:pre-wrap can restore
				// original spacing at layout time, even when set via stylesheet rules.
				parent.AppendTextWithRaw(text, rawText)
			}

		case TokenEndTag:
			// Reset commentSeenInPre when leaving a <pre> element.
			if token.TagName == "pre" {
				p.commentSeenInPre = false
			}
			// Pop stack until we find the matching tag
			p.closeTag(token.TagName)
		}
	}

	// HTML5 §13.2.6 / CSS Backgrounds 3 §2.11.2: the html element is
	// always implicit. When a document contains only head-level content
	// (title, link, style) with no body content to trigger ensureBody,
	// no implicit <html> is created during parsing. Synthesize one here
	// so CSS rules targeting `html { ... }` (e.g. body-less canvas
	// backgrounds) can apply to a real root element.
	if !p.fragmentMode {
		p.ensureFinalHTML()
		p.doc.UpdateActiveStyleSheets()
	}

	return p.doc, nil
}

// ensureFinalHTML synthesizes an implicit <html> at document close when
// no html element exists in doc.Root, moving all head-level orphans
// (title, link, style, etc.) into a <head> under it. Mirrors the
// "after html" insertion-mode fallback in HTML5 §13.2.6.
func (p *Parser) ensureFinalHTML() {
	root := p.doc.Root
	if root == nil {
		return
	}
	for _, child := range root.Children {
		if child.Type == ElementNode && child.TagName == "html" {
			return // already present
		}
	}
	htmlNode := &Node{
		Type:       ElementNode,
		TagName:    "html",
		Attributes: map[string]string{},
		Children:   make([]*Node, 0),
	}
	p.moveOrphanedHeadElements(root, htmlNode)
	// Move any remaining non-head children into an implicit <body>.
	var bodyChildren []*Node
	var remaining []*Node
	for _, child := range root.Children {
		if child.Type == ElementNode && isHeadElement(child.TagName) {
			remaining = append(remaining, child)
			continue
		}
		bodyChildren = append(bodyChildren, child)
	}
	if len(bodyChildren) > 0 {
		bodyNode := &Node{
			Type:       ElementNode,
			TagName:    "body",
			Attributes: map[string]string{},
			Children:   make([]*Node, 0),
		}
		for _, c := range bodyChildren {
			c.Parent = bodyNode
			bodyNode.Children = append(bodyNode.Children, c)
		}
		htmlNode.AddChild(bodyNode)
	}
	htmlNode.Parent = root
	root.Children = append(remaining, htmlNode)
}

// currentParent returns the current parent node (top of stack)
func (p *Parser) currentParent() *Node {
	if len(p.stack) == 0 {
		return p.doc.Root
	}
	return p.stack[len(p.stack)-1]
}

// push adds a node to the stack
func (p *Parser) push(node *Node) {
	p.stack = append(p.stack, node)
}

// pop removes the top node from the stack
func (p *Parser) pop() *Node {
	if len(p.stack) == 0 {
		return nil
	}
	node := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	return node
}

// isSelfClosing returns true for void/self-closing HTML elements
func (p *Parser) isSelfClosing(tagName string) bool {
	selfClosingTags := map[string]bool{
		"br": true, "hr": true, "img": true, "input": true,
		"meta": true, "link": true, "area": true, "base": true,
		"col": true, "embed": true, "param": true, "source": true,
		"track": true, "wbr": true,
	}
	return selfClosingTags[tagName]
}

// ensureBody creates implicit <html> and <body> elements if they are not
// already on the parser stack. Called when bare text or non-structural
// elements appear as direct children of the document root (e.g., text after
// <!DOCTYPE html> without explicit <html>/<body> tags — HTML5 §8.2.6.4).
// ensureHTML creates an implicit <html> element at the document root if one
// doesn't already exist on the parser stack. Moves orphaned head-level elements
// (meta, title, link) from document.Root into an implicit <head> inside <html>.
func (p *Parser) ensureHTML() {
	for _, node := range p.stack {
		if node.TagName == "html" {
			return // <html> already on stack
		}
	}
	htmlNode := &Node{
		Type:       ElementNode,
		TagName:    "html",
		Attributes: map[string]string{},
		Children:   make([]*Node, 0),
	}
	docRoot := p.currentParent()
	// Move orphaned head-level elements into <html>/<head>.
	p.moveOrphanedHeadElements(docRoot, htmlNode)
	docRoot.AddChild(htmlNode)
	p.push(htmlNode)
}

// ensureBody creates implicit <html> and <body> elements at the document root
// if they don't already exist on the parser stack. Used when block-level content
// appears at the document root without explicit <html>/<body> tags.
func (p *Parser) ensureBody() {
	// First ensure <html> exists.
	p.ensureHTML()
	// Check if <body> is already on the stack.
	for _, node := range p.stack {
		if node.TagName == "body" {
			return
		}
	}
	// Create implicit <body> inside <html>.
	bodyNode := &Node{
		Type:       ElementNode,
		TagName:    "body",
		Attributes: map[string]string{},
		Children:   make([]*Node, 0),
	}
	p.currentParent().AddChild(bodyNode)
	p.push(bodyNode)
}

// moveOrphanedHeadElements moves meta, title, link, and other head-level
// elements from the document root into an implicit <head> inside the html node.
func (p *Parser) moveOrphanedHeadElements(docRoot, htmlNode *Node) {
	var headElements []*Node
	var remaining []*Node
	for _, child := range docRoot.Children {
		if child.Type == ElementNode && isHeadElement(child.TagName) {
			headElements = append(headElements, child)
		} else {
			remaining = append(remaining, child)
		}
	}
	if len(headElements) > 0 {
		headNode := &Node{
			Type:       ElementNode,
			TagName:    "head",
			Attributes: map[string]string{},
			Children:   make([]*Node, 0),
		}
		for _, el := range headElements {
			el.Parent = headNode
			headNode.Children = append(headNode.Children, el)
		}
		htmlNode.AddChild(headNode)
		docRoot.Children = remaining
	}
}

// adoptOrphanedChildren moves orphaned children from document.Root into an
// explicit <html> element. Called when an explicit <html> tag is encountered
// after head-level elements were already added to document.Root.
func (p *Parser) adoptOrphanedChildren(htmlNode *Node) {
	docRoot := p.currentParent()
	if docRoot.TagName != "document" {
		return
	}
	for _, child := range docRoot.Children {
		if child.Type == ElementNode && isHeadElement(child.TagName) {
			// Find or create <head> inside htmlNode
			var headNode *Node
			for _, hc := range htmlNode.Children {
				if hc.TagName == "head" {
					headNode = hc
					break
				}
			}
			if headNode == nil {
				headNode = &Node{
					Type:       ElementNode,
					TagName:    "head",
					Attributes: map[string]string{},
					Children:   make([]*Node, 0),
				}
				htmlNode.AddChild(headNode)
			}
			child.Parent = headNode
			headNode.Children = append(headNode.Children, child)
		}
	}
	// Remove adopted children from document root
	var remaining []*Node
	for _, child := range docRoot.Children {
		if child.Type != ElementNode || !isHeadElement(child.TagName) {
			remaining = append(remaining, child)
		}
	}
	docRoot.Children = remaining
}

// isHeadElement returns true for elements that belong in <head>.
func isHeadElement(tagName string) bool {
	switch tagName {
	case "meta", "title", "link", "base", "noscript", "style":
		return true
	}
	return false
}

// closeTag pops the stack until the matching tag is found and closed
func (p *Parser) closeTag(tagName string) {
	// Per HTML5 §12.2.6: </html> and </body> end tags should not actually
	// remove the element from the stack in most contexts. Ignoring them
	// prevents malformed HTML (e.g. </html> appearing before </head>)
	// from orphaning subsequent content.
	if tagName == "html" || tagName == "body" {
		return
	}
	// Table-structure end tags act only when the element is in table scope
	// (ProcessEndTagForInTable / InTableBody / InRow / InCell,
	// html_tree_builder.cc:2007-2117, :2319-2345 @
	// a9f50e522efa9005e6ec765a9a785c74f5c2c86b); popping through it also
	// covers CloseTheCell and the fake </tr> those modes issue first. Any
	// other end tag never pops past a cell, caption or table: those are
	// scope markers for "has an element in scope" (IsScopeMarkerTag,
	// html_element_stack.cc:47-56) and special elements for
	// ProcessAnyOtherEndTagForInBody (html_tree_builder.cc:1730) @ the same
	// SHA. Unmatched end tags are ignored.
	marker := isCellOrTableScopeMarker
	if tagName == "caption" || tagName == "colgroup" || tagName == "tr" || tagName == "table" ||
		isTableCellContextTag(tagName) || isTableBodyContextTag(tagName) {
		marker = isTableScopeMarker
	}
	if i := p.inScopeCommon(marker, tagName); i >= 0 {
		p.popUntilPopped(i)
	}
}

// insertionMode is the subset of Blink's HTMLTreeBuilder::InsertionMode
// (core/html/parser/html_tree_builder.h @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) that the table start-tag rules
// distinguish. louis14's tree builder keeps no explicit mode state: the mode
// is recomputed from the stack of open elements on demand, exactly as
// ResetInsertionModeAppropriately does (html_tree_builder.cc:1949-2005 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Every non-table mode collapses
// to inOtherMode, where the table rules do not apply.
type insertionMode int

const (
	inOtherMode insertionMode = iota
	inTableMode
	inCaptionMode
	inColumnGroupMode
	inTableBodyMode
	inRowMode
	inCellMode
)

// resetInsertionModeAppropriately mirrors HTMLTreeBuilder::
// ResetInsertionModeAppropriately (html_tree_builder.cc:1949-2005 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), restricted to the table
// modes: the nearest table-structure element on the stack picks the mode;
// template/head/body/html (or reaching the document root) means a non-table
// mode. Only HTML-namespace items count (item->IsHTMLNamespace()).
func (p *Parser) resetInsertionModeAppropriately() insertionMode {
	for i := len(p.stack) - 1; i >= 1; i-- {
		n := p.stack[i]
		if n.NamespaceURI() != XHTMLNamespaceURI {
			continue
		}
		switch n.TagName {
		case "td", "th":
			return inCellMode
		case "tr":
			return inRowMode
		case "tbody", "thead", "tfoot":
			return inTableBodyMode
		case "caption":
			return inCaptionMode
		case "colgroup":
			return inColumnGroupMode
		case "table":
			return inTableMode
		case "template", "head", "body", "frameset", "html":
			return inOtherMode
		}
	}
	return inOtherMode
}

func isTableCellContextTag(tag string) bool { return tag == "td" || tag == "th" } // TABLE_CELL_CONTEXT_CASES
func isTableBodyContextTag(tag string) bool {
	return tag == "tbody" || tag == "thead" || tag == "tfoot" // TABLE_BODY_CONTEXT_CASES
}
func isCaptionColOrColgroupTag(tag string) bool {
	return tag == "caption" || tag == "col" || tag == "colgroup" // CAPTION_COL_OR_COLGROUP_CASES
}

// isTableStructureTag reports the start tags that end an open cell or row:
// the union of the case labels at html_tree_builder.cc:1557-1559 / 1573-1576
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func isTableStructureTag(tag string) bool {
	return isCaptionColOrColgroupTag(tag) || isTableCellContextTag(tag) ||
		tag == "tr" || isTableBodyContextTag(tag)
}

// Scope markers, html_element_stack.cc:94-136 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b (HTML namespace only). The
// document root (stack[0]) stands in for html when no <html> is open.
func isTableScopeMarker(n *Node) bool {
	return n.NamespaceURI() == XHTMLNamespaceURI &&
		(n.TagName == "html" || n.TagName == "table" || n.TagName == "template")
}
func isTableBodyScopeMarker(n *Node) bool {
	return n.NamespaceURI() == XHTMLNamespaceURI && (n.TagName == "html" ||
		isTableBodyContextTag(n.TagName) || n.TagName == "template")
}
func isTableRowScopeMarker(n *Node) bool {
	return n.NamespaceURI() == XHTMLNamespaceURI &&
		(n.TagName == "html" || n.TagName == "tr" || n.TagName == "template")
}

// popUntil pops open elements until the current node satisfies marker or
// only the document root remains. Shared body of HTMLElementStack::
// PopUntilTableScopeMarker / PopUntilTableBodyScopeMarker /
// PopUntilTableRowScopeMarker (html_element_stack.cc:243-259 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *Parser) popUntil(marker func(*Node) bool) {
	for len(p.stack) > 1 && !marker(p.currentParent()) {
		p.pop()
	}
}

// inScopeCommon mirrors HTMLElementStack's InScopeCommon template
// (html_element_stack.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): the
// index of the topmost open element named one of tags, searching down to the
// first element satisfying marker (inclusive), or -1.
func (p *Parser) inScopeCommon(marker func(*Node) bool, tags ...string) int {
	for i := len(p.stack) - 1; i >= 1; i-- {
		n := p.stack[i]
		for _, t := range tags {
			if n.TagName == t {
				return i
			}
		}
		if marker(n) {
			return -1
		}
	}
	return -1
}

// inTableScope mirrors HTMLElementStack::InTableScope
// (html_element_stack.cc:484-491 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *Parser) inTableScope(tags ...string) int {
	return p.inScopeCommon(isTableScopeMarker, tags...)
}

// isCellOrTableScopeMarker is the table-related subset of IsScopeMarkerTag
// (html_element_stack.cc:47-56 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b)
// that bounds non-table end tags.
func isCellOrTableScopeMarker(n *Node) bool {
	return n.NamespaceURI() == XHTMLNamespaceURI &&
		(isTableCellContextTag(n.TagName) || n.TagName == "caption" || n.TagName == "table")
}

// popUntilPopped pops open elements through stack[i] (HTMLElementStack::
// PopUntilPopped, after the caller located the element).
func (p *Parser) popUntilPopped(i int) {
	p.stack = p.stack[:i]
}

// processFakeStartTag inserts an implied element (no attributes) as the
// current node — HTMLTreeBuilder::ProcessFakeStartTag
// (html_tree_builder.cc:599 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), used
// for the implied <tbody>, <tr> and <colgroup>.
func (p *Parser) processFakeStartTag(tag string) {
	node := &Node{Type: ElementNode, TagName: tag, Attributes: map[string]string{}, Children: make([]*Node, 0)}
	insertElement(node, p.currentParent())
	p.push(node)
}

// closeTheCell mirrors HTMLTreeBuilder::CloseTheCell
// (html_tree_builder.cc:1304-1314 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
// the fake </td> or </th> pops through the open cell
// (ProcessEndTagForInCell, :2096-2107), leaving the row as current node.
func (p *Parser) closeTheCell() bool {
	i := p.inTableScope("td", "th")
	if i < 0 {
		return false
	}
	p.popUntilPopped(i)
	return true
}

// processTrEndTagForInRow mirrors HTMLTreeBuilder::ProcessTrEndTagForInRow
// (html_tree_builder.cc:2306-2317 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func (p *Parser) processTrEndTagForInRow() bool {
	if p.inTableScope("tr") < 0 {
		return false
	}
	p.popUntil(isTableRowScopeMarker)
	p.pop() // the <tr>
	return true
}

// processStartTagForTable runs the table insertion modes' start-tag rules
// for tag before the generic insertion path inserts it as the current
// node. It mirrors the kInTableMode / kInCaptionMode / kInColumnGroupMode /
// kInTableBodyMode / kInRowMode / kInCellMode arms of
// HTMLTreeBuilder::ProcessStartTag (html_tree_builder.cc:1497-1590 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) plus ProcessStartTagForInTable
// (:1316-1393). Blink's "reprocess the token" (ProcessStartTag(token) after a
// mode change) is the loop's continue; returning lets the caller insert the
// element.
//
// Not modelled: foster parenting of non-table content inside a table (the
// RedirectToFosterParentGuard at :1392) and the active-formatting-element
// markers; such content stays where louis14 already puts it (the current
// node). XML documents are parsed by Blink's XMLDocumentParser, not the
// HTML tree builder, so none of this applies to them.
func (p *Parser) processStartTagForTable(tag string) {
	if p.isXML {
		return
	}
	// Tokens in foreign content bypass the insertion modes
	// (HTMLTreeBuilder::ProcessToken → ProcessTokenInForeignContent). The
	// loop below only pops or inserts HTML elements, so this holds for
	// every reprocess iteration.
	if cur := p.currentParent(); cur.Type == ElementNode && cur.NamespaceURI() != XHTMLNamespaceURI &&
		shouldProcessTokenInForeignContent(tag, cur) {
		return
	}
	for {
		switch p.resetInsertionModeAppropriately() {
		case inCellMode: // :1571-1590
			if !isTableStructureTag(tag) || !p.closeTheCell() {
				return
			}
		case inRowMode: // :1548-1570
			switch {
			case isTableCellContextTag(tag):
				p.popUntil(isTableRowScopeMarker)
				return
			case tag == "tr" || isCaptionColOrColgroupTag(tag) || isTableBodyContextTag(tag):
				if !p.processTrEndTagForInRow() {
					return
				}
			default:
				if !p.processStartTagForInTable(tag) {
					return
				}
			}
		case inTableBodyMode: // :1516-1547
			switch {
			case tag == "tr":
				p.popUntil(isTableBodyScopeMarker)
				return
			case isTableCellContextTag(tag):
				p.popUntil(isTableBodyScopeMarker) // ProcessFakeStartTag(kTr) → the tr arm
				p.processFakeStartTag("tr")
			case isCaptionColOrColgroupTag(tag) || isTableBodyContextTag(tag):
				if p.inTableScope("tbody", "thead", "tfoot") < 0 {
					return
				}
				p.popUntil(isTableBodyScopeMarker)
				p.pop() // ProcessFakeEndTag(current table body)
			default:
				if !p.processStartTagForInTable(tag) {
					return
				}
			}
		case inCaptionMode: // :1478-1489 → ProcessCaptionEndTagForInCaption
			i := p.inTableScope("caption")
			if !isTableStructureTag(tag) || i < 0 {
				return
			}
			p.popUntilPopped(i)
		case inColumnGroupMode: // :1496-1515 → ProcessColgroupEndTagForInColumnGroup
			if tag == "col" || tag == "template" || p.currentParent().TagName != "colgroup" {
				return
			}
			p.pop()
		case inTableMode:
			if !p.processStartTagForInTable(tag) {
				return
			}
		default:
			return
		}
	}
}

// processStartTagForInTable mirrors HTMLTreeBuilder::ProcessStartTagForInTable
// (html_tree_builder.cc:1316-1393 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
// It reports whether the token must be reprocessed in the new mode.
func (p *Parser) processStartTagForInTable(tag string) bool {
	switch {
	case tag == "caption" || tag == "colgroup" || isTableBodyContextTag(tag):
		p.popUntil(isTableScopeMarker)
		return false
	case tag == "col":
		p.popUntil(isTableScopeMarker) // ProcessFakeStartTag(kColgroup) → the colgroup arm
		p.processFakeStartTag("colgroup")
		return true
	case isTableCellContextTag(tag) || tag == "tr":
		p.popUntil(isTableScopeMarker) // ProcessFakeStartTag(kTbody) → the tbody arm
		p.processFakeStartTag("tbody")
		return true
	case tag == "table":
		// ProcessTableEndTagForInTable (:2319-2328): a <table> start tag in
		// table context closes the open table.
		i := p.inTableScope("table")
		if i < 0 {
			return false
		}
		p.popUntilPopped(i)
		return true
	}
	return false
}

// autoCloseP closes an open <p> element if one is on the stack
func (p *Parser) autoCloseP() {
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].TagName == "p" {
			p.stack = p.stack[:i]
			return
		}
		// Don't close past block-level containers
		if p.isBlockElement(p.stack[i].TagName) {
			return
		}
	}
}

// autoCloseListItem implements the HTML5 "in body" implied end tags for
// list items and definition list items (§13.2.6.4.7): a new <li> start tag
// closes any open <li> up to the nearest list container (ol/ul/menu — the
// "list item scope" boundary), and a new <dt>/<dd> closes any open <dt>/<dd>
// up to the nearest <dl>. Without this, `<li>a<li>b` nests b inside a,
// corrupting list-item counter scoping and box nesting.
func (p *Parser) autoCloseListItem(tagName string) {
	var closes func(string) bool
	var boundary func(string) bool
	switch tagName {
	case "li":
		closes = func(t string) bool { return t == "li" }
		boundary = func(t string) bool { return t == "ol" || t == "ul" || t == "menu" }
	case "dt", "dd":
		closes = func(t string) bool { return t == "dt" || t == "dd" }
		boundary = func(t string) bool { return t == "dl" }
	default:
		return
	}
	for i := len(p.stack) - 1; i >= 1; i-- {
		t := p.stack[i].TagName
		if closes(t) {
			p.stack = p.stack[:i]
			return
		}
		if boundary(t) {
			return
		}
	}
}

// isInsidePreformatted returns true if the current parse position is inside
// a preformatted element (<pre>, <xmp>, <listing>) where whitespace should be preserved.
func (p *Parser) isInsidePreformatted() bool {
	for i := len(p.stack) - 1; i >= 0; i-- {
		switch p.stack[i].TagName {
		case "pre", "xmp", "listing", "textarea":
			return true
		}
	}
	return false
}

// isBlockElement returns true for elements that auto-close <p>
func (p *Parser) isBlockElement(tagName string) bool {
	switch tagName {
	case "address", "article", "aside", "blockquote", "details", "dialog",
		"dd", "div", "dl", "dt", "fieldset", "figcaption", "figure",
		"footer", "form", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "hgroup", "hr", "li", "main", "nav", "ol",
		"p", "pre", "section", "table", "ul":
		return true
	}
	return false
}

// loadLinkStylesheet loads CSS from a data URI href or via the CSS fetcher.
// @import resolution is deferred to CSS-parse time (pkg/css/at_import.go)
// so each imported sheet's URL identity survives into its own ParserContext.
func (p *Parser) loadLinkStylesheet(href string) string {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "data:text/css,") {
		encoded := href[len("data:text/css,"):]
		decoded, err := url.PathUnescape(encoded)
		if err != nil {
			return encoded
		}
		return decoded
	}
	if p.cssFetcher != nil {
		if css, err := p.cssFetcher(href); err == nil {
			return css
		}
	}
	return ""
}

func Parse(html string) (*Document, error) {
	parser := NewParser(html)
	return parser.Parse()
}

// ParseWithFetcher parses HTML and uses the provided fetcher to load external
// stylesheets referenced by <link rel="stylesheet"> tags. The same fetcher is
// stamped onto the resulting Document.CSSResourceFetcher so the pkg/css layer
// can resolve @import targets at CSS-parse time.
func ParseWithFetcher(htmlContent string, cssFetcher CSSFetcher) (*Document, error) {
	return ParseWithScriptFetcher(htmlContent, cssFetcher, nil)
}

// ParseWithScriptFetcher is ParseWithFetcher plus a fetcher for external
// `<script src="...">` content. scriptFetcher may be nil, in which case
// external scripts are silently skipped (same as before this function
// existed) — mirrors Blink's ScriptLoader, which no-ops a script load that
// the ResourceFetcher cannot resolve rather than failing the parse.
func ParseWithScriptFetcher(htmlContent string, cssFetcher CSSFetcher, scriptFetcher ScriptFetcher) (*Document, error) {
	parser := NewParser(htmlContent)
	parser.cssFetcher = cssFetcher
	parser.scriptFetcher = scriptFetcher
	doc, err := parser.Parse()
	if doc != nil {
		doc.CSSResourceFetcher = cssFetcher
	}
	return doc, err
}

// ParseFragment parses an HTML fragment string and returns detached child nodes.
// Unlike Parse, <script> tags become DOM nodes instead of being extracted
// into Document.Scripts (<style> is a DOM node in both modes).
//
// The full-document parser auto-wraps bare text and body-level elements in
// synthetic <html><body>…</body></html> per HTML5 §12.2.6.4. For fragments
// produced by element.innerHTML= (HTML5 §13.4 "Fragment parsing algorithm"),
// those wrappers must not appear in the result — the fragment's content
// becomes direct children of the context element. Strip a single top-level
// synthetic <html> wrapper and return the body's children instead (plus any
// head children, flattened, so script/style survive innerHTML round-trips).
func ParseFragment(htmlContent string) ([]*Node, error) {
	parser := NewParser(htmlContent)
	parser.fragmentMode = true
	doc, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	rootChildren := doc.Root.Children
	// Unwrap the synthetic <html> → <head>? + <body> created by the document
	// parser. Per the fragment parsing algorithm, innerHTML content sits
	// directly inside the context element with no html/body shell.
	if len(rootChildren) == 1 && rootChildren[0].Type == ElementNode && rootChildren[0].TagName == "html" {
		var flat []*Node
		for _, c := range rootChildren[0].Children {
			if c.Type == ElementNode && (c.TagName == "head" || c.TagName == "body") {
				flat = append(flat, c.Children...)
			} else {
				flat = append(flat, c)
			}
		}
		rootChildren = flat
	}
	// Detach from any synthetic parent so adopters own the nodes outright.
	children := make([]*Node, len(rootChildren))
	copy(children, rootChildren)
	for _, child := range children {
		child.Parent = nil
	}
	doc.Root.Children = nil
	return children, nil
}

// parseViewportWidth extracts the width value from a viewport meta content string.
// e.g. "width=1120, initial-scale=1" → 1120. Returns 0 if not found or not numeric.
func parseViewportWidth(content string) int {
	for _, part := range strings.Split(content, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "width=") {
			val := strings.TrimSpace(part[6:])
			if val == "device-width" {
				return 0 // ignore device-width
			}
			var w int
			if _, err := fmt.Sscanf(val, "%d", &w); err == nil && w > 0 {
				return w
			}
		}
	}
	return 0
}

// looksLikeXML reports whether the document is XML/XHTML, mirroring Blink's
// choice of XMLDocumentParser (for `application/xhtml+xml`) over
// HTMLDocumentParser. louis14's parse entry points receive only the byte
// stream, not a MIME type, so we key off the definitive in-band XML signal:
// an `<?xml …?>` prolog, which is only legal at the very start of an XML
// document (XML 1.0 §2.8) and never appears in an HTML document. HTML
// documents containing a `<?xml>`-looking processing instruction are a parse
// error and are not affected here because the prolog must be the first thing
// in the stream.
func looksLikeXML(s string) bool {
	s = strings.TrimPrefix(s, "\xef\xbb\xbf") // strip UTF-8 BOM (HTML5 §8.2.2.4)
	return strings.HasPrefix(s, "<?xml")
}

// stripXMLComments removes whole XML comments (`<!-- … -->`, delimiters and the
// content between them) from XML/XHTML <style> element text. An unterminated
// comment consumes to end-of-string, matching the XML well-formedness rule
// that an open comment must be closed (XML 1.0 §2.5).
func stripXMLComments(s string) string {
	const open, closeTag = "<!--", "-->"
	if !strings.Contains(s, open) {
		return s // common case: no comments, avoid the Builder allocation
	}
	var b strings.Builder
	for {
		before, rest, found := strings.Cut(s, open)
		b.WriteString(before)
		if !found {
			break
		}
		_, after, closed := strings.Cut(rest, closeTag)
		if !closed {
			// Unterminated comment: drop the remainder.
			break
		}
		s = after
	}
	return b.String()
}

// stripCDATA removes XHTML CDATA markers from style content.
// XHTML documents wrap CSS in <![CDATA[ ... ]]> sections.
func stripCDATA(s string) string {
	// CDATA delimiters may be surrounded by whitespace inside the
	// rawtext block; trim ONLY enough whitespace to find them, not the
	// trailing whitespace of the actual content (CSS Syntax §4.3.5
	// distinguishes a string clipped by newline `<bad-string-token>` from
	// one clipped by raw EOF `<string-token>`, so the trailing newline is
	// semantically significant — see variable-reference-032).
	leading := strings.TrimLeft(s, " \t\n\r\f")
	if strings.HasPrefix(leading, "<![CDATA[") {
		s = leading[len("<![CDATA["):]
	}
	trailing := strings.TrimRight(s, " \t\n\r\f")
	if strings.HasSuffix(trailing, "]]>") {
		s = trailing[:len(trailing)-len("]]>")]
	}
	return s
}

// insertElement appends the element node to parent, giving it the namespace
// namespaceForInsertedElement picks. Both start-tag insertion paths (the
// data-block <script> and the generic element path) use it, as Blink routes
// each start-tag element through HTMLConstructionSite::InsertHTMLElement or
// InsertForeignElement (core/html/parser/html_construction_site.cc:1059,
// :1127 @ d4ecdfed88f962439247c2ad36b8fe47805b1520). The synthesized
// html/head/body nodes and the reparenting moves bypass it and keep the
// unset (XHTML) namespace.
func insertElement(node, parent *Node) {
	if ns := namespaceForInsertedElement(node.TagName, parent); ns != XHTMLNamespaceURI {
		node.namespaceURI = ns
	}
	parent.AddChild(node)
}

// namespaceForInsertedElement returns the namespace of an element with
// start tag tag inserted under parent. Inside foreign content (an SVG or
// MathML parent that is not an integration point for this tag) the element
// takes the parent's namespace — Blink ProcessTokenInForeignContent's
// InsertForeignElement(token, current_namespace)
// (core/html/parser/html_tree_builder.cc:3109, :3128 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520). Otherwise the "in body" rules
// apply: <math> is MathML (:1198), <svg> is SVG (:1203), anything else is
// HTML (HTMLConstructionSite::InsertHTMLElement,
// html_construction_site.cc:1059).
//
// Not modelled (LOU-437 decision L7): the "breakout" that pops foreign
// content for HTML start tags such as <div> or <p> directly inside <svg>,
// and fragment (innerHTML) parsing context. Tag names arrive lowercased
// from the tokenizer, so SVG's foreignObject is "foreignobject".
func namespaceForInsertedElement(tag string, parent *Node) string {
	if parent != nil && parent.Type == ElementNode && parent.NamespaceURI() != XHTMLNamespaceURI &&
		shouldProcessTokenInForeignContent(tag, parent) {
		return parent.NamespaceURI()
	}
	switch tag {
	case "math":
		return MathMLNamespaceURI
	case "svg":
		return SVGNamespaceURI
	}
	return XHTMLNamespaceURI
}

// shouldProcessTokenInForeignContent reports whether a start tag under the
// foreign (non-HTML) element parent stays in foreign content. Ports the
// start-tag cases of Blink HTMLTreeBuilder::ShouldProcessTokenInForeignContent
// (core/html/parser/html_tree_builder.cc:2998-3027 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func shouldProcessTokenInForeignContent(tag string, parent *Node) bool {
	if isMathMLTextIntegrationPoint(parent) && tag != "mglyph" && tag != "malignmark" {
		return false
	}
	if parent.NamespaceURI() == MathMLNamespaceURI && parent.TagName == "annotation-xml" && tag == "svg" {
		return false
	}
	return !isHTMLIntegrationPoint(parent)
}

// isMathMLTextIntegrationPoint mirrors Blink
// HTMLElementStack::IsMathMLTextIntegrationPoint
// (core/html/parser/html_element_stack.cc:262-270 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func isMathMLTextIntegrationPoint(n *Node) bool {
	if n.NamespaceURI() != MathMLNamespaceURI {
		return false
	}
	switch n.TagName {
	case "mi", "mo", "mn", "ms", "mtext":
		return true
	}
	return false
}

// isHTMLIntegrationPoint mirrors Blink HTMLElementStack::IsHTMLIntegrationPoint
// (core/html/parser/html_element_stack.cc:273-290 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520): MathML annotation-xml with an
// HTML encoding, and SVG foreignObject / desc / title.
func isHTMLIntegrationPoint(n *Node) bool {
	switch n.NamespaceURI() {
	case MathMLNamespaceURI:
		if n.TagName != "annotation-xml" {
			return false
		}
		encoding, _ := n.GetAttribute("encoding")
		return strings.EqualFold(encoding, "text/html") || strings.EqualFold(encoding, "application/xhtml+xml")
	case SVGNamespaceURI:
		switch n.TagName {
		case "foreignobject", "desc", "title":
			return true
		}
	}
	return false
}
