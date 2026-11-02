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

func TestTokenizer_AttributeValueDecodesCharacterReferences(t *testing.T) {
	// HTML5 §13.2.5.38/.39 requires character references inside attribute
	// values to be consumed and replaced with their character. Without
	// this, an inline-style data URL written with `&quot;` in the source
	// survives into the CSS parser and breaks the URL.
	cases := []struct {
		name string
		in   string
		attr string
		want string
	}{
		{
			"named entity in double-quoted value",
			`<span style="filter: url(&quot;data:image/svg+xml;utf8,X&quot;);">`,
			"style",
			`filter: url("data:image/svg+xml;utf8,X");`,
		},
		{
			"amp + lt + gt",
			`<a title="a &amp; b &lt;c&gt;">`,
			"title",
			`a & b <c>`,
		},
		{
			"numeric entity (decimal + hex)",
			`<a title="&#34;&#x22;">`,
			"title",
			`""`,
		},
		{
			"single-quoted value with &apos;",
			`<a title='it&apos;s'>`,
			"title",
			`it's`,
		},
		{
			"unquoted value with &amp;",
			`<a href=foo&amp;bar>`,
			"href",
			`foo&bar`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := NewTokenizer(tc.in).NextToken()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := token.Attributes[tc.attr]; got != tc.want {
				t.Errorf("%s attr:\n  got  = %q\n  want = %q", tc.attr, got, tc.want)
			}
		})
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

// In the attribute name state a `"` is a parse error and joins the
// attribute name; tokenizing continues (HTML §13.2.5.33 attribute name
// state; HTMLTokenizer kAttributeNameState @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) (LOU-597).
func TestTokenizer_QuoteInAttributeNameRecovers(t *testing.T) {
	doc, err := Parse(`<link rel="assert" content"sibling-index() is 2 for body"><p id="after">hi</p>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	var found bool
	var walk func(n *Node)
	walk = func(n *Node) {
		if id, _ := n.GetAttribute("id"); id == "after" {
			found = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	if !found {
		t.Error("content after the malformed attribute was lost")
	}
}
