package mime

import "testing"

// net/base/mime_util.cc kPrimaryMappings / kSecondaryMappings and Blink's
// supported-type sets (third_party/blink/common/mime_util/mime_util.cc) @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b.
func TestGetWellKnownMIMETypeForExtension(t *testing.T) {
	for ext, want := range map[string]string{
		"png": "image/png", // primary image/png precedes image/apng and the secondary image/x-png
		"PNG": "image/png",
		"jpg": "image/jpeg", "svg": "image/svg+xml", "html": "text/html",
		"ico": "image/x-icon", // secondary only; image/x-icon precedes image/vnd.microsoft.icon
		"bmp": "image/bmp", "txt": "text/plain", "xyz": "", "": "",
	} {
		if got := GetWellKnownMIMETypeForExtension(ext); got != want {
			t.Errorf("%q → %q, want %q", ext, got, want)
		}
	}
}

func TestSupportedImageAndNonImageTypes(t *testing.T) {
	for _, m := range []string{"image/png", "IMAGE/PNG", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/x-icon"} {
		if !IsSupportedImageMIMEType(m) {
			t.Errorf("%s should be a supported image type", m)
		}
	}
	// SVG is a non-image (XML) type to Blink.
	for _, m := range []string{"image/svg+xml", "text/html", "text/plain", "application/xhtml+xml", "application/ld+json", "text/javascript"} {
		if IsSupportedImageMIMEType(m) || !IsSupportedNonImageMIMEType(m) {
			t.Errorf("%s should be a supported non-image type", m)
		}
	}
	for _, m := range []string{"text/csv", "text/calendar", "application/pdf", "image/tiff"} {
		if IsSupportedImageMIMEType(m) || IsSupportedNonImageMIMEType(m) {
			t.Errorf("%s should be neither", m)
		}
	}
}
