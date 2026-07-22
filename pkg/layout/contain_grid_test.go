package layout

import (
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

func TestGridTrackSizing_SizeContainmentZeroesItemContributions(t *testing.T) {
	cases := []struct {
		name           string
		contain        string
		wantMinContent float64
		wantMaxContent float64
	}{
		// Under size containment, auto track = 0 → min/max-content = 0+20+80 = 100.
		{"contain size zeroes auto track", "size", 100, 100},
		{"contain strict zeroes auto track", "strict", 100, 100},
		{"contain inline-size zeroes auto track", "inline-size", 100, 100},
		// Without containment, auto track = item's 500px → min/max-content = 500+20+80 = 600.
		{"no containment keeps item contribution", "", 600, 600},
		// contain:content does NOT include size — items contribute normally.
		{"contain content preserves item contribution", "content", 600, 600},
		// contain:layout does NOT include size — items contribute normally.
		{"contain layout preserves item contribution", "layout", 600, 600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item1 := makeNode("div")
			item2 := makeNode("div")
			item3 := makeNode("div")
			grid := makeNode("div", item1, item2, item3)

			gridStyle := makeStyle(
				"display", "grid",
				"grid-template-columns", "auto 20px 80px",
			)
			if tc.contain != "" {
				gridStyle.Set("contain", tc.contain)
			}
			item1Style := makeStyle("width", "500px")
			item2Style := makeStyle()
			item3Style := makeStyle()

			styles := map[*html.Node]*css.Style{
				grid:  gridStyle,
				item1: item1Style,
				item2: item2Style,
				item3: item3Style,
			}

			ctx := testContext()
			layoutRoot := buildTestTree(grid, styles)
			wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
			space := NewConstraintSpaceBuilder(wdm, wdm, true).
				SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
				Build()

			mm := ComputeMinMaxSizes(ctx, layoutRoot, space)
			if mm.MinContent != tc.wantMinContent {
				t.Errorf("with contain:%q, min-content = %v, want %v",
					tc.contain, mm.MinContent, tc.wantMinContent)
			}
			if mm.MaxContent != tc.wantMaxContent {
				t.Errorf("with contain:%q, max-content = %v, want %v",
					tc.contain, mm.MaxContent, tc.wantMaxContent)
			}
		})
	}
}
