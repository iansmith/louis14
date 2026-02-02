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

// Phase 7 visual regression tests

func TestVisualRegression_Phase7_DisplayNone(t *testing.T) {
	testCase := visualTestCase{
		name:          "display_none",
		htmlFile:      "../../testdata/phase7/display-none.html",
		referenceFile: "../../testdata/phase7/reference/display-none.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineBlock(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_block",
		htmlFile:      "../../testdata/phase7/inline-block.html",
		referenceFile: "../../testdata/phase7/reference/inline-block.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineWrapping(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_wrapping",
		htmlFile:      "../../testdata/phase7/inline-wrapping.html",
		referenceFile: "../../testdata/phase7/reference/inline-wrapping.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_MixedInlineBlock(t *testing.T) {
	testCase := visualTestCase{
		name:          "mixed_inline_block",
		htmlFile:      "../../testdata/phase7/mixed-inline-block.html",
		referenceFile: "../../testdata/phase7/reference/mixed-inline-block.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineElements(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_elements",
		htmlFile:      "../../testdata/phase7/inline-elements.html",
		referenceFile: "../../testdata/phase7/reference/inline-elements.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineSizes(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_sizes",
		htmlFile:      "../../testdata/phase7/inline-sizes.html",
		referenceFile: "../../testdata/phase7/reference/inline-sizes.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineWithText(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_with_text",
		htmlFile:      "../../testdata/phase7/inline-with-text.html",
		referenceFile: "../../testdata/phase7/reference/inline-with-text.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 7 inline improvements

func TestVisualRegression_Phase7_InlineNoDimensions(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_no_dimensions",
		htmlFile:      "../../testdata/phase7/inline-no-dimensions.html",
		referenceFile: "../../testdata/phase7/reference/inline-no-dimensions.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_VerticalAlign(t *testing.T) {
	testCase := visualTestCase{
		name:          "vertical_align",
		htmlFile:      "../../testdata/phase7/vertical-align.html",
		referenceFile: "../../testdata/phase7/reference/vertical-align.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineTextFlow(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_text_flow",
		htmlFile:      "../../testdata/phase7/inline-text-flow.html",
		referenceFile: "../../testdata/phase7/reference/inline-text-flow.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_LineHeight(t *testing.T) {
	testCase := visualTestCase{
		name:          "line_height",
		htmlFile:      "../../testdata/phase7/line-height.html",
		referenceFile: "../../testdata/phase7/reference/line-height.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase7_InlineMargins(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_margins",
		htmlFile:      "../../testdata/phase7/inline-margins.html",
		referenceFile: "../../testdata/phase7/reference/inline-margins.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 8 visual regression tests

func TestVisualRegression_Phase8_BasicImage(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_image",
		htmlFile:      "../../testdata/phase8/basic-image.html",
		referenceFile: "../../testdata/phase8/reference/basic-image.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase8_ImageDimensions(t *testing.T) {
	testCase := visualTestCase{
		name:          "image_dimensions",
		htmlFile:      "../../testdata/phase8/image-dimensions.html",
		referenceFile: "../../testdata/phase8/reference/image-dimensions.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase8_FloatedImages(t *testing.T) {
	testCase := visualTestCase{
		name:          "floated_images",
		htmlFile:      "../../testdata/phase8/floated-images.html",
		referenceFile: "../../testdata/phase8/reference/floated-images.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase8_InlineImages(t *testing.T) {
	testCase := visualTestCase{
		name:          "inline_images",
		htmlFile:      "../../testdata/phase8/inline-images.html",
		referenceFile: "../../testdata/phase8/reference/inline-images.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase8_ImageBorder(t *testing.T) {
	testCase := visualTestCase{
		name:          "image_border",
		htmlFile:      "../../testdata/phase8/image-border.html",
		referenceFile: "../../testdata/phase8/reference/image-border.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase8_MissingImage(t *testing.T) {
	testCase := visualTestCase{
		name:          "missing_image",
		htmlFile:      "../../testdata/phase8/missing-image.html",
		referenceFile: "../../testdata/phase8/reference/missing-image.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 9: Table Layout Tests

func TestVisualRegression_Phase9_BasicTable(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_table",
		htmlFile:      "../../testdata/phase9/basic-table.html",
		referenceFile: "../../testdata/phase9/reference/basic-table.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase9_TableColspan(t *testing.T) {
	testCase := visualTestCase{
		name:          "table_colspan",
		htmlFile:      "../../testdata/phase9/table-colspan.html",
		referenceFile: "../../testdata/phase9/reference/table-colspan.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase9_TableRowspan(t *testing.T) {
	testCase := visualTestCase{
		name:          "table_rowspan",
		htmlFile:      "../../testdata/phase9/table-rowspan.html",
		referenceFile: "../../testdata/phase9/reference/table-rowspan.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase9_TableBorderCollapse(t *testing.T) {
	testCase := visualTestCase{
		name:          "table_border_collapse",
		htmlFile:      "../../testdata/phase9/table-border-collapse.html",
		referenceFile: "../../testdata/phase9/reference/table-border-collapse.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase9_ComplexTable(t *testing.T) {
	testCase := visualTestCase{
		name:          "complex_table",
		htmlFile:      "../../testdata/phase9/complex-table.html",
		referenceFile: "../../testdata/phase9/reference/complex-table.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase9_StyledTable(t *testing.T) {
	testCase := visualTestCase{
		name:          "styled_table",
		htmlFile:      "../../testdata/phase9/styled-table.html",
		referenceFile: "../../testdata/phase9/reference/styled-table.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 10: Flexbox Layout Tests

func TestVisualRegression_Phase10_FlexRow(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_row",
		htmlFile:      "../../testdata/phase10/flex-row.html",
		referenceFile: "../../testdata/phase10/reference/flex-row.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_FlexColumn(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_column",
		htmlFile:      "../../testdata/phase10/flex-column.html",
		referenceFile: "../../testdata/phase10/reference/flex-column.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_FlexWrap(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_wrap",
		htmlFile:      "../../testdata/phase10/flex-wrap.html",
		referenceFile: "../../testdata/phase10/reference/flex-wrap.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_JustifyContent(t *testing.T) {
	testCase := visualTestCase{
		name:          "justify_content",
		htmlFile:      "../../testdata/phase10/justify-content.html",
		referenceFile: "../../testdata/phase10/reference/justify-content.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_AlignItems(t *testing.T) {
	testCase := visualTestCase{
		name:          "align_items",
		htmlFile:      "../../testdata/phase10/align-items.html",
		referenceFile: "../../testdata/phase10/reference/align-items.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_FlexGrowShrink(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_grow_shrink",
		htmlFile:      "../../testdata/phase10/flex-grow-shrink.html",
		referenceFile: "../../testdata/phase10/reference/flex-grow-shrink.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_NestedFlex(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested_flex",
		htmlFile:      "../../testdata/phase10/nested-flex.html",
		referenceFile: "../../testdata/phase10/reference/nested-flex.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase10_FlexOrder(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_order",
		htmlFile:      "../../testdata/phase10/flex-order.html",
		referenceFile: "../../testdata/phase10/reference/flex-order.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 11: Pseudo-element Tests

func TestVisualRegression_Phase11_BasicBefore(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_before",
		htmlFile:      "../../testdata/phase11/basic-before.html",
		referenceFile: "../../testdata/phase11/reference/basic-before.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase11_BasicAfter(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_after",
		htmlFile:      "../../testdata/phase11/basic-after.html",
		referenceFile: "../../testdata/phase11/reference/basic-after.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase11_BeforeAndAfter(t *testing.T) {
	testCase := visualTestCase{
		name:          "before_and_after",
		htmlFile:      "../../testdata/phase11/before-and-after.html",
		referenceFile: "../../testdata/phase11/reference/before-and-after.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase11_StyledPseudo(t *testing.T) {
	testCase := visualTestCase{
		name:          "styled_pseudo",
		htmlFile:      "../../testdata/phase11/styled-pseudo.html",
		referenceFile: "../../testdata/phase11/reference/styled-pseudo.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase11_QuoteMarks(t *testing.T) {
	testCase := visualTestCase{
		name:          "quote_marks",
		htmlFile:      "../../testdata/phase11/quote-marks.html",
		referenceFile: "../../testdata/phase11/reference/quote-marks.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase11_ListMarkers(t *testing.T) {
	testCase := visualTestCase{
		name:          "list_markers",
		htmlFile:      "../../testdata/phase11/list-markers.html",
		referenceFile: "../../testdata/phase11/reference/list-markers.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 12: Advanced Borders Tests

func TestVisualRegression_Phase12_BorderStyles(t *testing.T) {
	testCase := visualTestCase{
		name:          "border_styles",
		htmlFile:      "../../testdata/phase12/border-styles.html",
		referenceFile: "../../testdata/phase12/reference/border-styles.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase12_BorderDouble(t *testing.T) {
	testCase := visualTestCase{
		name:          "border_double",
		htmlFile:      "../../testdata/phase12/border-double.html",
		referenceFile: "../../testdata/phase12/reference/border-double.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase12_BorderRadius(t *testing.T) {
	testCase := visualTestCase{
		name:          "border_radius",
		htmlFile:      "../../testdata/phase12/border-radius.html",
		referenceFile: "../../testdata/phase12/reference/border-radius.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase12_BorderRadiusWithBorder(t *testing.T) {
	testCase := visualTestCase{
		name:          "border_radius_with_border",
		htmlFile:      "../../testdata/phase12/border-radius-with-border.html",
		referenceFile: "../../testdata/phase12/reference/border-radius-with-border.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase12_MixedBorderStyles(t *testing.T) {
	testCase := visualTestCase{
		name:          "mixed_border_styles",
		htmlFile:      "../../testdata/phase12/mixed-border-styles.html",
		referenceFile: "../../testdata/phase12/reference/mixed-border-styles.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase12_ComplexBorders(t *testing.T) {
	testCase := visualTestCase{
		name:          "complex_borders",
		htmlFile:      "../../testdata/phase12/complex-borders.html",
		referenceFile: "../../testdata/phase12/reference/complex-borders.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// visualTestCase defines a visual regression test
type visualTestCase struct{
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

// Phase 13: Margin Auto Centering Tests

func TestVisualRegression_Phase13_SimpleCenter(t *testing.T) {
	testCase := visualTestCase{
		name:          "simple_center",
		htmlFile:      "../../testdata/phase13/01-simple-center.html",
		referenceFile: "../../testdata/phase13/reference/01-simple-center.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase13_NestedCenters(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested_centers",
		htmlFile:      "../../testdata/phase13/02-nested-centers.html",
		referenceFile: "../../testdata/phase13/reference/02-nested-centers.png",
		width:         900,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase13_MaxWidth(t *testing.T) {
	testCase := visualTestCase{
		name:          "max_width",
		htmlFile:      "../../testdata/phase13/03-max-width.html",
		referenceFile: "../../testdata/phase13/reference/03-max-width.png",
		width:         800,
		height:        300,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase13_HexColors(t *testing.T) {
	testCase := visualTestCase{
		name:          "hex_colors",
		htmlFile:      "../../testdata/phase13/04-hex-colors.html",
		referenceFile: "../../testdata/phase13/reference/04-hex-colors.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase13_WithBorders(t *testing.T) {
	testCase := visualTestCase{
		name:          "with_borders",
		htmlFile:      "../../testdata/phase13/05-with-borders.html",
		referenceFile: "../../testdata/phase13/reference/05-with-borders.png",
		width:         800,
		height:        300,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase13_WebpageLayout(t *testing.T) {
	testCase := visualTestCase{
		name:          "webpage_layout",
		htmlFile:      "../../testdata/phase13/06-webpage-layout.html",
		referenceFile: "../../testdata/phase13/reference/06-webpage-layout.png",
		width:         1200,
		height:        500,
	}

	runVisualTest(t, testCase)
}

// Phase 14: Flexbox Tests

func TestVisualRegression_Phase14_FlexBasic(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_basic",
		htmlFile:      "../../testdata/phase14/01-flex-basic.html",
		referenceFile: "../../testdata/phase14/reference/01-flex-basic.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase14_FlexJustifyCenter(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_justify_center",
		htmlFile:      "../../testdata/phase14/02-flex-justify-center.html",
		referenceFile: "../../testdata/phase14/reference/02-flex-justify-center.png",
		width:         1000,
		height:        300,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase14_FlexJustifyVariations(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_justify_variations",
		htmlFile:      "../../testdata/phase14/03-flex-justify-variations.html",
		referenceFile: "../../testdata/phase14/reference/03-flex-justify-variations.png",
		width:         900,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase14_FlexDirection(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_direction",
		htmlFile:      "../../testdata/phase14/04-flex-direction.html",
		referenceFile: "../../testdata/phase14/reference/04-flex-direction.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase14_FlexAlignItems(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_align_items",
		htmlFile:      "../../testdata/phase14/05-flex-align-items.html",
		referenceFile: "../../testdata/phase14/reference/05-flex-align-items.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase14_FlexNavbar(t *testing.T) {
	testCase := visualTestCase{
		name:          "flex_navbar",
		htmlFile:      "../../testdata/phase14/06-flex-navbar.html",
		referenceFile: "../../testdata/phase14/reference/06-flex-navbar.png",
		width:         1000,
		height:        200,
	}

	runVisualTest(t, testCase)
}

// Phase 15: CSS Grid Tests

func TestVisualRegression_Phase15_GridBasic(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_basic",
		htmlFile:      "../../testdata/phase15/01-grid-basic.html",
		referenceFile: "../../testdata/phase15/reference/01-grid-basic.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase15_GridAutoFill(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_auto_fill",
		htmlFile:      "../../testdata/phase15/02-grid-auto-fill.html",
		referenceFile: "../../testdata/phase15/reference/02-grid-auto-fill.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase15_GridSizing(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_sizing",
		htmlFile:      "../../testdata/phase15/03-grid-sizing.html",
		referenceFile: "../../testdata/phase15/reference/03-grid-sizing.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase15_GridSpan(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_span",
		htmlFile:      "../../testdata/phase15/04-grid-span.html",
		referenceFile: "../../testdata/phase15/reference/04-grid-span.png",
		width:         800,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase15_GridAlignment(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_alignment",
		htmlFile:      "../../testdata/phase15/05-grid-alignment.html",
		referenceFile: "../../testdata/phase15/reference/05-grid-alignment.png",
		width:         800,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase15_GridLayout(t *testing.T) {
	testCase := visualTestCase{
		name:          "grid_layout",
		htmlFile:      "../../testdata/phase15/06-grid-layout.html",
		referenceFile: "../../testdata/phase15/reference/06-grid-layout.png",
		width:         900,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 16: CSS Transform Tests

func TestVisualRegression_Phase16_Translate(t *testing.T) {
	testCase := visualTestCase{
		name:          "translate",
		htmlFile:      "../../testdata/phase16/01-translate.html",
		referenceFile: "../../testdata/phase16/reference/01-translate.png",
		width:         600,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase16_Rotate(t *testing.T) {
	testCase := visualTestCase{
		name:          "rotate",
		htmlFile:      "../../testdata/phase16/02-rotate.html",
		referenceFile: "../../testdata/phase16/reference/02-rotate.png",
		width:         600,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase16_Scale(t *testing.T) {
	testCase := visualTestCase{
		name:          "scale",
		htmlFile:      "../../testdata/phase16/03-scale.html",
		referenceFile: "../../testdata/phase16/reference/03-scale.png",
		width:         700,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase16_CenterWithTransform(t *testing.T) {
	testCase := visualTestCase{
		name:          "center_with_transform",
		htmlFile:      "../../testdata/phase16/04-center-with-transform.html",
		referenceFile: "../../testdata/phase16/reference/04-center-with-transform.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase16_MultipleTransforms(t *testing.T) {
	testCase := visualTestCase{
		name:          "multiple_transforms",
		htmlFile:      "../../testdata/phase16/05-multiple-transforms.html",
		referenceFile: "../../testdata/phase16/reference/05-multiple-transforms.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase16_TransformOrigin(t *testing.T) {
	testCase := visualTestCase{
		name:          "transform_origin",
		htmlFile:      "../../testdata/phase16/06-transform-origin.html",
		referenceFile: "../../testdata/phase16/reference/06-transform-origin.png",
		width:         800,
		height:        500,
	}

	runVisualTest(t, testCase)
}

// Phase 17: Link Styling and Text Decoration Tests

func TestVisualRegression_Phase17_BasicLinks(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_links",
		htmlFile:      "../../testdata/phase17/01-basic-links.html",
		referenceFile: "../../testdata/phase17/reference/01-basic-links.png",
		width:         600,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase17_CustomLinkColors(t *testing.T) {
	testCase := visualTestCase{
		name:          "custom_link_colors",
		htmlFile:      "../../testdata/phase17/02-custom-link-colors.html",
		referenceFile: "../../testdata/phase17/reference/02-custom-link-colors.png",
		width:         600,
		height:        300,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase17_NoUnderline(t *testing.T) {
	testCase := visualTestCase{
		name:          "no_underline",
		htmlFile:      "../../testdata/phase17/03-no-underline.html",
		referenceFile: "../../testdata/phase17/reference/03-no-underline.png",
		width:         700,
		height:        300,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase17_TextDecorations(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_decorations",
		htmlFile:      "../../testdata/phase17/04-text-decorations.html",
		referenceFile: "../../testdata/phase17/reference/04-text-decorations.png",
		width:         600,
		height:        400,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase17_LinksInContext(t *testing.T) {
	testCase := visualTestCase{
		name:          "links_in_context",
		htmlFile:      "../../testdata/phase17/05-links-in-context.html",
		referenceFile: "../../testdata/phase17/reference/05-links-in-context.png",
		width:         800,
		height:        700,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase17_MixedDecorations(t *testing.T) {
	testCase := visualTestCase{
		name:          "mixed_decorations",
		htmlFile:      "../../testdata/phase17/06-mixed-decorations.html",
		referenceFile: "../../testdata/phase17/reference/06-mixed-decorations.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

// Phase 18: Complex CSS Selectors Tests

func TestVisualRegression_Phase18_DescendantSelectors(t *testing.T) {
	testCase := visualTestCase{
		name:          "descendant_selectors",
		htmlFile:      "../../testdata/phase18/01-descendant-selectors.html",
		referenceFile: "../../testdata/phase18/reference/01-descendant-selectors.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase18_ChildCombinators(t *testing.T) {
	testCase := visualTestCase{
		name:          "child_combinators",
		htmlFile:      "../../testdata/phase18/02-child-combinators.html",
		referenceFile: "../../testdata/phase18/reference/02-child-combinators.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase18_SiblingCombinators(t *testing.T) {
	testCase := visualTestCase{
		name:          "sibling_combinators",
		htmlFile:      "../../testdata/phase18/03-sibling-combinators.html",
		referenceFile: "../../testdata/phase18/reference/03-sibling-combinators.png",
		width:         700,
		height:        700,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase18_MultipleClasses(t *testing.T) {
	testCase := visualTestCase{
		name:          "multiple_classes",
		htmlFile:      "../../testdata/phase18/04-multiple-classes.html",
		referenceFile: "../../testdata/phase18/reference/04-multiple-classes.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase18_AttributeSelectors(t *testing.T) {
	testCase := visualTestCase{
		name:          "attribute_selectors",
		htmlFile:      "../../testdata/phase18/05-attribute-selectors.html",
		referenceFile: "../../testdata/phase18/reference/05-attribute-selectors.png",
		width:         700,
		height:        700,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase18_ComplexMixed(t *testing.T) {
	testCase := visualTestCase{
		name:          "complex_mixed",
		htmlFile:      "../../testdata/phase18/06-complex-mixed.html",
		referenceFile: "../../testdata/phase18/reference/06-complex-mixed.png",
		width:         700,
		height:        700,
	}

	runVisualTest(t, testCase)
}

// Phase 19: Visual Effects Tests

func TestVisualRegression_Phase19_BasicBoxShadow(t *testing.T) {
	testCase := visualTestCase{
		name:          "basic_box_shadow",
		htmlFile:      "../../testdata/phase19/01-basic-box-shadow.html",
		referenceFile: "../../testdata/phase19/reference/01-basic-box-shadow.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase19_BoxShadowBlur(t *testing.T) {
	testCase := visualTestCase{
		name:          "box_shadow_blur",
		htmlFile:      "../../testdata/phase19/02-box-shadow-blur.html",
		referenceFile: "../../testdata/phase19/reference/02-box-shadow-blur.png",
		width:         700,
		height:        650,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase19_BoxShadowSpread(t *testing.T) {
	testCase := visualTestCase{
		name:          "box_shadow_spread",
		htmlFile:      "../../testdata/phase19/03-box-shadow-spread.html",
		referenceFile: "../../testdata/phase19/reference/03-box-shadow-spread.png",
		width:         700,
		height:        750,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase19_Opacity(t *testing.T) {
	testCase := visualTestCase{
		name:          "opacity",
		htmlFile:      "../../testdata/phase19/04-opacity.html",
		referenceFile: "../../testdata/phase19/reference/04-opacity.png",
		width:         700,
		height:        700,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase19_RGBAColors(t *testing.T) {
	testCase := visualTestCase{
		name:          "rgba_colors",
		htmlFile:      "../../testdata/phase19/05-rgba-colors.html",
		referenceFile: "../../testdata/phase19/reference/05-rgba-colors.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase19_CombinedEffects(t *testing.T) {
	testCase := visualTestCase{
		name:          "combined_effects",
		htmlFile:      "../../testdata/phase19/06-combined-effects.html",
		referenceFile: "../../testdata/phase19/reference/06-combined-effects.png",
		width:         700,
		height:        750,
	}

	runVisualTest(t, testCase)
}

// Phase 20: Text and Typography Properties Tests

func TestVisualRegression_Phase20_TextTransform(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_transform",
		htmlFile:      "../../testdata/phase20/01-text-transform.html",
		referenceFile: "../../testdata/phase20/reference/01-text-transform.png",
		width:         700,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase20_TextTransformHeadings(t *testing.T) {
	testCase := visualTestCase{
		name:          "text_transform_headings",
		htmlFile:      "../../testdata/phase20/02-text-transform-headings.html",
		referenceFile: "../../testdata/phase20/reference/02-text-transform-headings.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase20_MixedTransforms(t *testing.T) {
	testCase := visualTestCase{
		name:          "mixed_transforms",
		htmlFile:      "../../testdata/phase20/03-mixed-transforms.html",
		referenceFile: "../../testdata/phase20/reference/03-mixed-transforms.png",
		width:         700,
		height:        650,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase20_TransformWithDecoration(t *testing.T) {
	testCase := visualTestCase{
		name:          "transform_with_decoration",
		htmlFile:      "../../testdata/phase20/04-transform-with-decoration.html",
		referenceFile: "../../testdata/phase20/reference/04-transform-with-decoration.png",
		width:         700,
		height:        500,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase20_TypographyShowcase(t *testing.T) {
	testCase := visualTestCase{
		name:          "typography_showcase",
		htmlFile:      "../../testdata/phase20/05-typography-showcase.html",
		referenceFile: "../../testdata/phase20/reference/05-typography-showcase.png",
		width:         700,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase20_RealWorldExample(t *testing.T) {
	testCase := visualTestCase{
		name:          "real_world_example",
		htmlFile:      "../../testdata/phase20/06-real-world-example.html",
		referenceFile: "../../testdata/phase20/reference/06-real-world-example.png",
		width:         900,
		height:        700,
	}

	runVisualTest(t, testCase)
}

// Phase 21: Overflow and Scrolling
func TestVisualRegression_Phase21_OverflowHidden(t *testing.T) {
	testCase := visualTestCase{
		name:          "overflow_hidden",
		htmlFile:      "../../testdata/phase21/01-overflow-hidden.html",
		referenceFile: "../../testdata/phase21/reference/01-overflow-hidden.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase21_OverflowVisible(t *testing.T) {
	testCase := visualTestCase{
		name:          "overflow_visible",
		htmlFile:      "../../testdata/phase21/02-overflow-visible.html",
		referenceFile: "../../testdata/phase21/reference/02-overflow-visible.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase21_OverflowScroll(t *testing.T) {
	testCase := visualTestCase{
		name:          "overflow_scroll",
		htmlFile:      "../../testdata/phase21/03-overflow-scroll.html",
		referenceFile: "../../testdata/phase21/reference/03-overflow-scroll.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase21_OverflowAuto(t *testing.T) {
	testCase := visualTestCase{
		name:          "overflow_auto",
		htmlFile:      "../../testdata/phase21/04-overflow-auto.html",
		referenceFile: "../../testdata/phase21/reference/04-overflow-auto.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase21_NestedOverflow(t *testing.T) {
	testCase := visualTestCase{
		name:          "nested_overflow",
		htmlFile:      "../../testdata/phase21/05-nested-overflow.html",
		referenceFile: "../../testdata/phase21/reference/05-nested-overflow.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}

func TestVisualRegression_Phase21_RealWorldPanel(t *testing.T) {
	testCase := visualTestCase{
		name:          "real_world_panel",
		htmlFile:      "../../testdata/phase21/06-real-world-panel.html",
		referenceFile: "../../testdata/phase21/reference/06-real-world-panel.png",
		width:         800,
		height:        600,
	}

	runVisualTest(t, testCase)
}
