// Package mime mirrors Blink's MIMETypeRegistry
// (third_party/blink/renderer/platform/network/mime/mime_type_registry.cc @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) together with the tables it
// consults: Chromium's well-known extension mappings
// (net/base/mime_util.cc kPrimaryMappings / kSecondaryMappings) and Blink's
// supported-type sets (third_party/blink/common/mime_util/mime_util.cc), all
// at the same SHA.
package mime

import "strings"

// mimeInfo is one extension mapping (net/base/mime_util.cc:91-97 MimeInfo):
// a MIME type and its comma-separated extensions.
type mimeInfo struct {
	mimeType   string
	extensions string
}

// primaryMappings mirrors kPrimaryMappings (net/base/mime_util.cc:169-208).
// Order matters: the first mapping for an extension wins.
var primaryMappings = []mimeInfo{
	{"video/webm", "webm"},
	{"audio/mpeg", "mp3"},
	{"application/wasm", "wasm"},
	{"application/x-chrome-extension", "crx"},
	{"application/xhtml+xml", "xhtml,xht,xhtm"},
	{"audio/flac", "flac"},
	{"audio/matroska", "mka"},
	{"audio/mp3", "mp3"},
	{"audio/ogg", "ogg,oga,opus"},
	{"audio/wav", "wav"},
	{"audio/webm", "webm"},
	{"audio/x-m4a", "m4a"},
	{"image/avif", "avif"},
	{"image/gif", "gif"},
	{"image/jpeg", "jpeg,jpg,jpe"},
	{"image/jxl", "jxl"},
	{"image/png", "png"},
	{"image/apng", "png,apng"},
	{"image/svg+xml", "svg,svgz"},
	{"image/webp", "webp"},
	{"multipart/related", "mht,mhtml"},
	{"text/css", "css"},
	{"text/html", "html,htm,shtml,shtm"},
	{"text/javascript", "js,mjs"},
	{"text/xml", "xml"},
	{"video/matroska", "mkv"},
	{"video/mp4", "mp4,m4v"},
	{"video/ogg", "ogv,ogm"},
	{"text/csv", "csv"},
}

