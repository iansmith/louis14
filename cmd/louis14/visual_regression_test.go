package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"louis14/pkg/visualtest"
)

// updateReferenceImages is a flag to regenerate reference images
// Set to true when you intentionally change rendering behavior
// Run with: go test -v ./cmd/louis14 -run TestVisual
var updateReferenceImages = os.Getenv("UPDATE_REFS") == "1"

func TestVisualRegression_Phase1_Simple(t *testing.T) {
	testCase := visualTestCase{
		name:          "simple",
		htmlFile:      "../../testdata/phase1/simple.html",
		referenceFile: "../../testdata/phase1/reference/simple.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase1_SingleBox(t *testing.T) {
	testCase := visualTestCase{
		name:          "single_box",
		htmlContent:   `<div style="background-color: red; width: 200px; height: 100px;"></div>`,
		referenceFile: "../../testdata/phase1/reference/single_box.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase1_AllColors(t *testing.T) {
	colors := []string{
		"red", "green", "blue", "yellow", "cyan", "magenta",
		"white", "black", "gray", "orange", "purple", "pink",
	}

	for _, color := range colors {
		t.Run(color, func(t *testing.T) {
			testCase := visualTestCase{
				name:          fmt.Sprintf("color_%s", color),
				htmlContent:   fmt.Sprintf(`<div style="background-color: %s; width: 100px; height: 100px;"></div>`, color),
				referenceFile: fmt.Sprintf("../../testdata/phase1/reference/color_%s.png", color),
				width:         800,
				height:        600,
			}

			runVisualTest(t, testCase)
		})
	}
}

func TestVisualRegression_Phase1_VerticalStacking(t *testing.T) {
	testCase := visualTestCase{
		name: "vertical_stacking",
		htmlContent: `
			<div style="background-color: red; width: 200px; height: 50px;"></div>
			<div style="background-color: blue; width: 200px; height: 50px;"></div>
			<div style="background-color: green; width: 200px; height: 50px;"></div>
		`,
		referenceFile: "../../testdata/phase1/reference/vertical_stacking.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase1_DifferentSizes(t *testing.T) {
	testCase := visualTestCase{
		name: "different_sizes",
		htmlContent: `
			<div style="background-color: red; width: 50px; height: 50px;"></div>
			<div style="background-color: blue; width: 100px; height: 100px;"></div>
			<div style="background-color: green; width: 200px; height: 150px;"></div>
			<div style="background-color: yellow; width: 400px; height: 75px;"></div>
		`,
		referenceFile: "../../testdata/phase1/reference/different_sizes.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase1_EmptyDocument(t *testing.T) {
	testCase := visualTestCase{
		name:          "empty",
		htmlContent:   "",
		referenceFile: "../../testdata/phase1/reference/empty.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 2 visual regression tests

func TestVisualRegression_Phase2_NestedElements(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested",
		htmlFile:      "../../testdata/phase2/nested.html",
		referenceFile: "../../testdata/phase2/reference/nested.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_BoxModel(t *testing.T) {
	testCase := visualTestCase{
		name:          "box_model",
		htmlFile:      "../../testdata/phase2/box_model.html",
		referenceFile: "../../testdata/phase2/reference/box_model.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_ComplexNesting(t *testing.T) {
	testCase := visualTestCase{
		name:          "complex_nesting",
		htmlFile:      "../../testdata/phase2/complex_nesting.html",
		referenceFile: "../../testdata/phase2/reference/complex_nesting.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_Padding(t *testing.T) {
	testCase := visualTestCase{
		name: "padding",
		htmlContent: `<div style="background-color: blue; width: 200px; height: 100px; padding: 30px;"></div>`,
		referenceFile: "../../testdata/phase2/reference/padding.png",
		width:  800,
		height: 600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_Margin(t *testing.T) {
	testCase := visualTestCase{
		name: "margin",
		htmlContent: `<div style="background-color: red; width: 150px; height: 150px; margin: 50px;"></div>`,
		referenceFile: "../../testdata/phase2/reference/margin.png",
		width:  800,
		height: 600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_Border(t *testing.T) {
	testCase := visualTestCase{
		name: "border",
		htmlContent: `<div style="background-color: yellow; width: 200px; height: 100px; border: 10px solid black;"></div>`,
		referenceFile: "../../testdata/phase2/reference/border.png",
		width:  800,
		height: 600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase2_MarginPaddingBorder(t *testing.T) {
	testCase := visualTestCase{
		name: "margin_padding_border",
		htmlContent: `<div style="background-color: green; width: 150px; height: 100px; margin: 20px; padding: 25px; border: 8px solid red;"></div>`,
		referenceFile: "../../testdata/phase2/reference/margin_padding_border.png",
		width:  800,
		height: 600,
	}

	runVisualTest(t, testCase)
}

// visualTestCase defines a visual regression test
type visualTestCase struct {
	name          string
	htmlFile      string // Path to HTML file (optional, use this OR htmlContent)
	htmlContent   string // Inline HTML content (optional, use this OR htmlFile)
	referenceFile string // Path to reference PNG
	width         int
	height        int
}

// runVisualTest executes a visual regression test
func runVisualTest(t *testing.T, tc visualTestCase) {
	t.Helper()

	// Create temp directory for actual output
	tmpDir := t.TempDir()
	actualPath := filepath.Join(tmpDir, "actual.png")

	// Render HTML to PNG
	var err error
	if tc.htmlFile != "" {
		err = visualtest.RenderHTMLFile(tc.htmlFile, actualPath, tc.width, tc.height)
	} else {
		err = visualtest.RenderHTMLToFile(tc.htmlContent, actualPath, tc.width, tc.height)
	}

	if err != nil {
		t.Fatalf("failed to render HTML: %v", err)
	}

	// If updating reference images, generate new reference and skip comparison
	if updateReferenceImages {
		if err := copyFile(actualPath, tc.referenceFile); err != nil {
			t.Fatalf("failed to update reference image: %v", err)
		}
		t.Logf("✓ Updated reference image: %s", tc.referenceFile)
		return
	}

	// Check if reference image exists
	if _, err := os.Stat(tc.referenceFile); os.IsNotExist(err) {
		t.Fatalf("Reference image does not exist: %s\nRun with UPDATE_REFS=1 to generate it", tc.referenceFile)
	}

	// Compare with reference
	opts := visualtest.DefaultOptions()
	opts.SaveDiffImage = true
	opts.DiffImagePath = filepath.Join(tmpDir, "diff.png")

	result, err := visualtest.CompareImages(actualPath, tc.referenceFile, opts)
	if err != nil {
		t.Fatalf("comparison failed: %v", err)
	}

	if !result.Match {
		t.Errorf("Visual regression test failed: %s", tc.name)
		t.Errorf("  Different pixels: %d / %d (%.2f%%)",
			result.DifferentPixels,
			result.TotalPixels,
			100.0*float64(result.DifferentPixels)/float64(result.TotalPixels))
		t.Errorf("  Max difference: %d (tolerance: %d)", result.MaxDifference, opts.Tolerance)
		t.Errorf("  Actual output: %s", actualPath)
		t.Errorf("  Reference: %s", tc.referenceFile)
		t.Errorf("  Diff image: %s", opts.DiffImagePath)
		t.Errorf("\nTo update reference image if this change is intentional:")
		t.Errorf("  UPDATE_REFS=1 go test -v ./cmd/louis14 -run %s", t.Name())
	} else {
		t.Logf("✓ Visual test passed: %s (max diff: %d)", tc.name, result.MaxDifference)
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Write destination
	return os.WriteFile(dst, data, 0644)
}
