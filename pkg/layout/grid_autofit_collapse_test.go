package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

func TestGridLayout_AutoFitCollapsesEmptyTracks(t *testing.T) {
	cases := []struct {
		name         string
		containerW   string
		templateCols string
		wantWidth    float64
	}{
		{
			name:         "auto-fit minmax(0,1fr) collapses 99 empty tracks",
			containerW:   "100px",
			templateCols: "repeat(auto-fit, minmax(0, 1fr))",
			wantWidth:    100,
		},
		{
			name:         "auto-fill minmax(0,1fr) does NOT collapse empty tracks",
			containerW:   "100px",
			templateCols: "repeat(auto-fill, minmax(0, 1fr))",
			wantWidth:    1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := makeNode("div")
			grid := makeNode("div", item)

			gridStyle := makeStyle(
				"display", "grid",
				"width", tc.containerW,
				"grid-template-columns", tc.templateCols,
			)
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

			frag := findFragmentByNode(result.Fragment, item)
			if frag == nil {
				t.Fatalf("no item fragment found in layout result")
			}
			gotWidth := frag.Size.WidthF64()
			if gotWidth != tc.wantWidth {
				t.Errorf("item width: got %.1f, want %.1f", gotWidth, tc.wantWidth)
			}
		})
	}
}
