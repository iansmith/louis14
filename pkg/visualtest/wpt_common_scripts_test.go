package visualtest

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// TestWPTCommonScripts_ReftestWaitRemovedByTakeScreenshot pins that corpus
// documents run WPT's upstream /common/ scripts (served from the wpt-css3 doc
// root, as wptserve serves them): upstream takeScreenshot() removes the root's
// reftest-wait class, so a style keyed on html.reftest-wait must not apply in
// the render. No Blink analog: test-harness data.
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
	img := renderWithBase(t, filepath.Join("testdata", "wpt-css3", "css-transforms"), 100, 100, doc)
	assertPixel(t, img, 20, 20, color.RGBA{0, 128, 0, 255}, "reftest-wait class removed from the root")
}
