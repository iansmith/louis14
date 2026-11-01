package layout

import (
	"testing"

	"louis14/pkg/html"
)

// TestBreakBeforeListItemKeepsMarker pins Blink's
// BlockBreakToken::CreateBreakBefore, which marks a break-before token for a
// list item as having an unpositioned list marker
// (has_unpositioned_list_marker_ = node.IsListItem(), block_break_token.h:48
// @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The list item's first
// fragment then still seeds its outside marker
// (block_layout_algorithm.cc:323-328), so a list item pushed whole into the
// next column keeps its ::marker (WPT multicol-list-item-008's reference).
func TestBreakBeforeListItemKeepsMarker(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0">` +
		`<div style="font-size:20px;line-height:50px;columns:2;column-fill:auto;column-gap:0;width:200px;height:100px;margin-left:50px">` +
		`<div style="height:100px">Normal</div>` +
		`<div id=li style="display:list-item"><div>List item</div></div></div>`
	doc, err := html.Parse(src)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	var li *Box
	var markers []*Box
	var walk func(b *Box)
	walk = func(b *Box) {
		if b.Node != nil {
			if id, _ := b.Node.GetAttribute("id"); id == "li" {
				li = b
			}
			if b.Node.TagName == "::marker" && b.Text == "" {
				markers = append(markers, b)
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, b := range NewLayoutEngine(800, 600).Layout(doc) {
		walk(b)
	}
	if li == nil {
		t.Fatal("no list-item box")
	}
	if li.X != 150 || li.Y != 0 {
		t.Errorf("list item at (%v,%v), want (150,0) in column 2", li.X, li.Y)
	}
	if len(markers) != 1 {
		t.Fatalf("got %d ::marker boxes, want 1", len(markers))
	}
	if m := markers[0]; m.Y != li.Y || m.X >= li.X {
		t.Errorf("::marker at (%v,%v), want outside the list item at y=%v, x<%v", m.X, m.Y, li.Y, li.X)
	}
}
