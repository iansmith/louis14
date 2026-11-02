package visualtest

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestWPTCommonScripts_ReftestWaitRemovedByTakeScreenshot pins that a corpus
// document loading WPT's shared /common/reftest-wait.js and
// /common/rendering-utils.js runs the upstream scripts: wptserve serves
// /common/ from the doc root, so both resolve to wpt-css3/common/, and
// upstream takeScreenshot() removes the root's `reftest-wait` class
// (common/reftest-wait.js), which is wptrunner's signal to take the
// screenshot. A style keyed on html.reftest-wait therefore must not apply
// in the captured render. No Blink analog: test-harness data.
func TestWPTCommonScripts_ReftestWaitRemovedByTakeScreenshot(t *testing.T) {
	for _, rel := range []string{"common/reftest-wait.js", "common/rendering-utils.js"} {
		if _, err := os.Stat(filepath.Join("testdata", "wpt-css3", rel)); err != nil {
			t.Errorf("/%s not in the corpus: %v", rel, err)
		}
	}

	const doc = `<!doctype html>
<html class="reftest-wait">
<style>
html, body { margin: 0 }
div { width: 40px; height: 40px; background: rgb(0, 128, 0) }
html.reftest-wait div { background: rgb(255, 0, 0) }
</style>
<script src="/common/reftest-wait.js"></script>
<script src="/common/rendering-utils.js"></script>
<div></div>
<script>
waitForAtLeastOneFrame().then(takeScreenshot);
</script>
</html>`
	// Any directory inside the wpt-css3 corpus gives the wpt-css3 doc root.
	base := filepath.Join("testdata", "wpt-css3", "css-transforms")
	out := filepath.Join(t.TempDir(), "out.png")
	if err := RenderHTMLToFileWithBase(doc, out, 100, 100, base); err != nil {
		t.Fatalf("render: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(20, 20).RGBA()
	got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	if want := (color.RGBA{0, 128, 0, 255}); got != want {
		t.Errorf("pixel (20,20) = %v, want %v (reftest-wait class still on the root)", got, want)
	}
}
