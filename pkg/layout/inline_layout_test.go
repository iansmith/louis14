package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/text"
	"math"
	"testing"
)

func makeTextNode(t string) *html.Node {
	return &html.Node{Type: html.TextNode, Text: t}
}

// inlineLayoutForTest exercises the inline layout pipeline directly:
// CollectInlines → LineBreaker → createLineBox. This bypasses the
// BlockLayoutAlgorithm dispatch so the tests work regardless of whether
// inline layout is wired into the production path.
func inlineLayoutForTest(
	parent *html.Node,
	styles map[*html.Node]*css.Style,
	contentInlineSize float64,
) (lineBoxes []*PhysicalFragment, totalBlockSize float64) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}

	// Phase 1: Collect inline items.
	itemsData := CollectInlines(parent, styles)
	if len(itemsData.Items) == 0 {
		return nil, 0
	}

	// Phase 2: Create line breaker.
	fonts := text.DefaultFontConfig()
	lineSpace := ConstraintSpace{
		AvailableSize:    LogicalSize{InlineSize: contentInlineSize, BlockSize: Indefinite},
		WritingDirection: wdm,
	}
	lb := NewLineBreaker(itemsData, ctx, lineSpace, fonts, LineBreakerContent)
	lb.availableWidth = contentInlineSize

	// Get text-align from container style.
	textAlign := "start"
	if parentStyle := styles[parent]; parentStyle != nil {
		if ta, ok := parentStyle.Get("text-align"); ok {
			textAlign = ta
		}
	}

	// Phase 3: Process lines.
	blockOffset := 0.0
	var line LineInfo

	for lb.NextLine(&line) {
		line.TextAlign = textAlign
		lineFragment, lineHeight := createLineBox(itemsData, &line, wdm, contentInlineSize)
		lineBoxes = append(lineBoxes, lineFragment)
		blockOffset += lineHeight
	}

	return lineBoxes, blockOffset
}

func TestInlineLayout_SimpleText(t *testing.T) {
	textNode := makeTextNode("Hello")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 800)

	if len(lineBoxes) != 1 {
		t.Fatalf("expected 1 line box, got %d", len(lineBoxes))
	}

	lb := lineBoxes[0]
	if lb.Type != FragmentLineBox {
		t.Errorf("expected FragmentLineBox, got %d", lb.Type)
	}

	if len(lb.Children) != 1 {
		t.Fatalf("expected 1 text fragment in line box, got %d", len(lb.Children))
	}

	textFrag := lb.Children[0].Fragment
	if textFrag.Type != FragmentText {
		t.Errorf("expected FragmentText, got %d", textFrag.Type)
	}
	if textFrag.TextContent != "Hello" {
		t.Errorf("text content: got %q, want %q", textFrag.TextContent, "Hello")
	}
}

func TestInlineLayout_AutoHeight(t *testing.T) {
	textNode := makeTextNode("Hello world")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	_, totalHeight := inlineLayoutForTest(parent, styles, 800)

	if totalHeight <= 0 {
		t.Errorf("total height should be > 0, got %f", totalHeight)
	}
}

func TestInlineLayout_MultipleLines(t *testing.T) {
	textNode := makeTextNode("This is a longer piece of text that should definitely wrap across multiple lines when constrained")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, totalHeight := inlineLayoutForTest(parent, styles, 200)

	if len(lineBoxes) < 2 {
		t.Errorf("expected multiple line boxes, got %d", len(lineBoxes))
	}

	for i, lb := range lineBoxes {
		if lb.Type != FragmentLineBox {
			t.Errorf("line %d: expected FragmentLineBox, got %d", i, lb.Type)
		}
	}

	if totalHeight <= 0 {
		t.Errorf("total height should be > 0, got %f", totalHeight)
	}
}

func TestInlineLayout_TextAlignCenter(t *testing.T) {
	textNode := makeTextNode("Hi")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px", "text-align", "center")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 400)

	if len(lineBoxes) < 1 || len(lineBoxes[0].Children) < 1 {
		t.Fatal("no text fragment in line box")
	}

	textOffset := lineBoxes[0].Children[0].Offset.X
	if textOffset <= 0 {
		t.Errorf("text-align:center should offset text, got X=%f", textOffset)
	}
}

