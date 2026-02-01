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

// Phase 3 visual regression tests

func TestVisualRegression_Phase3_BasicStylesheet(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_stylesheet",
		htmlFile:      "../../testdata/phase3/basic_stylesheet.html",
		referenceFile: "../../testdata/phase3/reference/basic_stylesheet.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase3_ClassSelector(t *testing.T) {
	testCase := visualTestCase{
		name:          "class_selector",
		htmlFile:      "../../testdata/phase3/class_selector.html",
		referenceFile: "../../testdata/phase3/reference/class_selector.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase3_IDSelector(t *testing.T) {
	testCase := visualTestCase{
		name:          "id_selector",
		htmlFile:      "../../testdata/phase3/id_selector.html",
		referenceFile: "../../testdata/phase3/reference/id_selector.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase3_Cascade(t *testing.T) {
	testCase := visualTestCase{
		name:          "cascade",
		htmlFile:      "../../testdata/phase3/cascade.html",
		referenceFile: "../../testdata/phase3/reference/cascade.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 4 visual regression tests

func TestVisualRegression_Phase4_RelativePositioning(t *testing.T) {
	testCase := visualTestCase{
		name:          "relative_positioning",
		htmlFile:      "../../testdata/phase4/relative_positioning.html",
		referenceFile: "../../testdata/phase4/reference/relative_positioning.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase4_AbsolutePositioning(t *testing.T) {
	testCase := visualTestCase{
		name:          "absolute_positioning",
		htmlFile:      "../../testdata/phase4/absolute_positioning.html",
		referenceFile: "../../testdata/phase4/reference/absolute_positioning.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase4_Overlapping(t *testing.T) {
	testCase := visualTestCase{
		name:          "overlapping",
		htmlFile:      "../../testdata/phase4/overlapping.html",
		referenceFile: "../../testdata/phase4/reference/overlapping.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase4_ZIndex(t *testing.T) {
	testCase := visualTestCase{
		name:          "zindex",
		htmlFile:      "../../testdata/phase4/zindex.html",
		referenceFile: "../../testdata/phase4/reference/zindex.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 5 visual regression tests

func TestVisualRegression_Phase5_FloatLeft(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_left",
		htmlFile:      "../../testdata/phase5/float-left.html",
		referenceFile: "../../testdata/phase5/reference/float-left.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_FloatRight(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_right",
		htmlFile:      "../../testdata/phase5/float-right.html",
		referenceFile: "../../testdata/phase5/reference/float-right.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_MultipleFloats(t *testing.T) {
	testCase := visualTestCase{
		name:          "multiple_floats",
		htmlFile:      "../../testdata/phase5/multiple-floats.html",
		referenceFile: "../../testdata/phase5/reference/multiple-floats.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_FloatBothSides(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_both_sides",
		htmlFile:      "../../testdata/phase5/float-both-sides.html",
		referenceFile: "../../testdata/phase5/reference/float-both-sides.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_ClearProperty(t *testing.T) {
	testCase := visualTestCase{
		name:          "clear_property",
		htmlFile:      "../../testdata/phase5/clear-property.html",
		referenceFile: "../../testdata/phase5/reference/clear-property.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_ComplexFloat(t *testing.T) {
	testCase := visualTestCase{
		name:          "complex_float",
		htmlFile:      "../../testdata/phase5/complex-float.html",
		referenceFile: "../../testdata/phase5/reference/complex-float.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 5 enhanced features

func TestVisualRegression_Phase5_FloatShrinkwrap(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_shrinkwrap",
		htmlFile:      "../../testdata/phase5/float-shrinkwrap.html",
		referenceFile: "../../testdata/phase5/reference/float-shrinkwrap.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_FloatDrop(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_drop",
		htmlFile:      "../../testdata/phase5/float-drop.html",
		referenceFile: "../../testdata/phase5/reference/float-drop.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_FloatStackingAdvanced(t *testing.T) {
	testCase := visualTestCase{
		name:          "float_stacking_advanced",
		htmlFile:      "../../testdata/phase5/float-stacking-advanced.html",
		referenceFile: "../../testdata/phase5/reference/float-stacking-advanced.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase5_NestedFloats(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested_floats",
		htmlFile:      "../../testdata/phase5/nested-floats.html",
		referenceFile: "../../testdata/phase5/reference/nested-floats.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 6 visual regression tests

func TestVisualRegression_Phase6_BasicText(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_text",
		htmlFile:      "../../testdata/phase6/basic-text.html",
		referenceFile: "../../testdata/phase6/reference/basic-text.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_TextColors(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_colors",
		htmlFile:      "../../testdata/phase6/text-colors.html",
		referenceFile: "../../testdata/phase6/reference/text-colors.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_TextSizes(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_sizes",
		htmlFile:      "../../testdata/phase6/text-sizes.html",
		referenceFile: "../../testdata/phase6/reference/text-sizes.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_NestedText(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested_text",
		htmlFile:      "../../testdata/phase6/nested-text.html",
		referenceFile: "../../testdata/phase6/reference/nested-text.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_MixedContent(t *testing.T) {
	testCase := visualTestCase{
		name:          "mixed_content",
		htmlFile:      "../../testdata/phase6/mixed-content.html",
		referenceFile: "../../testdata/phase6/reference/mixed-content.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_StyledText(t *testing.T) {
	testCase := visualTestCase{
		name:          "styled_text",
		htmlFile:      "../../testdata/phase6/styled-text.html",
		referenceFile: "../../testdata/phase6/reference/styled-text.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 6 enhanced features

func TestVisualRegression_Phase6_LineBreaking(t *testing.T) {
	testCase := visualTestCase{
		name:          "line_breaking",
		htmlFile:      "../../testdata/phase6/line-breaking.html",
		referenceFile: "../../testdata/phase6/reference/line-breaking.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_TextAlign(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_align",
		htmlFile:      "../../testdata/phase6/text-align.html",
		referenceFile: "../../testdata/phase6/reference/text-align.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_FontWeight(t *testing.T) {
	testCase := visualTestCase{
		name:          "font_weight",
		htmlFile:      "../../testdata/phase6/font-weight.html",
		referenceFile: "../../testdata/phase6/reference/font-weight.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase6_TextWrapFloats(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_wrap_floats",
		htmlFile:      "../../testdata/phase6/text-wrap-floats.html",
		referenceFile: "../../testdata/phase6/reference/text-wrap-floats.png",
		width:         800,
		height:        600,
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
