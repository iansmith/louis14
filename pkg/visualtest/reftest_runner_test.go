package visualtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"louis14/pkg/html"
)

// wptFuzzy holds the parsed WPT fuzzy annotation values.
type wptFuzzy struct {
	MaxDifference int // max allowed per-channel difference
	TotalPixels   int // max allowed number of differing pixels
}

// parseFuzzy extracts the WPT <meta name="fuzzy"> annotation from HTML content.
// Supports both formats:
//
//	content="0-25;0-90"
//	content="maxDifference=0-25;totalPixels=0-90"
//
// Returns nil if no fuzzy annotation is found.
func parseFuzzy(content string) *wptFuzzy {
	// Match <meta name="fuzzy" content="..."> or <meta name='fuzzy' content='...'>
	re := regexp.MustCompile(`(?i)<meta\s+name=["']fuzzy["']\s+content=["']([^"']+)["']`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return nil
	}
	raw := m[1]

	// Split on ";" — first part is maxDifference, second is totalPixels.
	parts := strings.Split(raw, ";")
	if len(parts) != 2 {
		return nil
	}

	parseRange := func(s string) int {
		s = strings.TrimSpace(s)
		// Strip optional label like "maxDifference=" or "totalPixels="
		if idx := strings.Index(s, "="); idx >= 0 {
			s = s[idx+1:]
		}
		// Could be "25" or "0-25" — we want the upper bound.
		if idx := strings.LastIndex(s, "-"); idx > 0 {
			s = s[idx+1:]
		}
		v, _ := strconv.Atoi(strings.TrimSpace(s))
		return v
	}

	return &wptFuzzy{
		MaxDifference: parseRange(parts[0]),
		TotalPixels:   parseRange(parts[1]),
	}
}

// hasWPTFlag reports whether the HTML content has a <meta name="flags"> element
// whose content includes the given flag token (e.g. "dom", "svg", "scroll").
func hasWPTFlag(content, flag string) bool {
	re := regexp.MustCompile(`(?i)<meta\s+name=["']flags["']\s+content=["']([^"']+)["']`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return false
	}
	for _, f := range strings.Fields(m[1]) {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}

// TestWPTReftests runs WPT CSS 2.1 reftests by rendering both test and reference
// HTML files and comparing the resulting images pixel-by-pixel.
func TestWPTReftests(t *testing.T) {
	testDir := filepath.Join("testdata", "wpt-css2")
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("no wpt-css2 testdata directory found")
	}

	// Collect test files that have a <link rel="match">
	var testFiles []string
	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".xht") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "-ref.html") || strings.HasSuffix(base, "-ref.xht") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"reference"+string(filepath.Separator)) {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if findRefLink(string(content)) != "" {
			testFiles = append(testFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk test directory: %v", err)
	}

	if len(testFiles) == 0 {
		t.Skip("no WPT reftest files found with <link rel=\"match\">")
	}

	// Sort test files and move empty-inline-002.xht to the bottom
	sort.Strings(testFiles)
	for i, path := range testFiles {
		if strings.Contains(path, "empty-inline-002.xht") {
			// Move to end
			testFiles = append(append(testFiles[:i], testFiles[i+1:]...), path)
			break
		}
	}

	t.Logf("Found %d WPT reftest files", len(testFiles))

	passed, failed := 0, 0
	for _, testFile := range testFiles {
		relPath, _ := filepath.Rel(testDir, testFile)
		t.Run(relPath, func(t *testing.T) {
			if runReftest(t, testFile) {
				passed++
			} else {
				failed++
			}
		})
	}

	t.Logf("Summary: %d/%d passed, %d failed", passed, len(testFiles), failed)
}

// TestWPTCSS3Reftests runs WPT CSS3 reftests (flexbox, etc.) using the same
// comparison infrastructure as CSS2 tests.
func TestWPTCSS3Reftests(t *testing.T) {
	testDir := filepath.Join("testdata", "wpt-css3")
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("no wpt-css3 testdata directory found")
	}

	var testFiles []string
	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".xht") && !strings.HasSuffix(path, ".htm") && !strings.HasSuffix(path, ".xhtml") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "-ref.html") || strings.HasSuffix(base, "-ref.xht") || strings.HasSuffix(base, "-ref.htm") || strings.HasSuffix(base, "-ref.xhtml") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"reference"+string(filepath.Separator)) {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if findRefLink(string(content)) != "" {
			testFiles = append(testFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk test directory: %v", err)
	}

	if len(testFiles) == 0 {
		t.Skip("no WPT CSS3 reftest files found with <link rel=\"match\">")
	}

	sort.Strings(testFiles)
	t.Logf("Found %d WPT CSS3 reftest files", len(testFiles))

	passed, failed := 0, 0
	for _, testFile := range testFiles {
		relPath, _ := filepath.Rel(testDir, testFile)
		t.Run(relPath, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("REFTEST PANIC: %v", r)
					failed++
				}
			}()
			if runReftest(t, testFile) {
				passed++
			} else {
				failed++
			}
		})
	}

	t.Logf("Summary: %d/%d passed, %d failed", passed, len(testFiles), failed)
}

