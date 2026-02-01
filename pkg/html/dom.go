package html

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
	Root *Node
}

func NewDocument() *Document {
	return &Document{
		Root: &Node{
			Type:     ElementNode,
			TagName:  "document",
			Children: make([]*Node, 0),
		},
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
