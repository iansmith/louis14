package html

import (
	"fmt"
	gohtml "html"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenStartTag TokenType = iota
	TokenEndTag
	TokenText
	TokenComment
	TokenEOF
)

type Token struct {
	Type        TokenType
	TagName     string
	Attributes  map[string]string
	Text        string
	RawText     string // Original text with whitespace preserved (for <pre> contexts)
	SelfClosing bool   // True for tags ending with /> (XHTML self-closing syntax)
}

type Tokenizer struct {
	input string
	pos   int
}

func NewTokenizer(html string) *Tokenizer {
	// Strip UTF-8 BOM (U+FEFF) if present at start of document (HTML5 §8.2.2.4)
	html = strings.TrimPrefix(html, "\xef\xbb\xbf")
	return &Tokenizer{input: html, pos: 0}
}

func (t *Tokenizer) NextToken() (Token, error) {
	if t.pos >= len(t.input) {
		return Token{Type: TokenEOF}, nil
	}
	// Only skip whitespace before tags, not before text content.
	// Whitespace before text is significant for inline flow
	// (e.g., the space in "</em> word" must be preserved).
	if t.input[t.pos] == '<' {
		return t.readTag()
	}
	return t.readText()
}

func (t *Tokenizer) readTag() (Token, error) {
	t.pos++

	// Handle <!-- comments -->
	if t.pos+2 < len(t.input) && t.input[t.pos] == '!' && t.input[t.pos+1] == '-' && t.input[t.pos+2] == '-' {
		t.pos += 3
		for t.pos+2 < len(t.input) {
			if t.input[t.pos] == '-' && t.input[t.pos+1] == '-' && t.input[t.pos+2] == '>' {
				t.pos += 3
				return Token{Type: TokenComment}, nil
			}
			t.pos++
		}
		t.pos = len(t.input)
		return Token{Type: TokenComment}, nil
	}

	// Handle <?xml ...?> and other processing instructions
	if t.pos < len(t.input) && t.input[t.pos] == '?' {
		// Skip to closing ?>
		for t.pos+1 < len(t.input) {
			if t.input[t.pos] == '?' && t.input[t.pos+1] == '>' {
				t.pos += 2
				return t.NextToken()
			}
			t.pos++
		}
		t.pos = len(t.input)
		return t.NextToken()
	}

	// Handle <!DOCTYPE ...> — DOCTYPE content is informational and dropped.
	// HTML5 §13.2.5.53 "DOCTYPE state" / §13.2.5.61 "After DOCTYPE name state":
	// EOF is a parse error but parsing continues. We don't surface DOCTYPE
	// content to the parser, so on EOF we just consume what we have and
	// emit no token; NextToken() will return TokenEOF on its next call.
	if t.pos < len(t.input) && t.input[t.pos] == '!' {
		if err := t.skipTo('>'); err != nil {
			return Token{Type: TokenEOF}, nil
		}
		t.pos++
		return t.NextToken()
	}

	isEndTag := false
	if t.pos < len(t.input) && t.input[t.pos] == '/' {
		isEndTag = true
		t.pos++
	}
	tagName := t.readTagName()
	if tagName == "" {
		return Token{}, fmt.Errorf("expected tag name at position %d", t.pos)
	}
	if isEndTag {
		// HTML5 §13.2.5.8 "Tag name state": EOF in tag is a parse error;
		// the spec emits an EOF token (dropping the partial end tag) and
		// the tree-builder closes any open elements at EOF anyway. We
		// emit the end tag with the name we have — equivalent visual
		// result and slightly more permissive against truncated input
		// like `</html` (no closing '>') that real browsers also accept.
		if err := t.skipTo('>'); err != nil {
			return Token{Type: TokenEndTag, TagName: tagName}, nil
		}
		t.pos++
		return Token{Type: TokenEndTag, TagName: tagName}, nil
	}
	attributes := make(map[string]string)
	for {
		t.skipWhitespace()
		if t.pos >= len(t.input) {
			// HTML5 §13.2.5.32 "Before attribute name state" / §13.2.5.34
			// "Attribute name state": EOF in tag is a parse error. Spec
			// drops the partial start tag; we emit it with attributes
			// parsed so far so subsequent content (if any) doesn't lose
			// structural context. Matches the tree-builder's EOF closure
			// behavior in effect.
			return Token{Type: TokenStartTag, TagName: tagName, Attributes: attributes}, nil
		}
		if t.input[t.pos] == '>' {
			t.pos++
			break
		}
		// HTML5 error recovery: if we hit '<' while reading attributes,
		// the current tag's closing '>' was omitted. Implicitly close
		// the tag without consuming the '<', so the next NextToken()
		// call will parse the new tag that starts here.
		if t.input[t.pos] == '<' {
			break
		}
		if t.input[t.pos] == '/' {
			t.pos++
			t.skipWhitespace()
			if t.pos < len(t.input) && t.input[t.pos] == '>' {
				t.pos++
				return Token{Type: TokenStartTag, TagName: tagName, Attributes: attributes, SelfClosing: true}, nil
			}
		}
		name, value, err := t.readAttribute()
		if err != nil {
			return Token{}, err
		}
		attributes[name] = value
	}
	return Token{Type: TokenStartTag, TagName: tagName, Attributes: attributes}, nil
}

