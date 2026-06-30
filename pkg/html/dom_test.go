package html

import "testing"

func makeTree() *Node {
	// <div id="parent"><span>hello</span><p>world</p></div>
	parent := &Node{
		Type:       ElementNode,
		TagName:    "div",
		Attributes: map[string]string{"id": "parent"},
		Children:   make([]*Node, 0),
	}
	span := &Node{Type: ElementNode, TagName: "span", Children: make([]*Node, 0)}
	span.AppendText("hello")
	parent.AddChild(span)

	p := &Node{Type: ElementNode, TagName: "p", Children: make([]*Node, 0)}
	p.AppendText("world")
	parent.AddChild(p)

	return parent
}

func TestRemoveChild(t *testing.T) {
	parent := makeTree()
	span := parent.Children[0]
	removed := parent.RemoveChild(span)
	if removed != span {
		t.Fatal("RemoveChild should return the removed child")
	}
	if span.Parent != nil {
		t.Error("removed child should have nil parent")
	}
	if len(parent.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(parent.Children))
	}
	if parent.Children[0].TagName != "p" {
		t.Error("remaining child should be <p>")
	}
}

func TestRemoveChildNotFound(t *testing.T) {
	parent := makeTree()
	other := &Node{Type: ElementNode, TagName: "em"}
	result := parent.RemoveChild(other)
	if result != nil {
		t.Error("RemoveChild of non-child should return nil")
	}
}

func TestInsertBefore(t *testing.T) {
	parent := makeTree()
	em := &Node{Type: ElementNode, TagName: "em", Children: make([]*Node, 0)}
	p := parent.Children[1] // <p>
	parent.InsertBefore(em, p)
	if len(parent.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(parent.Children))
	}
	if parent.Children[1] != em {
		t.Error("em should be at index 1")
	}
	if em.Parent != parent {
		t.Error("em.Parent should be parent")
	}
}

func TestInsertBeforeNilRef(t *testing.T) {
	parent := makeTree()
	em := &Node{Type: ElementNode, TagName: "em", Children: make([]*Node, 0)}
	parent.InsertBefore(em, nil)
	if parent.Children[len(parent.Children)-1] != em {
		t.Error("InsertBefore(nil) should append")
	}
}

func TestInsertBeforeReparent(t *testing.T) {
	parent := makeTree()
	span := parent.Children[0]
	// Insert span before <p> — should move span from index 0 to index 0 (before p, which is now at 0)
	p := parent.Children[1]
	parent.InsertBefore(span, p)
	if len(parent.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(parent.Children))
	}
	if parent.Children[0] != span {
		t.Error("span should remain at index 0")
	}
}

func TestCloneNodeShallow(t *testing.T) {
	parent := makeTree()
	clone := parent.CloneNode(false)
	if clone.TagName != "div" {
		t.Error("clone should have same tagName")
	}
	if clone.Parent != nil {
		t.Error("clone should have nil parent")
	}
	if len(clone.Children) != 0 {
		t.Errorf("shallow clone should have 0 children, got %d", len(clone.Children))
	}
	if clone.Attributes["id"] != "parent" {
		t.Error("clone should copy attributes")
	}
	// Verify independence
	clone.Attributes["id"] = "clone"
	if parent.Attributes["id"] != "parent" {
		t.Error("modifying clone should not affect original")
	}
}

func TestCloneNodeDeep(t *testing.T) {
	parent := makeTree()
	clone := parent.CloneNode(true)
	if len(clone.Children) != 2 {
		t.Fatalf("deep clone should have 2 children, got %d", len(clone.Children))
	}
	if clone.Children[0].TagName != "span" {
		t.Error("first child should be span")
	}
	if clone.Children[0].Parent != clone {
		t.Error("cloned children should point to clone as parent")
	}
	// Verify independence
	if clone.Children[0] == parent.Children[0] {
		t.Error("deep clone children should be different pointers")
	}
}