func TestInlineLayout_TextAlignRight(t *testing.T) {
	textNode := makeTextNode("Hi")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px", "text-align", "right")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 400)

	textOffset := lineBoxes[0].Children[0].Offset.X
	if textOffset <= 0 {
		t.Errorf("text-align:right should offset text, got X=%f", textOffset)
	}

	// Compare with center: right should offset more.
	textNodeC := makeTextNode("Hi")
	parentC := makeNode("div", textNodeC)
	parentStyleC := makeStyle("display", "block", "font-size", "16px", "text-align", "center")
	stylesC := map[*html.Node]*css.Style{parentC: parentStyleC}

	lineBoxesC, _ := inlineLayoutForTest(parentC, stylesC, 400)
	centerOffset := lineBoxesC[0].Children[0].Offset.X

	if textOffset <= centerOffset {
		t.Errorf("right offset (%f) should be > center offset (%f)", textOffset, centerOffset)
	}
}

func TestInlineLayout_HasOnlyInlineChildren(t *testing.T) {
	tests := []struct {
		name     string
		children []*html.Node
		styles   map[*html.Node]*css.Style
		want     bool
	}{
		{
			name:     "text only",
			children: []*html.Node{makeTextNode("hello")},
			want:     true,
		},
		{
			name:     "whitespace only",
			children: []*html.Node{makeTextNode("   ")},
			want:     false,
		},
		{
			name:     "block child",
			children: []*html.Node{makeNode("div")},
			styles:   map[*html.Node]*css.Style{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := makeNode("div", tt.children...)
			if tt.styles == nil {
				tt.styles = map[*html.Node]*css.Style{}
			}
			for _, c := range tt.children {
				if c.Type == html.ElementNode {
					if _, ok := tt.styles[c]; !ok {
						tt.styles[c] = makeStyle("display", "block")
					}
				}
			}
			got := hasOnlyInlineChildren(parent, tt.styles)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInlineLayout_InlineSpan(t *testing.T) {
	// <div>Hello <span>world</span></div>
	text1 := makeTextNode("Hello ")
	text2 := makeTextNode("world")
	span := makeNode("span", text2)
	parent := makeNode("div", text1, span)

	parentStyle := makeStyle("display", "block", "font-size", "16px")
	spanStyle := makeStyle("display", "inline", "font-size", "16px")
	styles := map[*html.Node]*css.Style{
		parent: parentStyle,
		span:   spanStyle,
	}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 800)

	if len(lineBoxes) < 1 {
		t.Fatal("expected at least 1 line box")
	}

	var allText string
	for _, lb := range lineBoxes {
		for _, child := range lb.Children {
			if child.Fragment.Type == FragmentText {
				allText += child.Fragment.TextContent
			}
		}
	}

	if allText != "Hello world" {
		t.Errorf("all text: got %q, want %q", allText, "Hello world")
	}
}

func TestInlineLayout_FragmentToBox_PreservesText(t *testing.T) {
	// Build a text fragment manually and verify fragmentToBox handles it.
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	parentNode := makeNode("div")
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parentNode: parentStyle}

	textFrag := &PhysicalFragment{
		Size:             PhysicalSize{Width: 50, Height: 16},
		Type:             FragmentText,
		TextContent:      "Test text",
		Node:             parentNode,
		WritingDirection: wdm,
	}

	lineFrag := &PhysicalFragment{
		Size: PhysicalSize{Width: 800, Height: 20},
		Children: []ChildLink{
			{Offset: PhysicalOffset{X: 0, Y: 2}, Fragment: textFrag},
		},
		Type:             FragmentLineBox,
		WritingDirection: wdm,
	}

	containerFrag := &PhysicalFragment{
		Size: PhysicalSize{Width: 800, Height: 20},
		Children: []ChildLink{
			{Offset: PhysicalOffset{X: 0, Y: 0}, Fragment: lineFrag},
		},
		Node:             parentNode,
		WritingDirection: wdm,
	}

	box := fragmentToBox(containerFrag, styles, nil, 0, 0)

	found := false
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Text == "Test text" {
			found = true
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(box)

	if !found {
		t.Error("fragmentToBox should produce a Box with Text='Test text'")
	}
}

func TestInlineLayout_LineBoxHeight(t *testing.T) {
	textNode := makeTextNode("Hello")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "20px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 800)

	if len(lineBoxes) < 1 {
		t.Fatal("no line boxes")
	}

	// Line box height should be approximately the font size (20px).
	h := lineBoxes[0].Size.Height
	if h < 15 || h > 30 {
		t.Errorf("line box height: got %f, expected ~20", h)
	}
}

func TestInlineLayout_EmptyContainer(t *testing.T) {
	parent := makeNode("div")
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, totalHeight := inlineLayoutForTest(parent, styles, 800)

	if len(lineBoxes) != 0 {
		t.Errorf("empty container should have 0 line boxes, got %d", len(lineBoxes))
	}
	if totalHeight != 0 {
		t.Errorf("empty container height: got %f, want 0", totalHeight)
	}
}

func TestInlineLayout_TextPositioning(t *testing.T) {
	textNode := makeTextNode("ABC")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	lineBoxes, _ := inlineLayoutForTest(parent, styles, 800)

	textFrag := lineBoxes[0].Children[0]

	// Left-aligned: text should start at X=0.
	if math.Abs(textFrag.Offset.X) > 0.1 {
		t.Errorf("left-aligned text X offset: got %f, want ~0", textFrag.Offset.X)
	}

	// Text width should be > 0.
	if textFrag.Fragment.Size.Width <= 0 {
		t.Errorf("text width should be > 0, got %f", textFrag.Fragment.Size.Width)
	}
}

func TestInlineLayout_CollectInlines(t *testing.T) {
	text1 := makeTextNode("Hello ")
	text2 := makeTextNode("world")
	span := makeNode("span", text2)
	parent := makeNode("div", text1, span)

	spanStyle := makeStyle("display", "inline")
	styles := map[*html.Node]*css.Style{span: spanStyle}

	data := CollectInlines(parent, styles)

	if data.TextContent != "Hello world" {
		t.Errorf("TextContent: got %q, want %q", data.TextContent, "Hello world")
	}

	// Should have: Text("Hello "), OpenTag(span), Text("world"), CloseTag(span)
	if len(data.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(data.Items))
	}

	if data.Items[0].Type != InlineItemText {
		t.Errorf("item 0: expected Text, got %d", data.Items[0].Type)
	}
	if data.Items[1].Type != InlineItemOpenTag {
		t.Errorf("item 1: expected OpenTag, got %d", data.Items[1].Type)
	}
	if data.Items[2].Type != InlineItemText {
		t.Errorf("item 2: expected Text, got %d", data.Items[2].Type)
	}
	if data.Items[3].Type != InlineItemCloseTag {
		t.Errorf("item 3: expected CloseTag, got %d", data.Items[3].Type)
	}
}

func TestInlineLayout_LineBreaker(t *testing.T) {
	textNode := makeTextNode("Hello world")
	parent := makeNode("div", textNode)
	parentStyle := makeStyle("display", "block", "font-size", "16px")
	styles := map[*html.Node]*css.Style{parent: parentStyle}

	data := CollectInlines(parent, styles)
	fonts := text.DefaultFontConfig()
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	ctx := &LayoutContext{ComputedStyles: styles, ViewportWidth: 800, ViewportHeight: 600}

	space := ConstraintSpace{
		AvailableSize:    LogicalSize{InlineSize: 800, BlockSize: Indefinite},
		WritingDirection: wdm,
	}
	lb := NewLineBreaker(data, ctx, space, fonts, LineBreakerContent)

	var line LineInfo
	lineCount := 0
	for lb.NextLine(&line) {
		lineCount++
		if line.Width <= 0 {
			t.Errorf("line %d width should be > 0", lineCount)
		}
	}

	if lineCount != 1 {
		t.Errorf("expected 1 line for short text, got %d", lineCount)
	}
}
