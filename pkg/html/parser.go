package html

import (
	"fmt"
	"net/url"
	"strings"
)

// CSSFetcher is a function that fetches CSS content from a URI.
// Used to support network-based stylesheet loading.
type CSSFetcher func(uri string) (string, error)

type Parser struct {
	tokenizer       *Tokenizer
	doc             *Document
	stack           []*Node // Phase 2: Stack for tracking nested elements
	cssFetcher      CSSFetcher // Optional fetcher for external stylesheets
	fragmentMode    bool       // When true, <script>/<style> become DOM nodes
}

func NewParser(html string) *Parser {
	return &Parser{
		tokenizer: NewTokenizer(html),
		doc:       NewDocument(),
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
			// Special handling for <style>/<script> tags in normal mode:
			// extract raw content. In fragment mode, treat them as DOM nodes.
			if !p.fragmentMode {
				if token.TagName == "style" {
					content := stripCDATA(p.tokenizer.ReadRawUntil("style"))
					if strings.TrimSpace(content) != "" {
						p.doc.Stylesheets = append(p.doc.Stylesheets, p.resolveImports(content))
					}
					continue
				}
				if token.TagName == "script" {
					content := p.tokenizer.ReadRawUntil("script")
					if strings.TrimSpace(content) != "" {
						p.doc.Scripts = append(p.doc.Scripts, content)
					}
					continue
				}
			}

			// Auto-close <p> when a block-level element is encountered inside it
			if p.isBlockElement(token.TagName) {
				p.autoCloseP()
			}

			// HTML5 table parsing: auto-close table structural elements.
			// colgroup is implicitly closed by tbody/tfoot/thead/tr/td/th.
			// tbody/thead/tfoot are implicitly closed by each other.
			p.autoCloseTableElements(token.TagName)

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

			parent.AddChild(node)

			// Handle <meta name="viewport" content="width=...">
			if token.TagName == "meta" {
				if name, ok := token.Attributes["name"]; ok && strings.EqualFold(name, "viewport") {
					if content, ok := token.Attributes["content"]; ok {
						p.doc.ViewportWidth = parseViewportWidth(content)
					}
				}
			}

			// Handle <link rel="stylesheet"> with data URI href
			if token.TagName == "link" {
				if rel, ok := token.Attributes["rel"]; ok {
					if strings.Contains(rel, "stylesheet") {
						if href, ok := token.Attributes["href"]; ok {
							if css := p.loadLinkStylesheet(href); css != "" {
								p.doc.Stylesheets = append(p.doc.Stylesheets, css)
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

		case TokenText:
			// Add text to current parent
			// Use raw text (preserving whitespace) inside preformatted elements
			parent := p.currentParent()
			text := token.Text
			rawText := token.RawText
			if rawText != "" && p.isInsidePreformatted() {
				text = rawText
				// HTML spec: strip a single leading newline in <pre> elements.
				// This applies to the first text node immediately after the <pre> tag.
				if parent.TagName == "pre" && len(parent.Children) == 0 && len(text) > 0 && text[0] == '\n' {
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
			// Pop stack until we find the matching tag
			p.closeTag(token.TagName)
		}
	}

	return p.doc, nil
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
	case "meta", "title", "link", "base", "noscript":
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
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].TagName == tagName {
			p.stack = p.stack[:i]
			return
		}
	}
	// Tag not found on stack; ignore the end tag
}

// autoCloseTableElements implements HTML5 table implicit closing rules.
// Key rules per HTML5 §12.2.6.4.9-12:
//   - <colgroup> is implicitly closed when <tbody>, <tfoot>, <thead>, <tr>, <td>, <th> is seen
//   - <tbody>/<thead>/<tfoot> are implicitly closed when another section element starts
func (p *Parser) autoCloseTableElements(tagName string) {
	parent := p.currentParent()
	switch tagName {
	case "tbody", "tfoot", "thead":
		// Close <colgroup> if it is the current parent
		if parent.TagName == "colgroup" {
			p.pop()
		}
		// Close any open tbody/thead/tfoot so they become siblings, not nested
		parent = p.currentParent()
		if parent.TagName == "tbody" || parent.TagName == "thead" || parent.TagName == "tfoot" {
			p.pop()
		}
	case "tr":
		// Close <colgroup> if it is the current parent
		if parent.TagName == "colgroup" {
			p.pop()
		}
	case "td", "th":
		// Close any open <tr> sibling (handled naturally by close tag)
		// No action needed here: td/th go inside the current tr
	}
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
func (p *Parser) loadLinkStylesheet(href string) string {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "data:text/css,") {
		encoded := href[len("data:text/css,"):]
		decoded, err := url.PathUnescape(encoded)
		if err != nil {
			return encoded
		}
		return p.resolveImports(decoded)
	}
	// Try the CSS fetcher for network URLs
	if p.cssFetcher != nil {
		if css, err := p.cssFetcher(href); err == nil {
			return p.resolveImports(css)
		}
	}
	return ""
}

// resolveImports processes @import rules in CSS text by fetching imported
// stylesheets and prepending their content. Per CSS spec, @import rules must
// appear before any other rules (except @charset).
func (p *Parser) resolveImports(cssText string) string {
	if p.cssFetcher == nil {
		return cssText
	}
	if !strings.Contains(cssText, "@import") {
		return cssText
	}

	var imported strings.Builder
	var remaining strings.Builder
	pastImports := false

	lines := strings.Split(cssText, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "/*") {
			if pastImports {
				remaining.WriteString(line)
				remaining.WriteByte('\n')
			} else {
				imported.WriteString(line)
				imported.WriteByte('\n')
			}
			continue
		}

		if !pastImports && strings.HasPrefix(trimmed, "@import") {
			// Parse the @import URL
			if importURL := parseImportURL(trimmed); importURL != "" {
				if css, err := p.cssFetcher(importURL); err == nil {
					// Recursively resolve imports in the fetched CSS (max depth handled by fetcher timeouts)
					resolved := p.resolveImports(css)
					imported.WriteString(resolved)
					imported.WriteByte('\n')
				}
			}
			continue
		}

		// Any non-import rule means we're past the import section
		pastImports = true
		remaining.WriteString(line)
		remaining.WriteByte('\n')
	}

	return imported.String() + remaining.String()
}

// parseImportURL extracts the URL from an @import rule.
// Supports: @import url("foo.css"); @import url(foo.css); @import "foo.css";
func parseImportURL(rule string) string {
	// Remove trailing semicolon and whitespace
	rule = strings.TrimSpace(rule)
	rule = strings.TrimSuffix(rule, ";")
	rule = strings.TrimSpace(rule)

	// Remove @import prefix
	rule = strings.TrimPrefix(rule, "@import")
	rule = strings.TrimSpace(rule)

	// Remove optional media queries (everything after the URL)
	// For now, we import unconditionally

	// Handle url(...) syntax
	if strings.HasPrefix(rule, "url(") {
		closeIdx := strings.Index(rule, ")")
		if closeIdx == -1 {
			return ""
		}
		inner := rule[4:closeIdx]
		inner = strings.TrimSpace(inner)
		// Remove quotes
		if len(inner) >= 2 {
			if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
				(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
				inner = inner[1 : len(inner)-1]
			}
		}
		return inner
	}

	// Handle bare string syntax: @import "foo.css" or @import 'foo.css'
	if len(rule) >= 2 {
		if (rule[0] == '"' && rule[len(rule)-1] == '"') ||
			(rule[0] == '\'' && rule[len(rule)-1] == '\'') {
			return rule[1 : len(rule)-1]
		}
		// May have media query after the string: @import "foo.css" screen;
		if rule[0] == '"' {
			endQuote := strings.Index(rule[1:], "\"")
			if endQuote >= 0 {
				return rule[1 : endQuote+1]
			}
		}
		if rule[0] == '\'' {
			endQuote := strings.Index(rule[1:], "'")
			if endQuote >= 0 {
				return rule[1 : endQuote+1]
			}
		}
	}

	return ""
}

func Parse(html string) (*Document, error) {
	parser := NewParser(html)
	return parser.Parse()
}

// ParseWithFetcher parses HTML and uses the provided fetcher to load external
// stylesheets referenced by <link rel="stylesheet"> tags.
func ParseWithFetcher(htmlContent string, cssFetcher CSSFetcher) (*Document, error) {
	parser := NewParser(htmlContent)
	parser.cssFetcher = cssFetcher
	return parser.Parse()
}

// ParseFragment parses an HTML fragment string and returns detached child nodes.
// Unlike Parse, <script> and <style> tags become DOM nodes instead of being
// extracted into Document.Scripts/Stylesheets.
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

// stripCDATA removes XHTML CDATA markers from style content.
// XHTML documents wrap CSS in <![CDATA[ ... ]]> sections.
func stripCDATA(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<![CDATA[") {
		s = s[len("<![CDATA["):]
	}
	if strings.HasSuffix(s, "]]>") {
		s = s[:len(s)-len("]]>")]
	}
	return s
}
