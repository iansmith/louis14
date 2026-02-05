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
						p.doc.Stylesheets = append(p.doc.Stylesheets, content)
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

			// Create new element node
			node := &Node{
				Type:       ElementNode,
				TagName:    token.TagName,
				Attributes: token.Attributes,
				Children:   make([]*Node, 0),
			}

			// Add to current parent (top of stack)
			parent := p.currentParent()
			parent.AddChild(node)

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
			if !p.isSelfClosing(token.TagName) {
				// Push onto stack to become new parent
				p.push(node)
			}

		case TokenText:
			// Add text to current parent
			if token.Text != "" {
				parent := p.currentParent()
				parent.AppendText(token.Text)
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

// closeTag pops the stack until the matching tag is found and closed
func (p *Parser) closeTag(tagName string) {
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].TagName == tagName {
			p.stack = p.stack[:i]
			return
		}
	}
	// Tag not found on stack; ignore the end tag
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
		return decoded
	}
	// Try the CSS fetcher for network URLs
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
// stylesheets referenced by <link rel="stylesheet"> tags.
func ParseWithFetcher(htmlContent string, cssFetcher CSSFetcher) (*Document, error) {
	parser := NewParser(htmlContent)
	parser.cssFetcher = cssFetcher
	return parser.Parse()
}

// ParseFragment parses an HTML fragment string and returns detached child nodes.
// Unlike Parse, <script> and <style> tags become DOM nodes instead of being
// extracted into Document.Scripts/Stylesheets.
func ParseFragment(htmlContent string) ([]*Node, error) {
	parser := NewParser(htmlContent)
	parser.fragmentMode = true
	doc, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	// Detach children from the synthetic root
	children := make([]*Node, len(doc.Root.Children))
	copy(children, doc.Root.Children)
	for _, child := range children {
		child.Parent = nil
	}
	doc.Root.Children = nil
	return children, nil
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