// runReftest renders a single test file and its reference, then compares.
// Returns true if the test passed.
func runReftest(t *testing.T, testPath string) bool {
	t.Helper()

	content, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
		return false
	}

	refHrefs := findRefLinks(string(content))
	if len(refHrefs) == 0 {
		t.Skip("no <link rel=\"match\"> found")
		return false
	}

	// WPT tests with flags="dom" require JavaScript DOM scripting which our
	// static renderer does not support. Skip them rather than fail incorrectly.
	if hasWPTFlag(string(content), "dom") {
		t.Skip("requires dom scripting (flags=dom)")
		return false
	}

	// WPT tests are designed for 800x600 minimum viewport (standard browser default).
	width, height := 800, 600

	// Render the test file once.
	tmpDir := t.TempDir()
	testPNG := filepath.Join(tmpDir, "test.png")
	testBasePath := filepath.Dir(testPath)
	testWPTRoot := findWPTRoot(testPath)
	testContent := ApplyWPTSubstitutions(string(content), testPath, testWPTRoot)

	if err := RenderHTMLToFileWithBase(testContent, testPNG, width, height, testBasePath); err != nil {
		t.Fatalf("failed to render test: %v", err)
		return false
	}

	// Try each reference. The test passes if it matches ANY reference.
	// This handles WPT tests with multiple <link rel="match"> entries
	// where either rendering is acceptable.
	var lastResult *CompareResult
	for i, refHref := range refHrefs {
		// Resolve reference path relative to test file.
		// WPT absolute paths (starting with "/") are resolved against the WPT root.
		var refPath string
		if strings.HasPrefix(refHref, "/") {
			wptRoot := findWPTRoot(testPath)
			refPath = filepath.Join(wptRoot, refHref[1:])
		} else {
			refPath = filepath.Join(filepath.Dir(testPath), refHref)
		}
		if _, err := os.Stat(refPath); os.IsNotExist(err) {
			continue // Skip missing references, try next
		}

		refContent, err := os.ReadFile(refPath)
		if err != nil {
			continue
		}

		refPNG := filepath.Join(tmpDir, fmt.Sprintf("ref%d.png", i))
		refBasePath := filepath.Dir(refPath)
		refWPTRoot := findWPTRoot(refPath)
		refSubstituted := ApplyWPTSubstitutions(string(refContent), refPath, refWPTRoot)

		if err := RenderHTMLToFileWithBase(refSubstituted, refPNG, width, height, refBasePath); err != nil {
			continue
		}

		opts := DefaultOptions()
		opts.Tolerance = 2
		opts.MaxDifferentPercent = 0
		opts.SaveDiffImage = true
		opts.DiffImagePath = filepath.Join(tmpDir, fmt.Sprintf("diff%d.png", i))

		result, err := CompareImages(testPNG, refPNG, opts)
		if err != nil {
			continue
		}

		lastResult = result

		if result.Match {
			if result.DifferentPixels > 0 {
				pct := float64(result.DifferentPixels) / float64(result.TotalPixels) * 100
				t.Logf("REFTEST PASS (%d pixels, max diff: %d, different: %d / %.1f%%, ref %d/%d)",
					result.TotalPixels, result.MaxDifference, result.DifferentPixels, pct, i+1, len(refHrefs))
			} else {
				t.Logf("REFTEST PASS (%d pixels, max diff: %d)", result.TotalPixels, result.MaxDifference)
			}
			return true
		}

		// Strict comparison failed. If a WPT fuzzy annotation is present,
		// check whether the differences fall within the specified bounds
		// AND all high-diff pixels are on color-transition edges (not in
		// flat interior regions, which would indicate a rendering bug).
		fuzzy := parseFuzzy(string(content))
		if fuzzy != nil && result.MaxDifference <= fuzzy.MaxDifference && result.DifferentPixels <= fuzzy.TotalPixels {
			edgeResult, edgeErr := ValidateEdgeLocality(testPNG, refPNG, 2, 10)
			if edgeErr != nil {
				t.Logf("edge-locality check failed: %v", edgeErr)
				continue
			}
			if edgeResult.InteriorPixels > 0 {
				t.Logf("REFTEST FAIL: fuzzy bounds [%d;%d] met but %d/%d high-diff pixels are in flat regions (not on edges)",
					fuzzy.MaxDifference, fuzzy.TotalPixels,
					edgeResult.InteriorPixels, edgeResult.HighDiffPixels)
				continue
			}
			pct := float64(result.DifferentPixels) / float64(result.TotalPixels) * 100
			t.Logf("REFTEST PASS via fuzzy [%d;%d], edge-validated (%d pixels, max diff: %d, different: %d / %.1f%%, all %d on edges, ref %d/%d)",
				fuzzy.MaxDifference, fuzzy.TotalPixels,
				result.TotalPixels, result.MaxDifference, result.DifferentPixels, pct,
				edgeResult.OnEdgePixels, i+1, len(refHrefs))
			return true
		}
	}

	// None of the references matched.
	if lastResult == nil {
		t.Skipf("no usable reference files found")
		return false
	}

	pct := float64(lastResult.DifferentPixels) / float64(lastResult.TotalPixels) * 100
	t.Errorf("REFTEST FAIL: %d/%d pixels differ (%.1f%%, max diff: %d)",
		lastResult.DifferentPixels, lastResult.TotalPixels, pct, lastResult.MaxDifference)

	// Save images to persistent output directory for debugging
	outputDir := filepath.Join("..", "..", "output", "reftests")
	if err := os.MkdirAll(outputDir, 0755); err == nil {
		baseName := strings.TrimSuffix(filepath.Base(testPath), filepath.Ext(testPath))
		copyFile(testPNG, filepath.Join(outputDir, baseName+"_test.png"))
		// Save the last reference comparison
		lastRefIdx := len(refHrefs) - 1
		lastRefPNG := filepath.Join(tmpDir, fmt.Sprintf("ref%d.png", lastRefIdx))
		lastDiffPNG := filepath.Join(tmpDir, fmt.Sprintf("diff%d.png", lastRefIdx))
		copyFile(lastRefPNG, filepath.Join(outputDir, baseName+"_ref.png"))
		copyFile(lastDiffPNG, filepath.Join(outputDir, baseName+"_diff.png"))
		t.Logf("  saved to output/reftests/%s_*.png", baseName)
	}
	return false
}

