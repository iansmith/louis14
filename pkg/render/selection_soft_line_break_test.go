package render_test

import (
	"image/color"
	"testing"

	"louis14/pkg/html"
	"louis14/pkg/render"
)

// softLineBreakPage wraps "XXX XXX" (Ahem, 20px) onto two lines of three
// glyphs; the wrapping space sits at offset 3.
const softLineBreakPage = `<!DOCTYPE html><style>
	body { margin: 0 }
	div { font: 20px/20px Ahem; width: 100px; color: transparent }
	div::selection { background: lime; color: transparent }
</style><div id="d">XXX XXX</div>`

// A selection running past a soft line break widens the line-end text
// fragment's selection rect by the font's space width
// (ExpandedSelectionRectForSoftLineBreakIfNeeded,
// core/layout/inline/inline_cursor.cc:38-54 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), when the selection covers the
// position after the fragment (LayoutSelection::ComputeSelectionStatus,
// core/editing/layout_selection.cc:695-760, same SHA). The last line ends
// where the selection ends, so it is not widened.
func TestSelection_SoftLineBreakWidensLineEndFragment(t *testing.T) {
	img, _ := renderScripted(t, softLineBreakPage+
		`<script>getSelection().selectAllChildren(document.getElementById("d"));</script>`,
		200, 60, render.WithEditingState())
	wantPixel(t, img, 30, 10, inlineLayerLime, "line 1 glyphs are selected")
	wantPixel(t, img, 70, 10, inlineLayerLime, "line 1 is widened by one 20px space past its 60px of glyphs")
	wantPixel(t, img, 85, 10, canvasWhite, "the widening is one space, not to the line end")
	wantPixel(t, img, 30, 30, inlineLayerLime, "line 2 glyphs are selected")
	wantPixel(t, img, 70, 30, canvasWhite, "line 2 ends the selection, so it is not widened")
}

// is_continuous needs the selection end strictly after the fragment's end:
// a selection ending right after line 1's glyphs does not select the soft
// line break; one ending after the wrapping space does.
func TestSelection_SoftLineBreakNeedsSelectionPastFragmentEnd(t *testing.T) {
	for _, tc := range []struct {
		end  int
		want color.RGBA
	}{{3, canvasWhite}, {4, inlineLayerLime}} {
		doc := render.ParsePage(t, softLineBreakPage)
		text := doc.Root.ElementByID("d").Children[0]
		doc.Selection = &html.Range{StartContainer: text, StartOffset: 0, EndContainer: text, EndOffset: tc.end}
		img, _ := render.RenderDocument(t, doc, 200, 60, render.WithEditingState())
		wantPixel(t, img, 30, 10, inlineLayerLime, "line 1 glyphs are selected")
		wantPixel(t, img, 70, 10, tc.want, "space-width widening past line 1")
	}
}

// A block's last line also selects its line break when the selection runs
// on into the next block (ComputeSelectionStatus's kInside/kStart cases);
// only an inline-block's last line never does (IsLastLineInInlineBlock,
// core/layout/inline/inline_cursor.cc:351-365 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestSelection_SoftLineBreakOnBlockLastLineWhenSelectionContinues(t *testing.T) {
	for _, tc := range []struct {
		display string
		want    color.RGBA
	}{{"block", inlineLayerLime}, {"inline-block", canvasWhite}} {
		doc := render.ParsePage(t, softLineBreakPage+`<style>#d { display: `+tc.display+` }</style>
			<p id="p" style="margin: 0; font: 20px/20px Ahem">YYY</p>`)
		d := doc.Root.ElementByID("d")
		doc.Selection = &html.Range{StartContainer: d.Children[0], StartOffset: 0,
			EndContainer: doc.Root.ElementByID("p").Children[0], EndOffset: 1}
		img, _ := render.RenderDocument(t, doc, 200, 80, render.WithEditingState())
		wantPixel(t, img, 30, 30, inlineLayerLime, tc.display+": line 2 glyphs are selected")
		wantPixel(t, img, 70, 30, tc.want, tc.display+": line-break widening past the block's last line")
	}
}
