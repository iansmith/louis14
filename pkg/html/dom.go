package html

import (
	"sort"
	"strings"
)

type Node struct {
	Type       NodeType
	TagName    string
	Attributes map[string]string
	Text       string
	Children   []*Node
	Parent     *Node // Phase 2: Support proper tree structure
}

type NodeType int

const (
	ElementNode NodeType = iota
	TextNode
)

type Document struct {
	Root        *Node
	Stylesheets []string // Phase 3: CSS from <style> tags
	Scripts     []string // JavaScript from <script> tags
}

func NewDocument() *Document {
	return &Document{
		Root: &Node{
			Type:     ElementNode,
			TagName:  "document",
			Children: make([]*Node, 0),
		},
		Stylesheets: make([]string, 0),
		Scripts:     make([]string, 0),
	}
}

func (n *Node) GetAttribute(name string) (string, bool) {
	if n.Attributes == nil {
		return "", false
	}
	val, ok := n.Attributes[name]
	return val, ok
}

// AddChild adds a child node and sets up the parent relationship
func (n *Node) AddChild(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

// AppendText creates a text node and adds it as a child
func (n *Node) AppendText(text string) {
	if text == "" {
		return
	}
	textNode := &Node{
		Type:   TextNode,
		Text:   text,
		Parent: n,
	}
	n.Children = append(n.Children, textNode)
}

// RemoveChild removes the given child from this node's children list,
// clears its parent pointer, and returns the removed child.
// Returns nil if child is not found.
func (n *Node) RemoveChild(child *Node) *Node {
	for i, c := range n.Children {
		if c == child {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			child.Parent = nil
			return child
		}
	}
	return nil
}

// InsertBefore inserts newChild before refChild in this node's children.
// If refChild is nil, appends newChild at the end.
// If newChild already has a parent, it is removed from that parent first.
func (n *Node) InsertBefore(newChild, refChild *Node) *Node {
	// Remove from old parent if re-parenting
	if newChild.Parent != nil {
		newChild.Parent.RemoveChild(newChild)
	}

	if refChild == nil {
		n.AddChild(newChild)
		return newChild
	}

	for i, c := range n.Children {
		if c == refChild {
			// Insert at position i
			n.Children = append(n.Children, nil)
			copy(n.Children[i+1:], n.Children[i:])
			n.Children[i] = newChild
			newChild.Parent = n
			return newChild
		}
	}

	// refChild not found — append
	n.AddChild(newChild)
	return newChild
}

// CloneNode returns a copy of the node. If deep is true, all descendants
// are cloned recursively. The clone has no parent.
func (n *Node) CloneNode(deep bool) *Node {
	clone := &Node{
		Type:    n.Type,
		TagName: n.TagName,
		Text:    n.Text,
	}
	if n.Attributes != nil {
		clone.Attributes = make(map[string]string, len(n.Attributes))
		for k, v := range n.Attributes {
			clone.Attributes[k] = v
		}
	}
	if deep {
		clone.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			childClone := child.CloneNode(true)
			childClone.Parent = clone
			clone.Children[i] = childClone
		}
	} else {
		clone.Children = make([]*Node, 0)
	}
	return clone
}

// Contains returns true if other is a descendant of n (or n itself).
func (n *Node) Contains(other *Node) bool {
	if n == other {
		return true
	}
	for _, child := range n.Children {
		if child.Contains(other) {
			return true
		}
	}
	return false
}

// IndexInParent returns the index of this node among its parent's children,
// or -1 if it has no parent.
func (n *Node) IndexInParent() int {
	if n.Parent == nil {
		return -1
	}
	for i, c := range n.Parent.Children {
		if c == n {
			return i
		}
	}
	return -1
}

// Serialize returns the innerHTML of this node — the serialized HTML of
// all child nodes, but not the node's own tags.
func (n *Node) Serialize() string {
	var sb strings.Builder
	for _, child := range n.Children {
		serializeNode(&sb, child)
	}
	return sb.String()
}

// SerializeOuter returns the outerHTML of this node — the node's own tags
// plus all descendants.
func (n *Node) SerializeOuter() string {
	var sb strings.Builder
	serializeNode(&sb, n)
	return sb.String()
}

func serializeNode(sb *strings.Builder, n *Node) {
	if n.Type == TextNode {
		sb.WriteString(escapeHTML(n.Text))
		return
	}

	sb.WriteByte('<')
	sb.WriteString(n.TagName)

	// Sort attributes for deterministic output
	if len(n.Attributes) > 0 {
		keys := make([]string, 0, len(n.Attributes))
		for k := range n.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteByte(' ')
			sb.WriteString(k)
			sb.WriteString(`="`)
			sb.WriteString(escapeAttr(n.Attributes[k]))
			sb.WriteByte('"')
		}
	}

	if isVoidElement(n.TagName) {
		sb.WriteString(">")
		return
	}

	sb.WriteByte('>')
	for _, child := range n.Children {
		serializeNode(sb, child)
	}
	sb.WriteString("</")
	sb.WriteString(n.TagName)
	sb.WriteByte('>')
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func isVoidElement(tag string) bool {
	switch tag {
	case "br", "hr", "img", "input", "meta", "link", "area", "base",
		"col", "embed", "param", "source", "track", "wbr":
		return true
	}
	return false
}
