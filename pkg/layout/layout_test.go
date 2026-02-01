package layout

import (
	"testing"
	"louis14/pkg/html"
)

func TestLayoutEngine_SingleBox(t *testing.T) {
	doc := html.NewDocument()
	node := &html.Node{
		Type:    html.ElementNode,
		TagName: "div",
		Attributes: map[string]string{
			"style": "width: 200px; height: 100px;",
		},
	}
	doc.Root.Children = append(doc.Root.Children, node)
	
	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)
	
	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}
	if boxes[0].Width != 200.0 || boxes[0].Height != 100.0 {
		t.Errorf("expected 200x100, got %fx%f", boxes[0].Width, boxes[0].Height)
	}
}

func TestLayoutEngine_VerticalStacking(t *testing.T) {
	doc := html.NewDocument()
	for i := 0; i < 3; i++ {
		node := &html.Node{
			Type:       html.ElementNode,
			TagName:    "div",
			Attributes: map[string]string{"style": "height: 50px;"},
		}
		doc.Root.Children = append(doc.Root.Children, node)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 3 {
		t.Fatalf("expected 3 boxes, got %d", len(boxes))
	}
	if boxes[0].Y != 0.0 || boxes[1].Y != 50.0 || boxes[2].Y != 100.0 {
		t.Error("boxes not stacking correctly")
	}
}

// Phase 2 tests: Nested elements and box model

func TestLayoutEngine_NestedElements(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 200px; height: 100px;"><p style="width: 50px; height: 30px;"></p></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	div := boxes[0]
	if div.Width != 200 || div.Height != 100 {
		t.Errorf("div: expected 200x100, got %fx%f", div.Width, div.Height)
	}

	if len(div.Children) != 1 {
		t.Fatalf("expected div to have 1 child, got %d", len(div.Children))
	}

	p := div.Children[0]
	if p.Width != 50 || p.Height != 30 {
		t.Errorf("p: expected 50x30, got %fx%f", p.Width, p.Height)
	}
}

func TestLayoutEngine_Padding(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 100px; height: 100px; padding: 10px;"></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	box := boxes[0]
	if box.Padding.Top != 10 || box.Padding.Right != 10 ||
		box.Padding.Bottom != 10 || box.Padding.Left != 10 {
		t.Errorf("expected padding 10 on all sides, got %+v", box.Padding)
	}

	// Content dimensions should be as specified
	if box.Width != 100 || box.Height != 100 {
		t.Errorf("expected content size 100x100, got %fx%f", box.Width, box.Height)
	}
}

func TestLayoutEngine_Margin(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 100px; height: 100px; margin: 20px;"></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	box := boxes[0]
	if box.Margin.Top != 20 || box.Margin.Right != 20 ||
		box.Margin.Bottom != 20 || box.Margin.Left != 20 {
		t.Errorf("expected margin 20 on all sides, got %+v", box.Margin)
	}

	// Position should account for margin
	if box.X != 20 || box.Y != 20 {
		t.Errorf("expected position (20,20) with margin, got (%f,%f)", box.X, box.Y)
	}
}

func TestLayoutEngine_Border(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 100px; height: 100px; border: 5px solid black;"></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	box := boxes[0]
	if box.Border.Top != 5 || box.Border.Right != 5 ||
		box.Border.Bottom != 5 || box.Border.Left != 5 {
		t.Errorf("expected border 5 on all sides, got %+v", box.Border)
	}
}

func TestLayoutEngine_FullBoxModel(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 100px; height: 100px; margin: 10px; padding: 20px; border: 5px solid red;"></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}

	box := boxes[0]

	// Check all box model components
	if box.Margin.Top != 10 {
		t.Errorf("expected margin 10, got %f", box.Margin.Top)
	}
	if box.Padding.Top != 20 {
		t.Errorf("expected padding 20, got %f", box.Padding.Top)
	}
	if box.Border.Top != 5 {
		t.Errorf("expected border 5, got %f", box.Border.Top)
	}

	// Content dimensions
	if box.Width != 100 || box.Height != 100 {
		t.Errorf("expected content 100x100, got %fx%f", box.Width, box.Height)
	}
}

func TestLayoutEngine_NestedWithPadding(t *testing.T) {
	doc, err := html.Parse(`<div style="width: 200px; height: 200px; padding: 20px;"><p style="width: 50px; height: 50px;"></p></div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	div := boxes[0]
	if len(div.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(div.Children))
	}

	p := div.Children[0]

	// Child should be positioned inside parent's padding
	// Parent is at (0,0), child should be at (0 + padding.Left, 0 + padding.Top) = (20, 20)
	if p.X != 20 || p.Y != 20 {
		t.Errorf("expected child at (20,20) accounting for parent padding, got (%f,%f)", p.X, p.Y)
	}
}