func (t *Tokenizer) readTagName() string {
	start := t.pos
	for t.pos < len(t.input) && isTagNameChar(t.input[t.pos]) {
		t.pos++
	}
	return strings.ToLower(t.input[start:t.pos])
}

func (t *Tokenizer) readAttribute() (string, string, error) {
	start := t.pos
	for t.pos < len(t.input) && isAttributeNameChar(t.input[t.pos]) {
		t.pos++
	}
	name := strings.ToLower(t.input[start:t.pos])
	if name == "" {
		return "", "", fmt.Errorf("expected attribute name at position %d", t.pos)
	}
	t.skipWhitespace()
	if t.pos >= len(t.input) || t.input[t.pos] != '=' {
		return name, "", nil
	}
	t.pos++
	t.skipWhitespace()
	value, err := t.readAttributeValue()
	if err != nil {
		return "", "", err
	}
	return name, value, nil
}

func (t *Tokenizer) readAttributeValue() (string, error) {
	if t.pos >= len(t.input) {
		return "", fmt.Errorf("expected attribute value at position %d", t.pos)
	}
	quote := t.input[t.pos]
	if quote == '"' || quote == '\'' {
		t.pos++
		start := t.pos
		for t.pos < len(t.input) && t.input[t.pos] != quote {
			t.pos++
		}
		if t.pos >= len(t.input) {
			// HTML5 §13.2.5.38/.39 "Attribute value (double-/single-quoted)
			// state": EOF in tag is a parse error. Recover by emitting the
			// attribute value built so far; the enclosing readTag loop will
			// then hit its own EOF branch and emit the partial start tag.
			return t.input[start:t.pos], nil
		}
		value := t.input[start:t.pos]
		t.pos++
		return value, nil
	}
	start := t.pos
	for t.pos < len(t.input) && !unicode.IsSpace(rune(t.input[t.pos])) && t.input[t.pos] != '>' {
		t.pos++
	}
	return t.input[start:t.pos], nil
}

func (t *Tokenizer) readText() (Token, error) {
	start := t.pos
	for t.pos < len(t.input) && t.input[t.pos] != '<' {
		t.pos++
	}
	raw := t.input[start:t.pos]
	// Return both normalized and raw text. The parser decides which to use
	// based on context (e.g., <pre> elements need raw whitespace preserved).
	text := normalizeWhitespace(raw)
	if text == "" {
		// All-whitespace that normalized to empty — check if raw is also empty
		rawUnescaped := gohtml.UnescapeString(raw)
		if strings.TrimSpace(rawUnescaped) == "" && rawUnescaped != "" {
			// Whitespace-only: return with RawText for <pre> contexts
			return Token{Type: TokenText, Text: " ", RawText: rawUnescaped}, nil
		}
		if t.pos < len(t.input) {
			return t.NextToken()
		}
		return Token{Type: TokenEOF}, nil
	}
	text = gohtml.UnescapeString(text)
	rawText := gohtml.UnescapeString(raw)
	return Token{Type: TokenText, Text: text, RawText: rawText}, nil
}

// normalizeWhitespace collapses runs of whitespace to a single space,
// preserving a single space at boundaries. This is important for inline
// flow: "text <em>word</em> more" must keep the spaces between the text
// nodes and the inline element.
func normalizeWhitespace(s string) string {
	hasLeading := len(s) > 0 && unicode.IsSpace(rune(s[0]))
	hasTrailing := len(s) > 0 && unicode.IsSpace(rune(s[len(s)-1]))

	fields := strings.Fields(s)
	if len(fields) == 0 {
		// All-whitespace token: keep as single space so inline flow
		// preserves word boundaries (e.g., between two inline elements).
		if hasLeading || hasTrailing {
			return " "
		}
		return ""
	}

	result := strings.Join(fields, " ")
	if hasLeading {
		result = " " + result
	}
	if hasTrailing {
		result = result + " "
	}
	return result
}

func (t *Tokenizer) skipWhitespace() {
	for t.pos < len(t.input) && unicode.IsSpace(rune(t.input[t.pos])) {
		t.pos++
	}
}

func (t *Tokenizer) skipTo(target byte) error {
	for t.pos < len(t.input) && t.input[t.pos] != target {
		t.pos++
	}
	if t.pos >= len(t.input) {
		return fmt.Errorf("expected '%c' but reached EOF", target)
	}
	return nil
}

// ReadRawUntil reads raw content until the closing end tag is found (e.g., </script>).
// This is used for raw text elements like <script> and <style> where '<' does not
// start a new tag.
func (t *Tokenizer) ReadRawUntil(endTag string) string {
	needle := "</" + endTag + ">"
	needleLower := strings.ToLower(needle)
	start := t.pos
	for t.pos+len(needle) <= len(t.input) {
		// Case-insensitive match for the end tag
		if strings.ToLower(t.input[t.pos:t.pos+len(needle)]) == needleLower {
			content := t.input[start:t.pos]
			t.pos += len(needle) // skip past </endTag>
			return content
		}
		t.pos++
	}
	// No closing tag found — consume everything remaining
	content := t.input[start:]
	t.pos = len(t.input)
	return content
}

func isTagNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

func isAttributeNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':' || c == '.'
}
