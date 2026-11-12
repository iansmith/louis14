package text

import (
	"path/filepath"
	"testing"
)

// TestResolveBuiltinFamily_SansSerifIsLiberationSans pins the sans-serif
// family to the font Chrome renders it with on the wpt.fyi Linux runners.
// Chromium's Linux default sans-serif family is Arial (IDS_SANS_SERIF_
// FONT_FAMILY, chrome/app/resources/locale_settings_linux.grd:169-170 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The runners' image (wpt
// tools/docker/Dockerfile @ a52409560c3b0458e8e684c6b5c595395e613356, run as
// ghcr.io/web-platform-tests/wpt:4 by tools/ci/tc/tasks/test.yml) installs
// fonts-liberation and no Arial, and fontconfig's 30-metric-aliases.conf
// (2.13.1) binds Arial to the metric-compatible Liberation Sans. The
// families louis14 treats as sans-serif aliases resolve the same way.
func TestResolveBuiltinFamily_SansSerifIsLiberationSans(t *testing.T) {
	fc := DefaultFontConfig()
	for _, family := range []string{"sans-serif", "arial", "helvetica", "liberation sans"} {
		for _, tc := range []struct {
			bold, italic bool
			file         string
		}{
			{false, false, "LiberationSans-Regular.ttf"},
			{true, false, "LiberationSans-Bold.ttf"},
			{false, true, "LiberationSans-Italic.ttf"},
			{true, true, "LiberationSans-BoldItalic.ttf"},
		} {
			if got := filepath.Base(fc.resolveBuiltinFamily(family, tc.bold, tc.italic)); got != tc.file {
				t.Errorf("%s bold=%v italic=%v: %s, want %s", family, tc.bold, tc.italic, got, tc.file)
			}
		}
	}
}
