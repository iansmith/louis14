package render_test

import "testing"

// TestClipPathInset_ClipsToInsetRect: clip-path: inset(20px 30px 40px 10px)
// on a 100x100 red box keeps only the inset rectangle x∈[10,70), y∈[20,60).
// Mirrors ClipPathClipper painting the BasicShapeInset::GetPath rounded
// rect (core/style/basic_shapes.cc:349-379 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestClipPathInset_ClipsToInsetRect(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:100px;height:100px;background:red;clip-path:inset(20px 30px 40px 10px)"></div>` +
		`</body></html>`
	img := renderToImage(t, src, 200, 200)
	sampleColorClose(t, img, 40, 40, 255, 0, 0, 0, "inside inset rect (40,40)")
	sampleColorClose(t, img, 5, 40, 255, 255, 255, 0, "left of inset (5,40)")
	sampleColorClose(t, img, 80, 40, 255, 255, 255, 0, "right of inset (80,40)")
	sampleColorClose(t, img, 40, 10, 255, 255, 255, 0, "above inset (40,10)")
	sampleColorClose(t, img, 40, 70, 255, 255, 255, 0, "below inset (40,70)")
}

// TestClipPathInset_EmptyRectClipsEverything: insets that meet leave a
// zero-height rect (BasicShapeInset::GetPath clamps the size at 0), so
// nothing of the box paints.
func TestClipPathInset_EmptyRectClipsEverything(t *testing.T) {
	const src = `<!DOCTYPE html><html><body style="margin:0">` +
		`<div style="width:100px;height:100px;background:red;clip-path:inset(60px 0 60px 0)"></div>` +
		`</body></html>`
	img := renderToImage(t, src, 200, 200)
	for _, y := range []int{1, 50, 59, 60, 99} {
		sampleColorClose(t, img, 50, y, 255, 255, 255, 0, "zero-height inset")
	}
}
