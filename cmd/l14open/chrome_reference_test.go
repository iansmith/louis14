package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"louis14/pkg/text"
	"louis14/pkg/visualtest"
)

// The visual-regression references are Chrome screenshots, not louis14
// output. UPDATE_REFS=1 recaptures them with captureChromeReference.
//
// Chrome draws each page with louis14's own font files: a copy of the page
// gets @font-face rules that bind the system family names Chrome uses for its
// default fonts (Times, Helvetica, Courier on macOS) and their common aliases
// to the files louis14 resolves for them. Glyph shapes then agree, so a
// mismatch is a layout or paint difference rather than a font choice. The
// test pages set no font-family, so the defaults are all that matter.

// chromePath is the Chrome binary used for capture; CHROME overrides it.
func chromePath() string {
	if p := os.Getenv("CHROME"); p != "" {
		return p
	}
	return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}

// chromeVersionFile records which Chrome produced the references.
const chromeVersionFile = "../../testdata/chrome-references-version.txt"

// chromeFontFamilies are the family names given @font-face rules, each bound
// to the face louis14's default FontConfig resolves for that name.
var chromeFontFamilies = []string{
	"Times", "Times New Roman", "Liberation Serif",
	"Helvetica", "Helvetica Neue", "Arial", "Liberation Sans", "Nimbus Sans",
	"Courier", "Courier New", "Liberation Mono",
}

var doctypeRe = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)

// fontFaceStyle returns a <style> element declaring the regular, bold, italic
// and bold-italic face of every chromeFontFamilies name.
func fontFaceStyle() string {
	fc := text.DefaultFontConfig()
	var b strings.Builder
	b.WriteString("<style>")
	for _, name := range chromeFontFamilies {
		for _, bold := range []bool{false, true} {
			for _, italic := range []bool{false, true} {
				weight, style := "normal", "normal"
				if bold {
					weight = "bold"
				}
				if italic {
					style = "italic"
				}
				fmt.Fprintf(&b, "@font-face{font-family:%q;font-weight:%s;font-style:%s;src:url(\"file://%s\")}\n",
					name, weight, style, fc.FontPathForFamily(name, bold, italic, false, false))
			}
		}
	}
	b.WriteString("</style>")
	return b.String()
}

// chromeCapturePage returns the page Chrome screenshots: html with the font
// faces injected and, when baseDir is set, a <base> so relative URLs resolve
// against the original document's directory.
func chromeCapturePage(html, baseDir string) string {
	var head strings.Builder
	rest := html
	if loc := doctypeRe.FindStringIndex(html); loc != nil {
		head.WriteString(html[:loc[1]])
		rest = html[loc[1]:]
	}
	if baseDir != "" {
		fmt.Fprintf(&head, "<base href=\"file://%s/\">", baseDir)
	}
	head.WriteString(fontFaceStyle())
	return head.String() + rest
}

// captureChromeReference screenshots tc in headless Chrome at 1x scale and
// tc's viewport, writing tc.referenceFile.
func captureChromeReference(t *testing.T, tc visualTestCase) error {
	t.Helper()
	html, baseDir := tc.htmlContent, ""
	if tc.htmlFile != "" {
		data, err := os.ReadFile(tc.htmlFile)
		if err != nil {
			return err
		}
		html = string(data)
		if baseDir, err = filepath.Abs(filepath.Dir(tc.htmlFile)); err != nil {
			return err
		}
	}

	tmp := t.TempDir()
	page := filepath.Join(tmp, "page.html")
	if err := os.WriteFile(page, []byte(chromeCapturePage(html, baseDir)), 0o644); err != nil {
		return err
	}
	ref, err := filepath.Abs(tc.referenceFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
		return err
	}
	cmd := exec.Command(chromePath(),
		"--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--force-device-scale-factor=1", "--allow-file-access-from-files",
		fmt.Sprintf("--window-size=%d,%d", tc.width, tc.height),
		"--user-data-dir="+filepath.Join(tmp, "profile"),
		"--screenshot="+ref,
		"file://"+page)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chrome: %v\n%s", err, out)
	}
	return recordChromeVersion()
}

var recordChromeVersion = sync.OnceValue(func() error {
	out, err := exec.Command(chromePath(), "--version").Output()
	if err != nil {
		return fmt.Errorf("chrome --version: %w", err)
	}
	return os.WriteFile(chromeVersionFile, out, 0o644)
})

// expectedFailuresFile lists the visual tests whose louis14 render is known
// not to match Chrome, in visualtest.LoadExpectedFailures format keyed by test
// name.
const expectedFailuresFile = "expected_failures.txt"

var (
	expectedFailures map[string]string
	consulted        = map[string]bool{}
)

// isExpectedFailure reports whether t is listed in expectedFailuresFile and
// records that the entry is still in use.
func isExpectedFailure(t *testing.T) bool {
	consulted[t.Name()] = true
	_, ok := expectedFailures[t.Name()]
	return ok
}

// TestMain loads expectedFailuresFile, and fails a full run whose list names a
// test that no longer exists, so stale entries can't accumulate.
func TestMain(m *testing.M) {
	var err error
	if expectedFailures, err = visualtest.LoadExpectedFailures(expectedFailuresFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	filtered := flag.Lookup("test.run").Value.String() != "" || flag.Lookup("test.skip").Value.String() != ""
	if code == 0 && !updateReferenceImages && !filtered {
		for name := range expectedFailures {
			if !consulted[name] {
				fmt.Fprintf(os.Stderr, "%s lists %s, which did not run\n", expectedFailuresFile, name)
				code = 1
			}
		}
	}
	os.Exit(code)
}
