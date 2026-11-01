package layout

import "testing"

// fieldsetRect lays src out and returns the single box rect of id.
func fieldsetRect(t *testing.T, rects map[string][]fragRect, id string) fragRect {
	t.Helper()
	got := rects[id]
	if len(got) != 1 {
		t.Fatalf("%s: want 1 box, got %d (%v)", id, len(got), got)
	}
	return got[0]
}

func checkFieldsetRect(t *testing.T, rects map[string][]fragRect, id string, want fragRect) {
	t.Helper()
	if got := fieldsetRect(t, rects, id); got != want {
		t.Errorf("%s: got %+v, want %+v", id, got, want)
	}
}

// TestFieldsetLegendAndContentBox pins FieldsetLayoutAlgorithm's placement:
// the legend sits at the block-start border edge (its border box is taller
// than the border) and is inset by border + padding inline; the content
// starts below the legend, inset by the fieldset padding, which the
// anonymous content box carries. Blink LayoutLegend / LayoutFieldsetContent
// (fieldset_layout_algorithm.cc:240-314, :316-428 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFieldsetLegendAndContentBox(t *testing.T) {
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<fieldset id="f" style="margin:0;width:200px;border:4px solid;padding:10px">`+
		`<legend id="l" style="height:20px;padding:0;width:50px"></legend>`+
		`<div id="c" style="height:30px"></div></fieldset>`)
	checkFieldsetRect(t, rects, "f", fragRect{0, 0, 228, 74})
	checkFieldsetRect(t, rects, "l", fragRect{14, 0, 50, 20})
	checkFieldsetRect(t, rects, "c", fragRect{14, 30, 200, 30})
}

// TestFieldsetLegendCenteredOnThickBorder: a legend thinner than the
// block-start border is centered on it, and the content starts after the
// border (fieldset_layout_algorithm.cc:265-280 @ a9f50e522efa).
func TestFieldsetLegendCenteredOnThickBorder(t *testing.T) {
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<fieldset id="f" style="margin:0;width:100px;border:0;border-top:30px solid;padding:0">`+
		`<legend id="l" style="height:10px;width:40px;padding:0"></legend>`+
		`<div id="c" style="height:20px"></div></fieldset>`)
	checkFieldsetRect(t, rects, "f", fragRect{0, 0, 100, 50})
	checkFieldsetRect(t, rects, "l", fragRect{0, 10, 40, 10})
	checkFieldsetRect(t, rects, "c", fragRect{0, 30, 100, 20})
}

// TestFieldsetColumnsApplyToContentBox: multicol on a fieldset is applied to
// its anonymous content box; the rendered legend stays outside the columns
// (layout_block_flow.cc:657-662, layout_fieldset.cc:130-141 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestFieldsetColumnsApplyToContentBox(t *testing.T) {
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<fieldset id="f" style="margin:0;border:0;padding:0;width:200px;column-count:2;column-gap:0">`+
		`<legend id="l" style="height:10px;width:30px;padding:0"></legend>`+
		`<div id="a" style="height:20px"></div><div id="b" style="height:20px"></div></fieldset>`)
	checkFieldsetRect(t, rects, "f", fragRect{0, 0, 200, 30})
	checkFieldsetRect(t, rects, "l", fragRect{0, 0, 30, 10})
	checkFieldsetRect(t, rects, "a", fragRect{0, 10, 100, 20})
	checkFieldsetRect(t, rects, "b", fragRect{100, 10, 100, 20})
}

// TestFieldsetLegendTextAlignRight: a legend is aligned along the border by
// its text-align (ComputeLegendBlockAlignment, fieldset_layout_algorithm.cc:
// 29-56 @ a9f50e522efa).
func TestFieldsetLegendTextAlignRight(t *testing.T) {
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+
		`<fieldset style="margin:0;border:0;padding:0;width:200px">`+
		`<legend id="l" style="height:10px;width:50px;padding:0;text-align:right"></legend></fieldset>`)
	checkFieldsetRect(t, rects, "l", fragRect{150, 0, 50, 10})
}

// TestFieldsetUAStyle pins the fieldset UA defaults (html.css:396-403 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): 2px inline margins, a 2px
// border, 0.35em/0.625em block and 0.75em inline padding.
func TestFieldsetUAStyle(t *testing.T) {
	rects := oofPageRects(t, `<!DOCTYPE html><body style="margin:0;font-size:20px">`+
		`<fieldset id="f" style="width:100px"><div id="c" style="height:10px"></div></fieldset>`)
	// Inline: 2 margin + 2 border + 15 padding; block: 2 border + 7 padding.
	checkFieldsetRect(t, rects, "c", fragRect{19, 9, 100, 10})
	// 100 + 30 + 4 wide; 10 + 7 + 12.5 + 4 tall.
	checkFieldsetRect(t, rects, "f", fragRect{2, 0, 134, 33.5})
}
