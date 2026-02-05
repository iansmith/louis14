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
		Type:       ElementNode,
		TagName:    "div",
		Children:   make([]*Node, 0),
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
