package css

import (
	"fmt"
	"strings"
	"unicode"
)

// Phase 3: CSS stylesheet tokenizer (not just inline styles)

type CSSTokenType int

const (
	CSSTokenSelector CSSTokenType = iota
	CSSTokenLBrace                // {
	CSSTokenRBrace                // }
	CSSTokenProperty
	CSSTokenColon // :
	CSSTokenValue
	CSSTokenSemicolon // ;
	CSSTokenEOF
)

type CSSToken struct {
	Type  CSSTokenType
	Value string
}

type CSSTokenizer struct {
	input string
	pos   int
}

func NewCSSTokenizer(input string) *CSSTokenizer {
	return &CSSTokenizer{
		input: input,
		pos:   0,
	}
}

func (t *CSSTokenizer) NextToken() (CSSToken, error) {
	// Skip whitespace
	t.skipWhitespace()

	if t.pos >= len(t.input) {
		return CSSToken{Type: CSSTokenEOF}, nil
	}

	ch := t.input[t.pos]

	switch ch {
	case '{':
		t.pos++
		return CSSToken{Type: CSSTokenLBrace, Value: "{"}, nil
	case '}':
		t.pos++
		return CSSToken{Type: CSSTokenRBrace, Value: "}"}, nil
	case ':':
		t.pos++
		return CSSToken{Type: CSSTokenColon, Value: ":"}, nil
	case ';':
		t.pos++
		return CSSToken{Type: CSSTokenSemicolon, Value: ";"}, nil
	default:
		// Try to determine if this is a selector, property, or value
		// based on context
		return t.readIdentifier()
	}
}

func (t *CSSTokenizer) readIdentifier() (CSSToken, error) {
	start := t.pos

	// Read until we hit whitespace or special char
	for t.pos < len(t.input) {
		ch := t.input[t.pos]
		if ch == '{' || ch == '}' || ch == ':' || ch == ';' {
			break
		}
		// Check for comment start
		if ch == '/' && t.pos+1 < len(t.input) && t.input[t.pos+1] == '*' {
			break
		}
		if unicode.IsSpace(rune(ch)) {
			// Check if there's more content (for multi-word values)
			// Peek ahead to see if we're reading a value
			if t.isReadingValue() {
				t.pos++
				continue
			}
			break
		}
		t.pos++
	}

	value := strings.TrimSpace(t.input[start:t.pos])
	if value == "" {
		return CSSToken{Type: CSSTokenEOF}, nil
	}

	// The actual type will be determined by the parser based on position
	// For now, return as selector (parser will interpret correctly)
	return CSSToken{Type: CSSTokenSelector, Value: value}, nil
}

func (t *CSSTokenizer) isReadingValue() bool {
	// Look back to see if we just passed a colon (means we're reading a value)
	for i := t.pos - 1; i >= 0; i-- {
		ch := t.input[i]
		if ch == ':' {
			return true
		}
		if ch == ';' || ch == '{' || ch == '}' {
			return false
		}
	}
	return false
}

func (t *CSSTokenizer) skipWhitespace() {
	for t.pos < len(t.input) {
		if unicode.IsSpace(rune(t.input[t.pos])) {
			t.pos++
		} else if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '*' {
			t.skipComment()
		} else {
			break
		}
	}
}

// skipComment skips a /* ... */ comment. Assumes pos is at the '/'.
func (t *CSSTokenizer) skipComment() {
	t.pos += 2 // skip /*
	for t.pos+1 < len(t.input) {
		if t.input[t.pos] == '*' && t.input[t.pos+1] == '/' {
			t.pos += 2
			return
		}
		t.pos++
	}
	// Unterminated comment: skip to end
	t.pos = len(t.input)
}

// Peek returns the next character without advancing
func (t *CSSTokenizer) peek() byte {
	if t.pos >= len(t.input) {
		return 0
	}
	return t.input[t.pos]
}

// Consume advances the position by n characters
func (t *CSSTokenizer) consume(n int) {
	t.pos += n
	if t.pos > len(t.input) {
		t.pos = len(t.input)
	}
}

// Error returns a formatted error with position information
func (t *CSSTokenizer) Error(msg string) error {
	return fmt.Errorf("CSS tokenizer error at position %d: %s", t.pos, msg)
}
