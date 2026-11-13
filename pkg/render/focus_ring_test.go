package render

import (
	"image/color"
	"testing"
)

// TestFocusRing_SingleRect pins Blink's focus ring for outline-style: auto
// around one rect (PaintFocusRing / PaintSingleFocusRing / FocusRingOffset,
// core/paint/outline_painter.cc:38-74, :758-811, :813-859, :861-930 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; DrawPlatformFocusRing,
// platform/graphics/platform_focus_ring.cc, same SHA): a 3px ring of a 2px
// outer stroke in white centred one pixel outside the outline rect, then a
// 2px inner stroke in outline-color centred on it, corner radius 3. With no
// border, outline-offset 0 and the box's left edge at x = 20, the outer
// stroke covers x 18-19 and the inner, painted over it, x 19-20 (one pixel
// inside the box): x 18 white, 19 and 20 blue, 21 the box's green.
func TestFocusRing_SingleRect(t *testing.T) {
	doc := ParsePage(t, `<!doctype html><style>
body { margin: 0; background: black }
#t { position: absolute; left: 20px; top: 20px; width: 100px; height: 50px; background: lime; outline: auto 1px blue }
</style><div id=t></div>`)
	img, _ := RenderDocument(t, doc, 160, 100)
	white := color.RGBA{255, 255, 255, 255}
	blue := color.RGBA{0, 0, 255, 255}
	lime := color.RGBA{0, 255, 0, 255}
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{18, 45, white}, {19, 45, blue}, {20, 45, blue}, {21, 45, lime}, // left edge
		{70, 18, white}, {70, 19, blue}, {70, 20, blue}, {70, 21, lime}, // top edge
		{121, 45, white}, {120, 45, blue}, {119, 45, blue}, {118, 45, lime}, // right edge
	} {
		if got := img.RGBAAt(p.x, p.y); got != p.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}

// TestFocusRing_MultiRect pins Blink's focus ring around an inline split
// across two lines: the fragment rects are united as a region whose
// boundary is stroked (PaintSingleFocusRing → ComputeRightAnglePath,
// core/paint/outline_painter.cc:87-101, :813-859 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not their bounding box. The
// span's first fragment is (90,10)-(130,30) and its second (10,30)-(90,50)
// in Ahem 20px, so the inner ring covers y 29-30 under the first line's
// "XXX " and nothing runs along the bounding box's top and bottom edges
// there.
func TestFocusRing_MultiRect(t *testing.T) {
	doc := ParsePage(t, splitInlineOutlinePage("auto 1px blue"))
	img, _ := RenderDocument(t, doc, 160, 80)
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	opPixel(t, img, 110, 9, blue, "guard: the first fragment's top edge is ringed")
	opPixel(t, img, 50, 29, blue, "second fragment's top edge, outer row")
	opPixel(t, img, 50, 30, blue, "second fragment's top edge, inner row")
	opPixel(t, img, 50, 9, black, "no ring along the bounding box's top edge over \"XXX \"")
	opPixel(t, img, 110, 49, black, "no ring along the bounding box's bottom edge under the first fragment")
}

// ringProbe is a pixel a focus ring test expects to be colour want,
// and why.
type ringProbe struct {
	x, y int
	want color.RGBA
	why  string
}

// ringCase is a page body on black and the pixels its rendering must have.
type ringCase struct {
	name, body string
	probes     []ringProbe
}

// runRingCases renders each case at w×h and checks its probes.
func runRingCases(t *testing.T, cases []ringCase, w, h int) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := ParsePage(t, `<!doctype html><style>body { margin: 0; background: black }</style>`+tc.body)
			img, _ := RenderDocument(t, doc, w, h)
			for _, p := range tc.probes {
				opPixel(t, img, p.x, p.y, p.want, p.why)
			}
		})
	}
}