// copyFile copies src to dst.
func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.WriteFile(dst, data, 0644)
}

// findWPTRoot returns the wpt-css2 or wpt-css3 ancestor directory of testPath.
// WPT absolute hrefs (starting with "/") are resolved against this root.
func findWPTRoot(testPath string) string {
	dir := filepath.Dir(testPath)
	for dir != "." && dir != "/" {
		base := filepath.Base(dir)
		if base == "wpt-css2" || base == "wpt-css3" {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return filepath.Dir(testPath)
}

// findRefLink extracts the href from <link rel="match" href="..."> in HTML content.
func findRefLink(content string) string {
	// Try parsing with our HTML parser first
	doc, err := html.Parse(content)
	if err == nil {
		if href := findRefLinkInDOM(doc.Root); href != "" {
			return href
		}
	}

	// Fallback: simple string search for <link rel="match" href="...">
	lower := strings.ToLower(content)
	idx := strings.Index(lower, `rel="match"`)
	if idx == -1 {
		idx = strings.Index(lower, `rel='match'`)
	}
	if idx == -1 {
		return ""
	}

	// Find the enclosing tag
	start := strings.LastIndex(lower[:idx], "<")
	if start == -1 {
		return ""
	}
	end := strings.Index(lower[idx:], ">")
	if end == -1 {
		return ""
	}
	tag := content[start : idx+end+1]

	// Extract href value
	for _, prefix := range []string{`href="`, `href='`} {
		hrefIdx := strings.Index(strings.ToLower(tag), prefix)
		if hrefIdx == -1 {
			continue
		}
		quote := tag[hrefIdx+5]
		rest := tag[hrefIdx+6:]
		endQuote := strings.IndexByte(rest, quote)
		if endQuote == -1 {
			continue
		}
		return rest[:endQuote]
	}
	return ""
}

// findRefLinkInDOM walks the DOM tree looking for <link rel="match" href="...">.
func findRefLinkInDOM(node *html.Node) string {
	if node.Type == html.ElementNode && node.TagName == "link" {
		if rel, ok := node.Attributes["rel"]; ok {
			if strings.ToLower(rel) == "match" {
				if href, ok := node.Attributes["href"]; ok {
					return href
				}
			}
		}
	}
	for _, child := range node.Children {
		if href := findRefLinkInDOM(child); href != "" {
			return href
		}
	}
	return ""
}

// findRefLinksInDOM walks the DOM tree collecting ALL <link rel="match" href="..."> hrefs.
// Some WPT tests have multiple match references (the test passes if it matches ANY).
func findRefLinksInDOM(node *html.Node) []string {
	var hrefs []string
	if node.Type == html.ElementNode && node.TagName == "link" {
		if rel, ok := node.Attributes["rel"]; ok {
			if strings.ToLower(rel) == "match" {
				if href, ok := node.Attributes["href"]; ok {
					hrefs = append(hrefs, href)
				}
			}
		}
	}
	for _, child := range node.Children {
		hrefs = append(hrefs, findRefLinksInDOM(child)...)
	}
	return hrefs
}

// findRefLinks extracts all hrefs from <link rel="match" href="..."> in HTML content.
// Returns all match references found. Some tests have multiple references and
// pass if they match ANY of them.
func findRefLinks(content string) []string {
	// Try parsing with our HTML parser first
	doc, err := html.Parse(content)
	if err == nil {
		if hrefs := findRefLinksInDOM(doc.Root); len(hrefs) > 0 {
			return hrefs
		}
	}

	// Fallback: simple string search (finds only the first one, same as before)
	if href := findRefLink(content); href != "" {
		return []string{href}
	}
	return nil
}

// TestListReftestResults provides a quick summary of all reftest results
// without failing. Useful for tracking progress.
func TestListReftestResults(t *testing.T) {
	testDir := filepath.Join("testdata", "wpt-css2")
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("no wpt-css2 testdata directory found")
	}

	var testFiles []string
	filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".xht") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "-ref.html") || strings.HasSuffix(base, "-ref.xht") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"reference"+string(filepath.Separator)) {
			return nil
		}
		content, _ := os.ReadFile(path)
		if findRefLink(string(content)) != "" {
			testFiles = append(testFiles, path)
		}
		return nil
	})

	passed, failed, skipped := 0, 0, 0
	for _, testFile := range testFiles {
		relPath, _ := filepath.Rel(testDir, testFile)
		content, _ := os.ReadFile(testFile)
		refHref := findRefLink(string(content))
		refPath := filepath.Join(filepath.Dir(testFile), refHref)

		if _, err := os.Stat(refPath); os.IsNotExist(err) {
			t.Logf("  SKIP  %s (ref not found)", relPath)
			skipped++
			continue
		}
		refContent, _ := os.ReadFile(refPath)

		tmpDir := t.TempDir()
		testPNG := filepath.Join(tmpDir, "test.png")
		refPNG := filepath.Join(tmpDir, "ref.png")

		testBasePath := filepath.Dir(testFile)
		refBasePath := filepath.Dir(refPath)
		RenderHTMLToFileWithBase(string(content), testPNG, 800, 600, testBasePath)
		RenderHTMLToFileWithBase(string(refContent), refPNG, 800, 600, refBasePath)

		result, err := CompareImages(testPNG, refPNG, DefaultOptions())
		if err != nil {
			t.Logf("  ERR   %s (%v)", relPath, err)
			failed++
			continue
		}

		if result.Match {
			t.Logf("  PASS  %s", relPath)
			passed++
		} else {
			pct := float64(result.DifferentPixels) / float64(result.TotalPixels) * 100
			t.Logf("  FAIL  %s (%d pixels / %.1f%%)", relPath, result.DifferentPixels, pct)
			failed++
		}
	}

	t.Logf("")
	t.Logf("=== REFTEST SUMMARY ===")
	t.Logf("  Total:   %d", len(testFiles))
	t.Logf("  Passed:  %d", passed)
	t.Logf("  Failed:  %d", failed)
	t.Logf("  Skipped: %d", skipped)
	t.Logf("  Pass %%:  %.0f%%", float64(passed)/float64(len(testFiles))*100)

	_ = fmt.Sprintf("placeholder") // use fmt
}

