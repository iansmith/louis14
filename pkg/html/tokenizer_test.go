package html

import "testing"

func TestTokenizer_SimpleStartTag(t *testing.T) {
	tokenizer := NewTokenizer("<div>")
	token, err := tokenizer.NextToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Type != TokenStartTag {
		t.Errorf("expected TokenStartTag, got %v", token.Type)
	}
	if token.TagName != "div" {
		t.Errorf("expected tag name 'div', got '%s'", token.TagName)
	}
}

func TestTokenizer_TagWithAttributes(t *testing.T) {
	tokenizer := NewTokenizer(`<div style="color: red" id="main">`)
	token, err := tokenizer.NextToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Attributes["style"] != "color: red" {
		t.Errorf("expected style='color: red', got '%s'", token.Attributes["style"])
	}
	if token.Attributes["id"] != "main" {
		t.Errorf("expected id='main', got '%s'", token.Attributes["id"])
	}
}

func TestTokenizer_CompleteSequence(t *testing.T) {
	tokenizer := NewTokenizer("<div>Hello</div>")
	token1, _ := tokenizer.NextToken()
	if token1.Type != TokenStartTag || token1.TagName != "div" {
		t.Error("expected start tag 'div'")
	}
	token2, _ := tokenizer.NextToken()
	if token2.Type != TokenText || token2.Text != "Hello" {
		t.Error("expected text 'Hello'")
	}
	token3, _ := tokenizer.NextToken()
	if token3.Type != TokenEndTag {
		t.Error("expected end tag")
	}
	token4, _ := tokenizer.NextToken()
	if token4.Type != TokenEOF {
		t.Error("expected EOF")
	}
}
