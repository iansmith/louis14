package visualtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Test that nocollapse elements actually prevent margin collapsing
func TestDebugWM013_NocollapseWorks(t *testing.T) {
	basePath := filepath.Join("testdata", "wpt-css3", "css-flexbox")

	// With nocollapse: gap should be 28px (17+11)
	withNocollapse := `<!DOCTYPE html>
<html><head>
<style>
.container { display: block; border: 2px solid purple; padding: 2px; width: 100px; }
.a { display: block; background: red; height: 20px; margin: 11px 0 17px 0; }
.b { display: block; background: blue; height: 20px; margin: 11px 0 17px 0; }
nocollapse { display: block; overflow: hidden; height: 0; }
</style></head><body>
<div class="container">
  <div class="a"></div> <nocollapse></nocollapse>
  <div class="b"></div>
</div>
</body></html>`

	// Without nocollapse: gap should be 17px (max(17,11))
	withoutNocollapse := `<!DOCTYPE html>
<html><head>
<style>
.container { display: block; border: 2px solid purple; padding: 2px; width: 100px; }
.a { display: block; background: red; height: 20px; margin: 11px 0 17px 0; }
.b { display: block; background: blue; height: 20px; margin: 11px 0 17px 0; }
</style></head><body>
<div class="container">
  <div class="a"></div>
  <div class="b"></div>
</div>
</body></html>`

	// Manual expected: with nocollapse
	manualExpected := `<!DOCTYPE html>
<html><head>
<style>
.container { display: block; border: 2px solid purple; padding: 2px; width: 100px; }
.a { display: block; background: red; height: 20px; margin: 11px 0 0 0; }
.gap { display: block; height: 28px; }
.b { display: block; background: blue; height: 20px; margin: 0; }
</style></head><body>
<div class="container">
  <div class="a"></div>
  <div class="gap"></div>
  <div class="b"></div>
</div>
</body></html>`

	tmpDir := t.TempDir()
	outDir := filepath.Join("..", "..", "output", "reftests")
	os.MkdirAll(outDir, 0755)

	// Render with nocollapse
	withNcPNG := filepath.Join(tmpDir, "with_nc.png")
	RenderHTMLToFileWithBase(withNocollapse, withNcPNG, 150, 150, basePath)
	copyFile(withNcPNG, filepath.Join(outDir, "nocollapse_with.png"))

	// Render without nocollapse
	withoutNcPNG := filepath.Join(tmpDir, "without_nc.png")
	RenderHTMLToFileWithBase(withoutNocollapse, withoutNcPNG, 150, 150, basePath)
	copyFile(withoutNcPNG, filepath.Join(outDir, "nocollapse_without.png"))

	// Render manual expected
	manualPNG := filepath.Join(tmpDir, "manual.png")
	RenderHTMLToFileWithBase(manualExpected, manualPNG, 150, 150, basePath)
	copyFile(manualPNG, filepath.Join(outDir, "nocollapse_manual.png"))

	// Compare: with_nc should match manual (28px gap)
	result1, _ := CompareImages(withNcPNG, manualPNG, DefaultOptions())
	pct1 := float64(result1.DifferentPixels) / float64(result1.TotalPixels) * 100
	t.Logf("with_nocollapse vs manual(28px gap): %.1f%% diff", pct1)

	// Compare: without should NOT match manual (margins collapse to 17px)
	result2, _ := CompareImages(withoutNcPNG, manualPNG, DefaultOptions())
	pct2 := float64(result2.DifferentPixels) / float64(result2.TotalPixels) * 100
	t.Logf("without_nocollapse vs manual(28px gap): %.1f%% diff", pct2)

	// Compare: with vs without
	result3, _ := CompareImages(withNcPNG, withoutNcPNG, DefaultOptions())
	pct3 := float64(result3.DifferentPixels) / float64(result3.TotalPixels) * 100
	t.Logf("with vs without nocollapse: %.1f%% diff (should be >0 if nocollapse works)", pct3)

	if pct3 == 0 {
		t.Errorf("nocollapse elements are NOT preventing margin collapse!")
	}
}