func TestParseFuzzy(t *testing.T) {
	tests := []struct {
		name string
		html string
		want *wptFuzzy
	}{
		{
			name: "shorthand format",
			html: `<meta name="fuzzy" content="0-25;0-90">`,
			want: &wptFuzzy{MaxDifference: 25, TotalPixels: 90},
		},
		{
			name: "named format",
			html: `<meta name="fuzzy" content="maxDifference=0-55;totalPixels=0-299">`,
			want: &wptFuzzy{MaxDifference: 55, TotalPixels: 299},
		},
		{
			name: "named with spaces",
			html: `<meta name="fuzzy" content="maxDifference=0-1; totalPixels=0-4000">`,
			want: &wptFuzzy{MaxDifference: 1, TotalPixels: 4000},
		},
		{
			name: "single value not range",
			html: `<meta name="fuzzy" content="25;90">`,
			want: &wptFuzzy{MaxDifference: 25, TotalPixels: 90},
		},
		{
			name: "no fuzzy annotation",
			html: `<meta name="author" content="test">`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFuzzy(tt.html)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tt.want)
			}
			if got.MaxDifference != tt.want.MaxDifference || got.TotalPixels != tt.want.TotalPixels {
				t.Errorf("got {%d, %d}, want {%d, %d}",
					got.MaxDifference, got.TotalPixels,
					tt.want.MaxDifference, tt.want.TotalPixels)
			}
		})
	}
}