// TestFocusRing_OutlineRects pins which boxes a focus ring's outline rects
// come from (PhysicalBoxFragment::AddOutlineRects and its helpers,
// core/layout/physical_box_fragment.cc:1249-1532 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - a block with non-visible overflow adds its own rect but not its
//     children's (:1275-1276), so a grandchild overflowing an overflow:
//     hidden child does not bump the ring;
//   - an anonymous block adds no rect of its own (:1263-1264), only its
//     text items (:1442-1455), so the anonymous wrapper of "XX" overflowing
//     a 10px-tall block contributes the 40px of text, not its full width;
//   - an inline box without a box fragment of its own (culled,
//     LayoutInline::ComputeInitialShouldCreateBoxFragment,
//     core/layout/layout_inline.cc:179-227 @
//     a9f50e522efa9005e6ec765a9a785c74f5c2c86b) leaves its text as the
//     block's line items, so the ring wraps a plain span's overflowing
//     text, while a span with a box fragment (here a transparent border)
//     adds only its non-text descendants, so its text does not count;
//   - a focused inline adds the descendants of every fragment
//     (AddOutlineRectsForInlineBox, :1306-1386), so a relatively positioned
//     inline-block in its second fragment extends the ring.
func TestFocusRing_OutlineRects(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	runRingCases(t, []ringCase{
		{"clipped descendant", `<div style="position:absolute;left:20px;top:20px;width:100px;height:50px;outline:auto 1px blue"><div style="height:50px;overflow:hidden"><div style="width:200px;height:20px;margin-top:10px"></div></div></div>`,
			[]ringProbe{
				{170, 29, black, "no ring along the clipped grandchild's top edge"},
				{120, 45, blue, "the ring runs down the block's right edge"},
			}},
		{"anonymous block", `<div style="position:absolute;left:20px;top:20px;width:100px;height:10px;font:20px/20px Ahem;color:transparent;outline:auto 1px blue">XX<div></div></div>`,
			[]ringProbe{
				{100, 39, black, "no ring along the anonymous wrapper's bottom edge"},
				{100, 29, blue, "the ring runs along the block's own bottom edge right of the text"},
			}},
		{"culled inline's text", `<div style="position:absolute;left:20px;top:20px;width:50px;height:50px;outline:auto 1px blue"><span style="font:50px/1 Ahem;color:transparent">XX</span></div>`,
			[]ringProbe{
				{120, 45, blue, "a culled span's text is a line item of the block, so the ring wraps its overflow"},
			}},
		{"box-fragment inline's text", `<div style="position:absolute;left:20px;top:20px;width:50px;height:50px;outline:auto 1px blue"><span style="font:50px/1 Ahem;color:transparent;border-left:1px solid transparent">XX</span></div>`,
			[]ringProbe{
				{120, 45, black, "text inside an inline box fragment is not a block line item"},
				{69, 45, blue, "the ring runs down the block's right edge"},
			}},
		{"later fragment's descendant", `<div style="position:absolute;left:10px;top:10px;width:120px;font:20px/20px Ahem;color:transparent">XXX <span style="outline:auto 1px blue">XX X<span style="display:inline-block;width:20px;height:40px;position:relative;top:20px"></span></span></div>`,
			[]ringProbe{
				{40, 89, blue, "the ring runs along the second fragment's inline-block bottom edge"},
			}},
	}, 260, 120)
}

// splitInlineOutlinePage is a page whose span #s, outlined with outline,
// splits across two Ahem 20px lines into the fragments (90,10)-(130,30)
// and (10,30)-(90,50), on black.
func splitInlineOutlinePage(outline string) string {
	return `<!doctype html><style>
body { margin: 0; background: black }
#d { position: absolute; left: 10px; top: 10px; width: 120px; font: 20px/20px Ahem; color: transparent }
#s { outline: ` + outline + ` }
</style><div id=d>XXX <span id=s>XX XXXX</span></div>`
}

