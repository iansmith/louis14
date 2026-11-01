package visualtest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	urlpkg "louis14/pkg/url"
	stdnet "louis14/std/net"
)

// findWPTRoot returns the WPT doc root for a document in dir: dir itself or
// its nearest ancestor named wpt-css2 or wpt-css3. Outside a corpus it falls
// back to filepath.Dir(dir).
//
// This is the harness's one doc_root derivation, the analog of the
// `--doc_root` Blink's web-test runner passes to wptserve
// (third_party/blink/tools/blinkpy/web_tests/servers/wptserve.py:99 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520).
func findWPTRoot(dir string) string {
	for d := dir; d != "." && d != "/"; d = filepath.Dir(d) {
		if base := filepath.Base(d); base == "wpt-css2" || base == "wpt-css3" {
			return d
		}
	}
	return filepath.Dir(dir)
}

// wptMountPoint maps URL paths under urlBase to files under
// <docRoot>/<localDir>: one wptserve alias, the analog of
// RoutesBuilder.add_mount_point(url_base, path)
// (third_party/wpt_tools/wpt/tools/serve/serve.py:925 @
// d4ecdfed88f962439247c2ad36b8fe47805b1520), which get_route_builder
// (serve.py:992) registers for each config "aliases" entry.
type wptMountPoint struct {
	urlBase  string // URL path prefix, with leading and trailing "/"
	localDir string // directory relative to the doc root ("" = the root)
}

// wptMountPoints is the harness's mount table, most specific first; the
// first matching urlBase wins, which is wptserve's "mount points that are
// added later get higher priority" order (RoutesBuilder.get_routes,
// serve.py:907-914 @ d4ecdfed88f962439247c2ad36b8fe47805b1520).
//
// The table contents have no Blink analog: louis14's corpus is flattened.
// Upstream /css/<section>/ is stored at wpt-css3/<section>/ (and
// /css/reference/ at wpt-css3/reference/), so "/css/" maps onto the doc
// root itself, as does "/" (for /fonts/, /images/, ...). Upstream
// /css/support/ is stored at wpt-css3/support/ like every other /css/<x>/,
// so it needs no mount of its own. wpt-css2 references only /fonts/, which
// "/" serves.
var wptMountPoints = []wptMountPoint{
	{urlBase: "/css/", localDir: ""},
	{urlBase: "/", localDir: ""},
}

// wptResourceResolver maps a subresource URL referenced by a WPT document to
// the corpus file that serves it. Every harness fetcher kind (CSS, script,
// @font-face, image, document, external SVG) and the rel=match lookup go
// through it.
type wptResourceResolver struct {
	basePath string // directory of the referencing document
	docRoot  string // findWPTRoot(basePath)
	cfg      WPTServerConfig
}

// newWPTResourceResolver returns the resolver for a document in basePath.
func newWPTResourceResolver(basePath string) wptResourceResolver {
	return wptResourceResolver{
		basePath: basePath,
		docRoot:  findWPTRoot(basePath),
		cfg:      DefaultWPTServerConfig(),
	}
}

// filesystemPath returns the corpus file for uri, the analog of wptserve's
// handlers.filesystem_path (third_party/wpt_tools/wpt/tools/wptserve/
// wptserve/handlers.py:32-50 @ d4ecdfed88f962439247c2ad36b8fe47805b1520):
// strip the mount's url_base and join the rest onto the mount's directory.
//
//   - Only the URL's path names the file: the query and fragment are
//     dropped first (filesystem_path reads request.url_parts.path).
//   - A URL on a configured WPT host is first reduced to its path
//     (stripWPTHost).
//   - A root-relative path is dot-segment-normalised (path.Clean, as KURL
//     canonicalisation does before a request reaches the server:
//     KURL::Init, third_party/blink/renderer/platform/weborigin/kurl.cc:890
//     @ d4ecdfed88f962439247c2ad36b8fe47805b1520), so it cannot climb out
//     of the doc root, then mapped through wptMountPoints.
//   - Any other absolute URL (data:, http(s), "//", file://) is an error:
//     tests do not touch the network.
//   - A relative URL is joined onto basePath (the document's directory).
func (r wptResourceResolver) filesystemPath(uri string) (string, error) {
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	if rewritten, ok := stripWPTHost(uri, r.cfg); ok {
		uri = rewritten
	}
	if strings.HasPrefix(uri, "/") && !strings.HasPrefix(uri, "//") {
		cleaned := path.Clean(uri)
		for _, m := range wptMountPoints {
			if rest, ok := strings.CutPrefix(cleaned, m.urlBase); ok {
				return filepath.Join(r.docRoot, m.localDir, filepath.FromSlash(rest)), nil
			}
		}
	}
	if urlpkg.IsAbsoluteURL(uri) {
		return "", fmt.Errorf("unsupported URI scheme: %s", uri)
	}
	return filepath.Join(r.basePath, uri), nil
}

// fetch is the resource fetcher image and @font-face loads go through:
// data: URLs are static data (stdnet.RequestResource), everything else is
// read from the corpus.
func (r wptResourceResolver) fetch(uri string) ([]byte, error) {
	return stdnet.RequestResource(r.readFile)(uri)
}

// readFile reads the corpus file that serves uri.
func (r wptResourceResolver) readFile(uri string) ([]byte, error) {
	p, err := r.filesystemPath(uri)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}
