package render

import (
	"image"
	"image/color"
	"testing"

	"louis14/pkg/html"
)

// renderWithCaret renders src after focusing #t, the way HTMLElement.focus()
// does (Document::SetFocusedElement, then Element::UpdateSelectionOnFocus).
// collapseAt, when non-nil, then collapses the selection there, as
// Selection.collapse(node, offset) does.
func renderWithCaret(t *testing.T, src string, focus bool, collapseAt func(t *html.Node) (*html.Node, int)) *image.RGBA {
	t.Helper()
	doc := ParsePage(t, src)
	target := doc.Root.ElementByID("t")
	if target == nil {
		t.Fatal("no #t")
	}
	if focus {
		doc.SetFocusedElement(target)
		doc.UpdateSelectionOnFocus(target)
	}
	if collapseAt != nil {
		n, off := collapseAt(target)
		doc.Selection = &html.Range{StartContainer: n, StartOffset: off, EndContainer: n, EndOffset: off}
	}
	img, _ := RenderDocument(t, doc, 300, 120, WithEditingState())
	return img
}

// TestFrameCaret_Paint checks the caret a focused contenteditable element
// paints at its collapsed selection, as Blink's FrameCaret /
// CaretDisplayItemClient::PaintCaret does (core/editing/
// caret_display_item_client.cc:253-261, :377-398, core/layout/inline/
// caret_rect.cc @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b):
//   - a block caret covers the next character, under the text (box_fragment_
//     painter.cc:755-768); with auto caret-color it is currentColor at alpha
//     0.5;
//   - a bar caret is 1px wide, over the text;
//   - an empty editable block gets a 1ch block caret
//     (LocalCaretRectForEmptyElement);
//   - a caret at the end of text before an atomic inline is 1ch wide;
//   - no caret paints without focus.
//
// Ahem's "p" is blank above its baseline (y < 80 in a 100px font), so the
// caret shows through there; its descent (y >= 80) is ink.
func TestFrameCaret_Paint(t *testing.T) {
	const base = `<!doctype html><style>body{margin:0;background:white}#t{font:100px/1 Ahem;color:black;width:300px;outline:none}</style>`
	lime := color.RGBA{0, 255, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	type px struct {
		x, y int
		want color.RGBA
	}
	cases := []struct {
		name       string
		body       string
		focus      bool
		collapseAt func(t *html.Node) (*html.Node, int)
		pixels     []px
	}{
		{
			name:   "block caret covers the next character, under the text",
			body:   `<div id=t contenteditable style="caret-shape:block;caret-color:lime;caret-animation:manual">pp</div>`,
			focus:  true,
			pixels: []px{{50, 10, lime}, {150, 10, white}, {50, 90, black}},
		},
		{
			name:   "auto caret-color on a block caret is currentColor at alpha 0.5",
			body:   `<div id=t contenteditable style="caret-shape:block;color:blue;caret-animation:manual">pp</div>`,
			focus:  true,
			pixels: []px{{50, 10, color.RGBA{127, 127, 255, 255}}, {150, 10, white}},
		},
		{
			name:   "bar caret is 1px wide, over the text",
			body:   `<div id=t contenteditable style="caret-color:lime;caret-animation:manual">pp</div>`,
			focus:  true,
			pixels: []px{{0, 90, lime}, {0, 10, lime}, {2, 10, white}},
		},
		{
			name:   "empty editable block gets a 1ch block caret",
			body:   `<div id=t contenteditable style="width:100px;caret-shape:block;caret-color:lime;caret-animation:manual"></div>`,
			focus:  true,
			pixels: []px{{50, 50, lime}},
		},
		{
			name:  "caret at the end of text before an atomic inline is 1ch wide",
			body:  `<div id=t contenteditable style="caret-shape:block;caret-color:lime;caret-animation:manual">p<span style="display:inline-block;width:100px;height:100px"></span></div>`,
			focus: true,
			collapseAt: func(t *html.Node) (*html.Node, int) {
				return t, 1
			},
			pixels: []px{{150, 50, lime}, {50, 10, white}},
		},
		{
			name:  "no caret without focus",
			body:  `<div id=t contenteditable style="caret-shape:block;caret-color:lime;caret-animation:manual">pp</div>`,
			focus: false,
			collapseAt: func(t *html.Node) (*html.Node, int) {
				return t, 0
			},
			pixels: []px{{50, 10, white}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := renderWithCaret(t, base+tc.body, tc.focus, tc.collapseAt)
			for _, p := range tc.pixels {
				got := img.RGBAAt(p.x, p.y)
				if !approxColor(got, p.want, 2) {
					t.Errorf("pixel (%d,%d) = %v, want %v", p.x, p.y, got, p.want)
				}
			}
		})
	}
}
