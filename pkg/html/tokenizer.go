package html

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenStartTag TokenType = iota
	TokenEndTag
	TokenText
	TokenEOF
)

type Token struct {
	Type       TokenType
	TagName    string
	Attributes map[string]string
	Text       string
}

type Tokenizer struct {
	input string
	pos   int
}

func NewTokenizer(html string) *Tokenizer {
	return &Tokenizer{input: html, pos: 0}
}

func (t *Tokenizer) NextToken() (Token, error) {
	t.skipWhitespace()
	if t.pos >= len(t.input) {
		return Token{Type: TokenEOF}, nil
	}
	if t.input[t.pos] == '<' {
		return t.readTag()
	}
	return t.readText()
}

func (t *Tokenizer) readTag() (Token, error) {
	t.pos++
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
		if err := t.skipTo('>'); err != nil {
			return Token{}, err
		}
		t.pos++
		return Token{Type: TokenEndTag, TagName: tagName}, nil
	}
	attributes := make(map[string]string)
	for {
		t.skipWhitespace()
		if t.pos >= len(t.input) {
			return Token{}, fmt.Errorf("unexpected EOF in tag")
		}
		if t.input[t.pos] == '>' {
			t.pos++
			break
		}
		if t.input[t.pos] == '/' {
			t.pos++
			t.skipWhitespace()
			if t.pos < len(t.input) && t.input[t.pos] == '>' {
				t.pos++
				break
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
			return "", fmt.Errorf("unterminated attribute value")
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
	text := strings.TrimSpace(t.input[start:t.pos])
	if text == "" && t.pos < len(t.input) {
		return t.NextToken()
	}
	return Token{Type: TokenText, Text: text}, nil
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

func isTagNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

func isAttributeNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':'
}