// secondaryMappings mirrors kSecondaryMappings (net/base/mime_util.cc:211-263).
var secondaryMappings = []mimeInfo{
	{"image/x-icon", "ico"},
	{"application/epub+zip", "epub"},
	{"application/font-woff", "woff"},
	{"application/gzip", "gz,tgz"},
	{"application/javascript", "js"},
	{"application/json", "json"},
	{"application/msword", "doc,dot"},
	{"application/octet-stream", "bin,exe,com"},
	{"application/pdf", "pdf"},
	{"application/pkcs7-mime", "p7m,p7c,p7z"},
	{"application/pkcs7-signature", "p7s"},
	{"application/postscript", "ps,eps,ai"},
	{"application/rdf+xml", "rdf"},
	{"application/rss+xml", "rss"},
	{"application/rtf", "rtf"},
	{"application/vnd.android.package-archive", "apk"},
	{"application/vnd.mozilla.xul+xml", "xul"},
	{"application/vnd.ms-excel", "xls"},
	{"application/vnd.ms-powerpoint", "ppt"},
	{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx"},
	{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
	{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"},
	{"application/x-gzip", "gz,tgz"},
	{"application/x-mpegurl", "m3u8"},
	{"application/x-shockwave-flash", "swf,swl"},
	{"application/x-tar", "tar"},
	{"application/x-x509-ca-cert", "cer,crt"},
	{"application/zip", "zip"},
	{"audio/webm", "weba"},
	{"image/bmp", "bmp"},
	{"image/jpeg", "jfif,pjpeg,pjp"},
	{"image/tiff", "tiff,tif"},
	{"image/vnd.microsoft.icon", "ico"},
	{"image/x-png", "png"},
	{"image/x-xbitmap", "xbm"},
	{"message/rfc822", "eml"},
	{"text/calendar", "ics"},
	{"text/html", "ehtml"},
	{"text/markdown", "md"},
	{"text/plain", "txt,text"},
	{"text/vtt", "vtt"},
	{"text/x-sh", "sh"},
	{"text/xml", "xsl,xbl,xslt"},
	{"video/mpeg", "mpeg,mpg,mpe"},
}

// wellKnownMIMETypes maps a lower-case extension to its MIME type: the first
// mapping listing it in primaryMappings, else in secondaryMappings. It is
// the lookup FindMimeType (net/base/mime_util.cc:265-280) performs by
// scanning, ASCII-case-insensitively, precomputed once.
var wellKnownMIMETypes = func() map[string]string {
	m := map[string]string{}
	for _, mappings := range [][]mimeInfo{primaryMappings, secondaryMappings} {
		for _, info := range mappings {
			for _, e := range strings.Split(info.extensions, ",") {
				e = strings.ToLower(strings.TrimSpace(e))
				if _, seen := m[e]; !seen {
					m[e] = info.mimeType
				}
			}
		}
	}
	return m
}()

// GetWellKnownMIMETypeForExtension returns the MIME type of a file extension
// from the hard-coded mappings only (no platform registry), or "".
// Mirrors MIMETypeRegistry::GetWellKnownMIMETypeForExtension
// (mime_type_registry.cc:82-89) → net::GetWellKnownMimeTypeFromExtension
// (net/base/mime_util.cc:316-320 → GetMimeTypeFromExtensionHelper, primary then secondary mappings).
func GetWellKnownMIMETypeForExtension(ext string) string {
	return wellKnownMIMETypes[strings.ToLower(ext)]
}

// supportedImageTypes mirrors kSupportedImageTypes
// (third_party/blink/common/mime_util/mime_util.cc:32-48; image/avif is in
// the set because Chrome ships the dav1d decoder).
var supportedImageTypes = map[string]bool{
	"image/jpeg": true, "image/pjpeg": true, "image/jpg": true, "image/webp": true,
	"image/png": true, "image/apng": true, "image/gif": true, "image/bmp": true,
	"image/vnd.microsoft.icon": true, "image/x-icon": true, "image/x-xbitmap": true,
	"image/x-png": true, "image/avif": true,
}

// IsSupportedImageMIMEType mirrors MIMETypeRegistry::IsSupportedImageMIMEType
// (mime_type_registry.cc:95-97) → blink::IsSupportedImageMimeType
// (mime_util.cc:122-130). image/jxl is behind a disabled-by-default feature
// and is not supported.
func IsSupportedImageMIMEType(mimeType string) bool {
	return supportedImageTypes[strings.ToLower(mimeType)]
}

// supportedNonImageTypes mirrors kSupportedNonImageTypes (mime_util.cc:107-118).
var supportedNonImageTypes = map[string]bool{
	"image/svg+xml": true, "application/xml": true, "application/atom+xml": true,
	"application/rss+xml": true, "application/xhtml+xml": true, "application/json": true,
	"message/rfc822": true, "multipart/related": true, "multipart/x-mixed-replace": true,
}

// supportedJavascriptTypes mirrors kSupportedJavascriptTypes (mime_util.cc:53-71).
var supportedJavascriptTypes = map[string]bool{
	"application/ecmascript": true, "application/javascript": true,
	"application/x-ecmascript": true, "application/x-javascript": true,
	"text/ecmascript": true, "text/javascript": true, "text/javascript1.0": true,
	"text/javascript1.1": true, "text/javascript1.2": true, "text/javascript1.3": true,
	"text/javascript1.4": true, "text/javascript1.5": true, "text/jscript": true,
	"text/livescript": true, "text/x-ecmascript": true, "text/x-javascript": true,
}

// unsupportedTextTypes mirrors kUnsupportedTextTypes (mime_util.cc:76-101).
var unsupportedTextTypes = map[string]bool{
	"text/calendar": true, "text/x-calendar": true, "text/x-vcalendar": true,
	"text/vcalendar": true, "text/vcard": true, "text/x-vcard": true,
	"text/directory": true, "text/ldif": true, "text/qif": true, "text/x-qif": true,
	"text/x-csv": true, "text/x-vcf": true, "text/rtf": true, "text/comma-separated-values": true,
	"text/csv": true, "text/tab-separated-values": true, "text/tsv": true, "text/ofx": true,
	"text/vnd.sun.j2me.app-descriptor": true, "text/x-ms-iqy": true, "text/x-ms-odc": true,
	"text/x-ms-rqy": true, "text/x-ms-contact": true,
}

// IsSupportedNonImageMIMEType mirrors
// MIMETypeRegistry::IsSupportedNonImageMIMEType (mime_type_registry.cc:
// 131-133) → blink::IsSupportedNonImageMimeType (mime_util.cc:132-142).
// louis14 has no media pipeline, so the media::IsSupportedMediaMimeType term
// is not ported: media types are not supported here.
func IsSupportedNonImageMIMEType(mimeType string) bool {
	m := strings.ToLower(mimeType)
	return supportedNonImageTypes[m] || supportedJavascriptTypes[m] ||
		(strings.HasPrefix(m, "text/") && !unsupportedTextTypes[m]) ||
		(strings.HasPrefix(m, "application/") && strings.HasSuffix(m, "+json"))
}
