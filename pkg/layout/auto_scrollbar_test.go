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
// grows by an overflowing descendant's auto scrollbar, whether that
// descendant is a block or an atomic inline: its min/max contribution
// includes its scrollbars (ComputeScrollbarsForNonAnonymous,
// core/layout/length_utils.cc:1543-1549 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the parent is laid out
// again when the scrollbar appears (BlockNode::Layout,
// core/layout/block_node.cc:552-644, same SHA). The block case is
// css-sizing/auto-scrollbar-inside-stf-abspos.
func TestAutoScrollbar_ShrinkToFitParentGrows(t *testing.T) {
	const content = `<div style="width: 100px; height: 200px"></div>`
	for _, tc := range []struct{ name, html string }{
		{"block in abspos", `<div id="a" style="position: absolute"><div id="s" style="height: 50px; overflow-y: auto">` + content + `</div></div>`},
		{"inline-block in float", `<div id="a" style="float: left"><span id="s" style="display: inline-block; height: 50px; overflow: auto">` + content + `</span></div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>`+tc.html)
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
		})
	}
}

// TestAutoScrollbar_NoneWhenContentsAreNotVisible pins that a horizontal
// auto scrollbar is not added while the scrollport has no height
// (ComputeScrollbarExistence, core/paint/paint_layer_scrollable_area.cc:
// 1834-1847 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). A 0-tall auto box
// whose out-of-flow content overflows it vertically gets a vertical
// scrollbar (its scrollport has width), which narrows it so the content
// overflows horizontally too; it still gets no horizontal scrollbar and
// stays 0 tall. The css-overflow case scrollbar-empty-002.
func TestAutoScrollbar_NoneWhenContentsAreNotVisible(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>
<div style="width: 300px"><div id="s" style="overflow: auto"><div style="height: 0; position: relative"><div style="position: absolute; width: 300px; height: 300px"></div></div></div></div>`)
	s := findBoxByID(root, "s")
	if s == nil {
		t.Fatal("missing #s")
	}
	if want := (css.BoxEdge{Right: classicScrollbarWidth(s.Style)}); s.Scrollbar != want || s.Height != 0 {
		t.Errorf("#s scrollbar %+v, height %v; want %+v and 0", s.Scrollbar, s.Height, want)
	}
}

// TestAutoScrollbar_TableCellDecidesAgainstItsFinalBlockSize pins a table
// cell's auto scrollbars. A cell is laid out once, at its final inline size
// but before the table stretches its block size to the row; Blink decides
// on the final layout (UpdateAfterLayout / TryRemovingAutoScrollbars,
// core/paint/paint_layer_scrollable_area.cc:1021-1075, :1851-1890 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The table makes the cell at
// least as tall as its content (the row's minimum, table_layout.go), so the
// block axis is decided against that size, not the measure size. A row
// stretched further by another cell is LOU-918.
func TestAutoScrollbar_TableCellDecidesAgainstItsFinalBlockSize(t *testing.T) {
	sb := classicScrollbarWidth(&css.Style{})
	for _, tc := range []struct {
		name, html string
		want       css.BoxEdge
		w, h       float64
	}{
		// The cell's measure height is 50 but the row grows to the 200px
		// content: no block-axis scrollbar, and nothing overflows inline.
		{"td grows to its content", `<table style="border-spacing: 0"><tr><td id="s" style="padding: 0; height: 50px; overflow: auto"><div style="width: 100px; height: 200px"></div></td></tr></table>`,
			css.BoxEdge{}, 100, 200},
		{"table-cell grows to its content", `<div style="display: table"><div id="s" style="display: table-cell; height: 50px; overflow: auto"><div style="width: 100px; height: 200px"></div></div></div>`,
			css.BoxEdge{}, 100, 200},
		// A fixed-layout cell narrower than its content: the inline-axis
		// scrollbar (css-tables/table-cell-overflow-auto-scrolled).
		{"fixed-width cell overflows inline", `<table style="table-layout: fixed; width: 100px; height: 100px; border-spacing: 0"><tr><td id="s" style="padding: 0; overflow: auto; vertical-align: top"><div style="width: 200px; height: 50px"></div></td></tr></table>`,
			css.BoxEdge{Bottom: sb}, 100, 100},
		// A 100x100 cell whose 0x0 wrapper's content overflows it both ways: the
		// cell stays 100 tall, so both scrollbars
		// (css-backgrounds/table-cell-background-local-003).
		{"fixed-size cell overflows both ways", `<table style="border-spacing: 0"><tr><td id="s" style="width: 100px; height: 100px; padding: 0; overflow: auto; vertical-align: top"><div style="width: 0; height: 0"><div style="width: 300px; height: 300px"></div></div></td></tr></table>`,
			css.BoxEdge{Right: sb, Bottom: sb}, 100, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, `<!DOCTYPE html><style>body { margin: 0 }</style>`+tc.html)
			s := findBoxByID(root, "s")
			if s == nil {
				t.Fatal("missing #s")
			}
			if s.Scrollbar != tc.want || s.Width != tc.w || s.Height != tc.h {
				t.Errorf("#s %vx%v scrollbar %+v; want %vx%v with %+v", s.Width, s.Height, s.Scrollbar, tc.w, tc.h, tc.want)
			}
		})
	}
}
