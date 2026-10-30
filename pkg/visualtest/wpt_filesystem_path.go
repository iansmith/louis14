package visualtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	urlpkg "louis14/pkg/url"
)

// wptResourceResolver maps a subresource URL referenced by a WPT document to
// the corpus file that serves it.
//
// RED stub (LOU-416): unwired, carries the harness's current semantics
// (docRoot = filepath.Dir(basePath), root-relative URLs joined verbatim onto
// it) so the new tests fail for the right reason.
type wptResourceResolver struct {
	basePath string
	docRoot  string
	cfg      WPTServerConfig
}

func newWPTResourceResolver(basePath string) wptResourceResolver {
	return wptResourceResolver{
		basePath: basePath,
		docRoot:  filepath.Dir(basePath),
		cfg:      DefaultWPTServerConfig(),
	}
}

func (r wptResourceResolver) filesystemPath(uri string) (string, error) {
	if rewritten, ok := stripWPTHost(uri, r.cfg); ok {
		uri = rewritten
	}
	if strings.HasPrefix(uri, "/") && !strings.HasPrefix(uri, "//") {
		return filepath.Join(r.docRoot, uri), nil
	}
	if urlpkg.IsAbsoluteURL(uri) {
		return "", fmt.Errorf("unsupported URI scheme: %s", uri)
	}
	return filepath.Join(r.basePath, uri), nil
}

func (r wptResourceResolver) readFile(uri string) ([]byte, error) {
	p, err := r.filesystemPath(uri)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}
