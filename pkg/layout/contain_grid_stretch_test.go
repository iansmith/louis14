package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

func TestGridLayout_AutoTrackStretchUnderContainment(t *testing.T) {
	cases := []struct {
		name          string
		contain       string
		containerW    string
		templateCols  string
		wantAutoWidth float64
	}{
		{
			name:          "contain size with definite width stretches auto column",
			contain:       "size",
			containerW:    "300px",
			templateCols:  "auto 100px",
			wantAutoWidth: 200,
		},
		{
			name:          "contain strict with definite width stretches auto column",
			contain:       "strict",
			containerW:    "300px",
			templateCols:  "auto 100px",
			wantAutoWidth: 200,
		},
		{
			name:          "contain inline-size with definite width stretches auto column",
			contain:       "inline-size",
			containerW:    "300px",
			templateCols:  "auto 100px",
			wantAutoWidth: 200,
		},
		{
			name:          "no containment with definite width stretches auto column",
			contain:       "",
			containerW:    "300px",
			templateCols:  "auto 100px",
			wantAutoWidth: 200,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item1 := makeNode("div")
			item2 := makeNode("div")
			grid := makeNode("div", item1, item2)

			gridStyle := makeStyle(
				"display", "grid",
				"width", tc.containerW,
				"grid-template-columns", tc.templateCols,
			)
			if tc.contain != "" {
				gridStyle.Set("contain", tc.contain)
			}
			item1Style := makeStyle()
			item2Style := makeStyle()

			styles := map[*html.Node]*css.Style{
				grid:  gridStyle,
				item1: item1Style,
				item2: item2Style,
			}

			ctx := testContext()
			layoutRoot := buildTestTree(grid, styles)
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				Build()

			result := layoutElement(ctx, layoutRoot, space)

			item1Frag := findFragmentByNode(result.Fragment, item1)
			if item1Frag == nil {
				t.Fatalf("no item1 fragment found in layout result")
			}
			gotWidth := item1Frag.Size.WidthF64()
			if gotWidth != tc.wantAutoWidth {
				t.Errorf("auto-column width: got %.1f, want %.1f (auto track should stretch to fill free space even under contain:%q)",
					gotWidth, tc.wantAutoWidth, tc.contain)
			}
		})
	}
}

func TestGridLayout_AutoRowStretchUnderContainment(t *testing.T) {
	cases := []struct {
		name           string
		contain        string
		height         string
		templateRows   string
		wantItemHeight float64
	}{
		{
			name:           "contain size with explicit height stretches item in auto row",
			contain:        "size",
			height:         "100px",
			templateRows:   "auto",
			wantItemHeight: 100,
		},
		{
			name:           "contain inline-size with explicit height stretches item in auto row",
			contain:        "inline-size",
			height:         "100px",
			templateRows:   "auto",
			wantItemHeight: 100,
		},
		{
			name:           "no containment with explicit height stretches item in auto row",
			contain:        "",
			height:         "100px",
			templateRows:   "auto",
			wantItemHeight: 100,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := makeNode("div")
			grid := makeNode("div", item)

			gridStyle := makeStyle(
				"display", "grid",
				"width", "100px",
				"height", tc.height,
				"grid-template-rows", tc.templateRows,
			)
			if tc.contain != "" {
				gridStyle.Set("contain", tc.contain)
			}
			itemStyle := makeStyle()

			styles := map[*html.Node]*css.Style{
				grid: gridStyle,
				item: itemStyle,
			}

			ctx := testContext()
			layoutRoot := buildTestTree(grid, styles)
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				Build()

			result := layoutElement(ctx, layoutRoot, space)

			itemFrag := findFragmentByNode(result.Fragment, item)
			if itemFrag == nil {
				t.Fatalf("no item fragment found in layout result")
			}
			gotHeight := itemFrag.Size.HeightF64()
			if gotHeight != tc.wantItemHeight {
				t.Errorf("item height in auto row: got %.1f, want %.1f (item should stretch to fill auto row track under contain:%q with explicit height)",
					gotHeight, tc.wantItemHeight, tc.contain)
			}
		})
	}
}
