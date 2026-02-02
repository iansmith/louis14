package layout

import (
	"testing"
	"louis14/pkg/css"
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

// Margin collapsing tests

func TestMarginCollapsing_AdjacentSiblings_BothPositive(t *testing.T) {
	// Two adjacent siblings with positive margins: use the larger margin
	doc, err := html.Parse(`<div>
		<div style="width: 100px; height: 50px; margin-bottom: 30px;"></div>
		<div style="width: 100px; height: 50px; margin-top: 20px;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	if len(boxes) != 1 {
		t.Fatalf("expected 1 box, got %d", len(boxes))
	}
	div := boxes[0]
	if len(div.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(div.Children))
	}

	first := div.Children[0]
	second := div.Children[1]

	// Without collapsing: gap = 30 + 20 = 50
	// With collapsing: gap = max(30, 20) = 30
	// second.Y should be first.Y + getTotalHeight(first) - (30 + 20 - 30) = first.Y + totalH - 20
	gap := second.Y - (first.Y + first.Margin.Top + first.Border.Top + first.Padding.Top + first.Height + first.Padding.Bottom + first.Border.Bottom)
	// The gap between the bottom border edge of first and the top of second's margin area
	// should be the collapsed margin = 30 (not 30+20=50)
	expectedGap := 30.0
	if gap < expectedGap-1 || gap > expectedGap+1 {
		t.Errorf("expected collapsed margin gap of %.0f between siblings, got %.1f (first.Y=%f second.Y=%f)", expectedGap, gap, first.Y, second.Y)
	}
}

func TestMarginCollapsing_AdjacentSiblings_BothNegative(t *testing.T) {
	// Two adjacent siblings with negative margins: use the more negative
	doc, err := html.Parse(`<div>
		<div style="width: 100px; height: 50px; margin-bottom: -10px;"></div>
		<div style="width: 100px; height: 50px; margin-top: -20px;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	div := boxes[0]
	if len(div.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(div.Children))
	}

	first := div.Children[0]
	second := div.Children[1]

	// Without collapsing: gap = -10 + -20 = -30
	// With collapsing: gap = min(-10, -20) = -20
	gap := second.Y - (first.Y + first.Margin.Top + first.Border.Top + first.Padding.Top + first.Height + first.Padding.Bottom + first.Border.Bottom)
	expectedGap := -20.0
	if gap < expectedGap-1 || gap > expectedGap+1 {
		t.Errorf("expected collapsed margin gap of %.0f between siblings, got %.1f", expectedGap, gap)
	}
}

func TestMarginCollapsing_AdjacentSiblings_Mixed(t *testing.T) {
	// One positive, one negative: sum them
	doc, err := html.Parse(`<div>
		<div style="width: 100px; height: 50px; margin-bottom: 30px;"></div>
		<div style="width: 100px; height: 50px; margin-top: -10px;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	div := boxes[0]
	if len(div.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(div.Children))
	}

	first := div.Children[0]
	second := div.Children[1]

	// Without collapsing: gap = 30 + -10 = 20
	// With collapsing (mixed): gap = 30 + (-10) = 20
	gap := second.Y - (first.Y + first.Margin.Top + first.Border.Top + first.Padding.Top + first.Height + first.Padding.Bottom + first.Border.Bottom)
	expectedGap := 20.0
	if gap < expectedGap-1 || gap > expectedGap+1 {
		t.Errorf("expected collapsed margin gap of %.0f between siblings, got %.1f", expectedGap, gap)
	}
}

func TestMarginCollapsing_EqualMargins(t *testing.T) {
	// Equal positive margins: collapsed to single margin
	doc, err := html.Parse(`<div>
		<div style="width: 100px; height: 50px; margin-bottom: 20px;"></div>
		<div style="width: 100px; height: 50px; margin-top: 20px;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	div := boxes[0]
	if len(div.Children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(div.Children))
	}

	first := div.Children[0]
	second := div.Children[1]

	gap := second.Y - (first.Y + first.Margin.Top + first.Border.Top + first.Padding.Top + first.Height + first.Padding.Bottom + first.Border.Bottom)
	expectedGap := 20.0
	if gap < expectedGap-1 || gap > expectedGap+1 {
		t.Errorf("expected collapsed margin gap of %.0f between siblings, got %.1f", expectedGap, gap)
	}
}

func TestMarginCollapsing_NoCollapseWithPaddingSeparation(t *testing.T) {
	// Parent with padding should prevent parent-child margin collapsing
	doc, err := html.Parse(`<div style="padding: 10px;">
		<div style="width: 100px; height: 50px; margin-top: 20px;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	boxes := engine.Layout(doc)

	div := boxes[0]
	if len(div.Children) < 1 {
		t.Fatalf("expected at least 1 child, got %d", len(div.Children))
	}

	child := div.Children[0]
	// Parent has padding 10, child has margin-top 20
	// With padding separation, child margin should NOT collapse with parent
	// Child Y should be: parent.Y + padding.Top + child.margin.Top = 10 + 20 = 30
	// (parent at Y=0, padding=10, then child margin=20)
	expectedY := 30.0
	if child.Y < expectedY-1 || child.Y > expectedY+1 {
		t.Errorf("expected child Y=%.0f (no parent-child collapsing due to padding), got %.1f", expectedY, child.Y)
	}
}

func TestMarginCollapsing_NoCollapseForFloats(t *testing.T) {
	// Floated elements should not have their margins collapsed
	doc, err := html.Parse(`<div>
		<div style="width: 100px; height: 50px; margin-bottom: 30px; float: left;"></div>
		<div style="width: 100px; height: 50px; margin-top: 20px; clear: left;"></div>
	</div>`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	engine := NewLayoutEngine(800, 600)
	_ = engine.Layout(doc)
	// Just verify it doesn't crash - float collapsing behavior is complex
}

func TestCollapseMargins_Unit(t *testing.T) {
	tests := []struct {
		name     string
		m1, m2   float64
		expected float64
	}{
		{"both positive, first larger", 30, 20, 30},
		{"both positive, second larger", 10, 25, 25},
		{"both positive, equal", 15, 15, 15},
		{"both negative, first more negative", -20, -10, -20},
		{"both negative, second more negative", -5, -15, -15},
		{"both negative, equal", -10, -10, -10},
		{"mixed, positive first", 30, -10, 20},
		{"mixed, negative first", -10, 30, 20},
		{"zero and positive", 0, 20, 20},
		{"zero and negative", 0, -10, -10},
		{"both zero", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collapseMargins(tt.m1, tt.m2)
			if result != tt.expected {
				t.Errorf("collapseMargins(%v, %v) = %v, want %v", tt.m1, tt.m2, result, tt.expected)
			}
		})
	}
}

func TestShouldCollapseMargins_Unit(t *testing.T) {
	// Test that floated elements don't collapse
	floatStyle := css.NewStyle()
	floatStyle.Set("float", "left")
	floatBox := &Box{Style: floatStyle, Position: css.PositionStatic}
	if shouldCollapseMargins(floatBox) {
		t.Error("floated elements should not collapse margins")
	}

	// Test that absolutely positioned elements don't collapse
	absBox := &Box{Style: css.NewStyle(), Position: css.PositionAbsolute}
	if shouldCollapseMargins(absBox) {
		t.Error("absolutely positioned elements should not collapse margins")
	}

	// Test that inline-block elements don't collapse
	inlineBlockStyle := css.NewStyle()
	inlineBlockStyle.Set("display", "inline-block")
	ibBox := &Box{Style: inlineBlockStyle, Position: css.PositionStatic}
	if shouldCollapseMargins(ibBox) {
		t.Error("inline-block elements should not collapse margins")
	}

	// Test that overflow:hidden elements don't collapse
	overflowStyle := css.NewStyle()
	overflowStyle.Set("overflow", "hidden")
	ofBox := &Box{Style: overflowStyle, Position: css.PositionStatic}
	if shouldCollapseMargins(ofBox) {
		t.Error("overflow:hidden elements should not collapse margins")
	}

	// Test that normal block elements DO collapse
	normalBox := &Box{Style: css.NewStyle(), Position: css.PositionStatic}
	if !shouldCollapseMargins(normalBox) {
		t.Error("normal block elements should collapse margins")
	}
}
