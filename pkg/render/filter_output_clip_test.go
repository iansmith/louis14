package render_test

import (
	"fmt"
	"testing"
)

// TestFilterOutputClip_OwnClipsClipTheOutput — a filtered element's own
// clip-path and CSS clip are its filter's output clip: UpdateClipPathClip and
// UpdateCssClip run before UpdateFilter, whose output_clip is the clip they
// leave current (core/paint/paint_property_tree_builder.cc:4079-4082, 2485 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The drop shadow cast past the
// clip is clipped away.
func TestFilterOutputClip_OwnClipsClipTheOutput(t *testing.T) {
	for _, c := range []string{"clip-path:inset(0)", "position:absolute;clip:rect(0px,100px,100px,0px)"} {
		t.Run(c, func(t *testing.T) {
			page := `<!DOCTYPE html><html><body style="margin:0">` +
				`<div style="` + c + `;width:100px;height:100px;background:blue;filter:drop-shadow(150px 0 0 green)"></div>` +
				`</body></html>`
			img := renderToImage(t, page, 400, 200)
			sampleColorClose(t, img, 50, 50, 0, 0, 255, 0, "content")
			sampleColorClose(t, img, 200, 50, 255, 255, 255, 0, "shadow outside the clip")
		})
	}
}

// TestFilterOutputClip_ExpanderKeepsInput — for a filter that moves pixels,
// descendants paint under PixelMovingFilterClipExpander, a clip that never
// clips paint (paint_property_tree_builder.cc:2512-2532;
// platform/graphics/compositing/paint_chunks_to_cc_layer.cc:414-415 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): an absolute child outside the
// element's CSS clip still feeds the filter, and its shadow lands inside the
// clip.
func TestFilterOutputClip_ExpanderKeepsInput(t *testing.T) {
	page := fmt.Sprintf(`<!DOCTYPE html><html><body style="margin:0">`+
		`<div style="position:absolute;clip:rect(0px,100px,100px,0px);width:100px;height:100px;filter:drop-shadow(%s 0 0 green)">`+
		`<div style="position:absolute;left:150px;top:0;width:50px;height:100px;background:blue"></div>`+
		`</div></body></html>`, "-125px")
	img := renderToImage(t, page, 400, 200)
	// The child (150..200) casts its shadow at 25..75, inside the clip.
	sampleColorClose(t, img, 50, 50, 0, 128, 0, 0, "shadow of the child outside the clip")
	sampleColorClose(t, img, 175, 50, 255, 255, 255, 0, "the child itself is clipped")
}
