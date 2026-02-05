package visualtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"louis14/pkg/html"
)

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

// runReftest renders a single test file and its reference, then compares.
// Returns true if the test passed.
func runReftest(t *testing.T, testPath string) bool {
	t.Helper()

	content, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
		return false
	}

	refHref := findRefLink(string(content))
	if refHref == "" {
		t.Skip("no <link rel=\"match\"> found")
		return false
	}

	// Resolve reference path relative to test file
	refPath := filepath.Join(filepath.Dir(testPath), refHref)
	if _, err := os.Stat(refPath); os.IsNotExist(err) {
		t.Skipf("reference file not found: %s", refPath)
		return false
	}

	refContent, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("failed to read reference file: %v", err)
		return false
	}

	// Render both to temporary PNGs
	tmpDir := t.TempDir()
	testPNG := filepath.Join(tmpDir, "test.png")
	refPNG := filepath.Join(tmpDir, "ref.png")

	width, height := 400, 400

	// Use the test file's directory as the base path for resolving relative image URLs
	testBasePath := filepath.Dir(testPath)
	refBasePath := filepath.Dir(refPath)

	if err := RenderHTMLToFileWithBase(string(content), testPNG, width, height, testBasePath); err != nil {
		t.Fatalf("failed to render test: %v", err)
		return false
	}

	if err := RenderHTMLToFileWithBase(string(refContent), refPNG, width, height, refBasePath); err != nil {
		t.Fatalf("failed to render reference: %v", err)
		return false
	}

	// Compare
	opts := DefaultOptions()
	opts.Tolerance = 2
	opts.FuzzyRadius = 2          // Allow 2px shift tolerance for table cell kerning differences
	opts.MaxDifferentPercent = 0.3 // Allow up to 0.3% different pixels for font/anti-aliasing variations
	opts.SaveDiffImage = true
	opts.DiffImagePath = filepath.Join(tmpDir, "diff.png")

	result, err := CompareImages(testPNG, refPNG, opts)
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
		return false
	}

	if !result.Match {
		pct := float64(result.DifferentPixels) / float64(result.TotalPixels) * 100
		t.Errorf("REFTEST FAIL: %d/%d pixels differ (%.1f%%, max diff: %d)",
			result.DifferentPixels, result.TotalPixels, pct, result.MaxDifference)

		// Save images to persistent output directory for debugging
		outputDir := filepath.Join("..", "..", "output", "reftests")
		if err := os.MkdirAll(outputDir, 0755); err == nil {
			baseName := strings.TrimSuffix(filepath.Base(testPath), filepath.Ext(testPath))
			copyFile(testPNG, filepath.Join(outputDir, baseName+"_test.png"))
			copyFile(refPNG, filepath.Join(outputDir, baseName+"_ref.png"))
			copyFile(opts.DiffImagePath, filepath.Join(outputDir, baseName+"_diff.png"))
			t.Logf("  saved to output/reftests/%s_*.png", baseName)
		}
		return false
	}

	t.Logf("REFTEST PASS (%d pixels, max diff: %d)", result.TotalPixels, result.MaxDifference)
	return true
}

// copyFile copies src to dst.
func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.WriteFile(dst, data, 0644)
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

		RenderHTMLToFile(string(content), testPNG, 400, 400)
		RenderHTMLToFile(string(refContent), refPNG, 400, 400)

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