// TestFocusRing_AnchorOutOfFlowChildren: a focused <a> also adds its
// out-of-flow positioned descendants' rects
// (OutlineType::kIncludeBlockInkOverflowForAnchor, set for an
// HTMLAnchorElement in PhysicalBoxFragment::AddSelfOutlineRects,
// core/layout/physical_box_fragment.cc:1232-1235 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a block anchor its own OOF
// children (:1290-1300), an inline anchor the OOF children of its
// container whose containing block it is (:1372-1386). Any other element
// leaves them out.
func TestFocusRing_AnchorOutOfFlowChildren(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	child := `<span style="position:absolute;left:80px;top:0;width:20px;height:20px"></span>`
	runRingCases(t, []ringCase{
		{"block anchor", `<a href="#" style="position:absolute;left:20px;top:20px;display:block;width:50px;height:20px;outline:auto 1px blue">` + child + `</a>`,
			[]ringProbe{
				{45, 19, blue, "guard: the anchor's own top edge"},
				{110, 19, blue, "the absolute child's top edge"},
			}},
		{"inline anchor", `<div style="position:absolute;left:20px;top:20px;font:20px/20px Ahem;color:transparent">X<a href="#" style="position:relative;outline:auto 1px blue">XX` + child + `</a></div>`,
			[]ringProbe{
				{60, 19, blue, "guard: the anchor's own top edge"},
				{130, 19, blue, "the absolute child, whose containing block the anchor is"},
			}},
		// A static inline with will-change: filter is a containing block too
		// (LayoutObject::ContainerForAbsolutePosition,
		// core/layout/layout_object.cc:1752-1758 @
		// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), not only a positioned
		// one; layout records it (LOU-990).
		{"inline will-change anchor", `<div style="position:absolute;left:20px;top:20px;font:20px/20px Ahem;color:transparent">X<a href="#" style="will-change:filter;outline:auto 1px blue">XX` + child + `</a></div>`,
			[]ringProbe{
				{60, 19, blue, "guard: the anchor's own top edge"},
				{130, 19, blue, "the absolute child, whose containing block the will-change: filter anchor is"},
			}},
		{"block div", `<div style="position:absolute;left:20px;top:20px;width:50px;height:20px;outline:auto 1px blue">` + child + `</div>`,
			[]ringProbe{
				{45, 19, blue, "guard: the div's own top edge"},
				{110, 19, black, "a non-anchor leaves its absolute child out"},
			}},
	}, 200, 80)
}

// TestFocusRing_TransformedDescendant: a descendant with a layer adds its
// rects in its own space, mapped to the focused element's through its
// transform (AddOutlineRectsForDescendant's HasLayer branch,
// core/layout/physical_box_fragment.cc:1499-1510 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; VectorOutlineRectCollector::
// Combine maps each rect through LocalToAncestorRect,
// core/layout/outline_rect_collector.cc:49-61, same SHA), so the ring
// reaches where a translated or scaled child paints, and nested transforms
// compose before one bounding box is taken.
func TestFocusRing_TransformedDescendant(t *testing.T) {
	blue := color.RGBA{0, 0, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	runRingCases(t, []ringCase{
		{"translated child", `<div style="position:absolute;left:20px;top:20px;width:50px;height:20px;outline:auto 1px blue"><div style="width:20px;height:20px;transform:translateX(80px)"></div></div>`,
			[]ringProbe{
				{45, 19, blue, "guard: the block's own top edge"},
				{110, 19, blue, "the translated child's top edge"},
			}},
		{"translated and scaled child", `<div style="position:absolute;left:20px;top:20px;width:50px;height:20px;outline:auto 1px blue"><div style="width:20px;height:20px;transform:translateX(80px) scale(2)"></div></div>`,
			[]ringProbe{
				{125, 9, blue, "the scaled child's top edge, x 90-130 at y 10"},
			}},
		{"nested rotations", `<div style="position:absolute;left:50px;top:50px;width:20px;height:20px;outline:auto 1px blue"><div style="width:20px;height:20px;transform:rotate(45deg)"><div style="width:20px;height:20px;transform:rotate(45deg)"></div></div></div>`,
			[]ringProbe{
				{60, 45, blue, "the 45° child's bounding box top, y 45.86 snapped to 46"},
				{60, 40, black, "the grandchild turns 90° in all, staying 20×20; no 40×40 box of a box"},
			}},
	}, 200, 120)
}
