package layout

import (
	"testing"

	"louis14/pkg/css"
)

// TestAutoScrollbar_ExistsOnlyWhenContentOverflows pins overflow:auto
// scrollbars: after layout a box's auto scrollbar exists when its content
// overflows that axis (PaintLayerScrollableArea::UpdateAfterLayout /
// ComputeScrollbarExistence / HasVerticalOverflow,
// core/paint/paint_layer_scrollable_area.cc:1021-1075, :1732-1830,
// :1384-1389 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the box is
// laid out again with it, so the scrollbar is a gutter between the border
// and the padding box like overflow:scroll's (LayoutBox::
// ComputeScrollbarsInternal, core/layout/layout_box.cc:1577-1630; the
// relayout in BlockNode::Layout, core/layout/block_node.cc:552-644, same
// SHA).
func TestAutoScrollbar_ExistsOnlyWhenContentOverflows(t *testing.T) {
	sb := classicScrollbarWidth(&css.Style{})
	tests := []struct {
		name         string
		style        string
		want         css.BoxEdge
		contentWidth float64 // #c's border-box width
	}{
		{"auto-y overflowing", "overflow-y: auto", css.BoxEdge{Right: sb}, 100 - sb},
		{"auto-y fitting", "overflow-y: auto; height: 300px", css.BoxEdge{}, 100},
		{"auto-x with only vertical overflow", "overflow-x: auto; overflow-y: hidden", css.BoxEdge{}, 100},
		{"auto both, vertical overflow only", "overflow: auto", css.BoxEdge{Right: sb}, 100 - sb},
		{"auto-y in RTL puts the bar on the left", "overflow-y: auto; direction: rtl", css.BoxEdge{Left: sb}, 100 - sb},
		{"scroll still reserves when fitting", "overflow-y: scroll; height: 300px", css.BoxEdge{Right: sb}, 100 - sb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div id="s" style="width: 100px; height: 50px; `+tt.style+`"><div id="c" style="height: 200px"></div></div>`)
			s, c := findBoxByID(root, "s"), findBoxByID(root, "c")
			if s == nil || c == nil {
				t.Fatal("missing #s or #c")
			}
			if s.Scrollbar != tt.want {
				t.Errorf("#s scrollbar = %+v, want %+v", s.Scrollbar, tt.want)
			}
			if c.Width != tt.contentWidth {
				t.Errorf("#c width = %v, want %v (the scrollbar narrows the content box)", c.Width, tt.contentWidth)
			}
		})
	}
}

// TestAutoScrollbar_ShrinkToFitParentGrows pins that a shrink-to-fit box
// grows by an overflowing child's auto scrollbar: the child's intrinsic
// inline size includes its scrollbars (ComputeScrollbarsForNonAnonymous,
// core/layout/length_utils.cc:1543-1549 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the abspos parent is laid
// out again when the scrollbar appears (BlockNode::Layout,
// core/layout/block_node.cc:552-644, same SHA). The css-sizing case
// auto-scrollbar-inside-stf-abspos.
func TestAutoScrollbar_ShrinkToFitParentGrows(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div id="a" style="position: absolute"><div id="s" style="height: 50px; overflow-y: auto"><div style="width: 100px; height: 200px"></div></div></div>`)
	a, s := findBoxByID(root, "a"), findBoxByID(root, "s")
	if a == nil || s == nil {
		t.Fatal("missing #a or #s")
	}
	sb := classicScrollbarWidth(s.Style)
	if s.Scrollbar.Right != sb {
		t.Errorf("#s scrollbar right = %v, want %v", s.Scrollbar.Right, sb)
	}
	if a.Width != 100+sb || s.Width != 100+sb {
		t.Errorf("#a width %v, #s width %v; want both %v (100 content + the scrollbar)", a.Width, s.Width, 100+sb)
	}
}