func TestContains(t *testing.T) {
	parent := makeTree()
	span := parent.Children[0]
	textNode := span.Children[0]

	if !parent.Contains(parent) {
		t.Error("node should contain itself")
	}
	if !parent.Contains(span) {
		t.Error("parent should contain child")
	}
	if !parent.Contains(textNode) {
		t.Error("parent should contain grandchild")
	}
	other := &Node{Type: ElementNode, TagName: "em"}
	if parent.Contains(other) {
		t.Error("parent should not contain unrelated node")
	}
}

func TestIndexInParent(t *testing.T) {
	parent := makeTree()
	if parent.IndexInParent() != -1 {
		t.Error("root node should have index -1")
	}
	if parent.Children[0].IndexInParent() != 0 {
		t.Error("first child should be at index 0")
	}
	if parent.Children[1].IndexInParent() != 1 {
		t.Error("second child should be at index 1")
	}
}

func TestSerialize(t *testing.T) {
	parent := makeTree()
	got := parent.Serialize()
	want := "<span>hello</span><p>world</p>"
	if got != want {
		t.Errorf("Serialize() = %q, want %q", got, want)
	}
}

func TestSerializeOuter(t *testing.T) {
	parent := makeTree()
	got := parent.SerializeOuter()
	want := `<div id="parent"><span>hello</span><p>world</p></div>`
	if got != want {
		t.Errorf("SerializeOuter() = %q, want %q", got, want)
	}
}

func TestSerializeVoidElement(t *testing.T) {
	n := &Node{
		Type:     ElementNode,
		TagName:  "div",
		Children: make([]*Node, 0),
	}
	br := &Node{Type: ElementNode, TagName: "br", Children: make([]*Node, 0)}
	n.AddChild(br)
	got := n.Serialize()
	want := "<br>"
	if got != want {
		t.Errorf("Serialize() = %q, want %q", got, want)
	}
}

func TestSerializeEscaping(t *testing.T) {
	n := &Node{
		Type:     ElementNode,
		TagName:  "p",
		Children: make([]*Node, 0),
	}
	n.AppendText(`<b>"hello" & 'world'</b>`)
	got := n.Serialize()
	want := `&lt;b&gt;"hello" &amp; 'world'&lt;/b&gt;`
	if got != want {
		t.Errorf("Serialize() = %q, want %q", got, want)
	}
}

func TestSerializeAttributes(t *testing.T) {
	n := &Node{
		Type:       ElementNode,
		TagName:    "a",
		Attributes: map[string]string{"href": "/test", "class": "link"},
		Children:   make([]*Node, 0),
	}
	n.AppendText("click")
	got := n.SerializeOuter()
	// Attributes sorted alphabetically
	want := `<a class="link" href="/test">click</a>`
	if got != want {
		t.Errorf("SerializeOuter() = %q, want %q", got, want)
	}
}

func TestIsReplacedElementTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		// Replaced (CSS Display 3 §2.2 atomic inlines regardless of computed display).
		{"img", true},
		{"video", true},
		{"canvas", true},
		{"svg", true},
		{"iframe", true},
		{"embed", true},
		{"object", true},
		{"input", true},
		{"textarea", true},
		{"select", true},
		{"button", true},
		// Non-replaced layout-controlled elements.
		{"div", false},
		{"span", false},
		{"p", false},
		{"a", false},
		{"table", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsReplacedElementTag(c.tag); got != c.want {
			t.Errorf("IsReplacedElementTag(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
}

func TestInheritedLanguage(t *testing.T) {
	// <html lang="ja"><body><div><span>x</span></div></body></html>
	// span has no lang; should inherit "ja" from html.
	htmlNode := &Node{Type: ElementNode, TagName: "html", Attributes: map[string]string{"lang": "ja"}}
	body := &Node{Type: ElementNode, TagName: "body"}
	htmlNode.AddChild(body)
	div := &Node{Type: ElementNode, TagName: "div"}
	body.AddChild(div)
	span := &Node{Type: ElementNode, TagName: "span"}
	div.AddChild(span)
	span.AppendText("x")
	textNode := span.Children[0]

	if got := span.InheritedLanguage(); got != "ja" {
		t.Errorf("span.InheritedLanguage() = %q, want %q", got, "ja")
	}
	if got := textNode.InheritedLanguage(); got != "ja" {
		t.Errorf("textNode.InheritedLanguage() = %q, want %q", got, "ja")
	}

	// Nearer lang shadows the outer one.
	div.Attributes = map[string]string{"lang": "en"}
	if got := span.InheritedLanguage(); got != "en" {
		t.Errorf("with div lang=en, span.InheritedLanguage() = %q, want %q", got, "en")
	}

	// No ancestor lang → "".
	bare := &Node{Type: ElementNode, TagName: "div"}
	if got := bare.InheritedLanguage(); got != "" {
		t.Errorf("bare.InheritedLanguage() = %q, want %q", got, "")
	}

	// Empty-string lang is treated as absent (per the implementation).
	bareWithEmpty := &Node{Type: ElementNode, TagName: "div", Attributes: map[string]string{"lang": ""}}
	if got := bareWithEmpty.InheritedLanguage(); got != "" {
		t.Errorf("empty lang attr: got %q, want %q", got, "")
	}
}

// TestUTF16OffsetToByteOffset covers the UTF-16-code-unit-to-UTF-8-byte
// conversion that Range.StartOffset/EndOffset (UTF-16, per this file's
// Range doc comment) need before indexing a Go (UTF-8) Node.Text string.
// LOU-344's selection-background-painting-order.html regression: a Range
// offset used directly as a byte index slices mid-rune for any character
// outside ASCII whose UTF-8 encoding is longer than 1 byte (e.g. ∫
// U+222B: 1 UTF-16 unit, 3 UTF-8 bytes).
func TestUTF16OffsetToByteOffset(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		utf16Offset int
		want        int
	}{
		{"zero offset", "hello", 0, 0},
		{"negative offset clamps to zero", "hello", -1, 0},
		{"ascii offset equals byte offset", "hello", 3, 3},
		{"ascii offset at end", "hello", 5, 5},
		// "ii∫∫∫": each ∫ (U+222B) is 1 UTF-16 unit but 3 UTF-8 bytes.
		// UTF-16 offset 3 = "ii∫" (3 code units), which is byte offset 5
		// (1+1+3), not byte offset 3 (which would land inside ∫'s
		// encoding) — this is the exact LOU-344 regression case.
		{"BMP multi-byte rune (integral sign)", "ii∫∫∫", 3, len("ii∫")},
		{"BMP multi-byte rune, offset 2", "ii∫∫∫", 2, len("ii")},
		{"BMP multi-byte rune, offset 5 (all selected)", "ii∫∫∫", 5, len("ii∫∫∫")},
		// "a🙂b": 🙂 (U+1F642) is outside the BMP, so it costs a UTF-16
		// surrogate pair (2 code units) but is 4 UTF-8 bytes.
		{"surrogate-pair rune before", "a🙂b", 1, len("a")},
		{"surrogate-pair rune spans 2 units", "a🙂b", 3, len("a🙂")},
		// Offset 2 falls strictly inside 🙂's surrogate pair (which spans
		// UTF-16 units [1,3)) — not a valid DOM boundary point in
		// practice, but defended against rather than panicking: rather
		// than slice the rune's own UTF-8 encoding in half, this rounds
		// up to the rune's end (byte offset of "🙂"'s last byte + 1).
		{"surrogate-pair rune, offset mid-pair rounds to rune end", "a🙂b", 2, len("a🙂")},
		{"offset beyond string clamps to full byte length", "hello", 100, len("hello")},
		{"empty string", "", 0, 0},
		{"empty string, positive offset clamps to zero", "", 5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UTF16OffsetToByteOffset(tt.text, tt.utf16Offset); got != tt.want {
				t.Errorf("UTF16OffsetToByteOffset(%q, %d) = %d, want %d", tt.text, tt.utf16Offset, got, tt.want)
			}
		})
	}
}
