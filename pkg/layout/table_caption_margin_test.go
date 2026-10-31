package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// layoutCaptionTable lays out a row-less table with the given table props and
// one caption per entry of captionProps, in an 800x600 horizontal-tb space.
// Every table gets border:0, padding:0 and border-spacing:0 so the tests pin
// only caption margin placement, not louis14's inside-the-table-border
// caption origin (Blink places captions in the table wrapper, outside the
// table border; louis14 has no wrapper/table-box split).
func layoutCaptionTable(t *testing.T, tableProps []string, captionProps ...[]string) *LayoutResult {
	t.Helper()
	var caps []*html.Node
	styles := map[*html.Node]*css.Style{}
	for _, props := range captionProps {
		c := makeNode("caption")
		styles[c] = makeStyle(append([]string{"display", "table-caption"}, props...)...)
		caps = append(caps, c)
	}
	table := makeNode("table", caps...)
	styles[table] = makeStyle(append([]string{
		"display", "table", "border-width", "0", "padding", "0", "border-spacing", "0",
	}, tableProps...)...)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()
	result := layoutElement(testContext(), buildTestTree(table, styles), space)
	if result == nil || result.Fragment == nil {
		t.Fatal("table layout returned nil")
	}
	if got := len(result.Fragment.Children); got != len(captionProps) {
		t.Fatalf("table has %d children, want %d captions", got, len(captionProps))
	}
	for i, ch := range result.Fragment.Children {
		t.Logf("caption[%d] offset=(%v,%v) size=%vx%v rel=(%v,%v)", i,
			ch.Offset.Left.Float64(), ch.Offset.Top.Float64(),
			ch.Fragment.Size.WidthF64(), ch.Fragment.Size.HeightF64(),
			ch.Fragment.RelativeOffset.Left.Float64(), ch.Fragment.RelativeOffset.Top.Float64())
	}
	return result
}

func assertNear(t *testing.T, what string, got, want float64) {
	t.Helper()
	if !approxEq(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// LOU-434: a caption is placed at its resolved margins. Blink's
// AddCaptionResult (table_layout_algorithm.cc:966-979 @ d4ecdfed88f9) adds
// margins.block_start before the caption, places it at
// LogicalOffset(margins.inline_start, block_offset), and advances by the
// caption block size plus margins.block_end; the table block size is the
// final child_block_offset clamped to >= 0 (:1592 @ d4ecdfed88f9). Top captions are added
// at :989-993 and bottom captions at :1585-1589. The position:relative
// offset is separate and paint-time (ComputeRelativeOffsetForBoxFragment,
// relative_utils.cc:92 @ d4ecdfed88f9).
//
// Caption: width:100px; height:50px; margin-left:200px; margin-top:10px;
// margin-bottom:5px; position:relative; left:-200px (the
// css-tables/caption-relative-positioning.html geometry plus block margins).
func TestTableCaption_Margins_LOU434(t *testing.T) {
	capProps := func(side string) []string {
		return []string{
			"caption-side", side, "position", "relative", "left", "-200px",
			"width", "100px", "height", "50px",
			"margin-left", "200px", "margin-top", "10px", "margin-bottom", "5px",
		}
	}
	table := []string{"width", "300px"}

	for _, side := range []string{"top", "bottom"} {
		t.Run(side, func(t *testing.T) {
			res := layoutCaptionTable(t, table, capProps(side))
			ch := res.Fragment.Children[0]
			assertNear(t, "caption offset.left", ch.Offset.Left.Float64(), 200)
			assertNear(t, "caption offset.top", ch.Offset.Top.Float64(), 10)
			assertNear(t, "caption RelativeOffset.left", ch.Fragment.RelativeOffset.Left.Float64(), -200)
			assertNear(t, "table block size", res.Fragment.Size.HeightF64(), 65)
		})
	}

	t.Run("top+bottom", func(t *testing.T) {
		res := layoutCaptionTable(t, table, capProps("top"), capProps("bottom"))
		top, bottom := res.Fragment.Children[0], res.Fragment.Children[1]
		assertNear(t, "top caption offset.top", top.Offset.Top.Float64(), 10)
		// 10 + 50 + 5 (top caption) + 10 (bottom caption margin-top).
		assertNear(t, "bottom caption offset.top", bottom.Offset.Top.Float64(), 75)
		assertNear(t, "bottom caption offset.left", bottom.Offset.Left.Float64(), 200)
		assertNear(t, "table block size", res.Fragment.Size.HeightF64(), 130)
	})

	t.Run("two-top", func(t *testing.T) {
		second := []string{"caption-side", "top", "width", "100px", "height", "50px", "margin-top", "3px"}
		res := layoutCaptionTable(t, table, capProps("top"), second)
		// 10 + 50 + 5 + 3.
		assertNear(t, "second caption offset.top", res.Fragment.Children[1].Offset.Top.Float64(), 68)
		assertNear(t, "table block size", res.Fragment.Size.HeightF64(), 118)
	})

	t.Run("negative-margin-clamps-block-size", func(t *testing.T) {
		// 0 + 50 - 80 = -30; Blink clamps the final child_block_offset with
		// ClampNegativeToZero (table_layout_algorithm.cc:1592 @ d4ecdfed88f9).
		res := layoutCaptionTable(t, table,
			[]string{"width", "100px", "height", "50px", "margin-bottom", "-80px"})
		assertNear(t, "table block size", res.Fragment.Size.HeightF64(), 0)
	})

	t.Run("percent", func(t *testing.T) {
		// Blink ComputeCaptionMargins (:118-128 @ d4ecdfed88f9) resolves %
		// against the table border-box inline size; at border:0/padding:0 that equals
		// the 300px content box louis14 uses.
		res := layoutCaptionTable(t, table,
			[]string{"width", "100px", "height", "10px", "margin-left", "10%"})
		assertNear(t, "caption offset.left", res.Fragment.Children[0].Offset.Left.Float64(), 30)
	})
}

// LOU-434: auto inline caption margins are resolved after layout against the
// table inline size. Blink LayoutCaption (table_layout_algorithm.cc:92-111,
// ResolveInlineAutoMargins at :107 @ d4ecdfed88f9; length_utils.cc:1551-1570).
func TestTableCaption_AutoInlineMargins_LOU434(t *testing.T) {
	t.Run("both-auto", func(t *testing.T) {
		res := layoutCaptionTable(t, []string{"width", "300px"},
			[]string{"width", "100px", "height", "10px", "margin-left", "auto", "margin-right", "auto"})
		assertNear(t, "caption offset.left", res.Fragment.Children[0].Offset.Left.Float64(), 100)
	})
	t.Run("start-auto", func(t *testing.T) {
		res := layoutCaptionTable(t, []string{"width", "300px"},
			[]string{"width", "100px", "height", "10px", "margin-left", "auto"})
		assertNear(t, "caption offset.left", res.Fragment.Children[0].Offset.Left.Float64(), 200)
	})
	t.Run("auto-width-table", func(t *testing.T) {
		// Blink widens an auto table to at least its caption's min size
		// (ComputeAssignableTableInlineSize :250-251 @ d4ecdfed88f9), so the
		// caption's auto-margin free space is 0. louis14 does not widen the table
		// (ComputeCaptionConstraint is deferred), but resolving against the
		// final table inline size rather than the 800px container still gives
		// Blink's offset: the free space clamps to 0.
		res := layoutCaptionTable(t, nil,
			[]string{"width", "100px", "height", "10px", "margin-left", "auto", "margin-right", "auto"})
		assertNear(t, "caption offset.left", res.Fragment.Children[0].Offset.Left.Float64(), 0)
	})
}

// LOU-434: an auto-width caption stretches to the table inline size minus its
// inline margins. Blink CreateCaptionConstraintSpace sets kStretchImplicit
// (table_layout_algorithm.cc:73 @ d4ecdfed88f9), and the kStretch inline size
// subtracts the box's own margins (length_utils.cc:46-58 @ d4ecdfed88f9).
func TestTableCaption_StretchMinusInlineMargins_LOU434(t *testing.T) {
	res := layoutCaptionTable(t, []string{"width", "300px"},
		[]string{"height", "10px", "margin-left", "30px", "margin-right", "20px"})
	ch := res.Fragment.Children[0]
	assertNear(t, "caption inline size", ch.Fragment.Size.WidthF64(), 250)
	assertNear(t, "caption offset.left", ch.Offset.Left.Float64(), 30)
}

// LOU-434: resolveInlineAutoMargins sets both inline margins as Blink
// ResolveInlineAutoMargins does (length_utils.cc:1551-1570 @ d4ecdfed88f9):
// both-auto splits the free space (clamped at 0) and sets inline_end to the
// remainder; start-auto takes the clamped free space; end-auto sets inline_end
// to the remainder after inline_start.
func TestResolveInlineAutoMargins_LOU434(t *testing.T) {
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	cases := []struct {
		name               string
		props              []string
		available, inline  float64
		in                 LogicalEdges
		wantStart, wantEnd float64
	}{
		{"both-auto", []string{"margin-left", "auto", "margin-right", "auto"}, 300, 100,
			LogicalEdges{}, 100, 100},
		{"start-auto", []string{"margin-left", "auto", "margin-right", "20px"}, 300, 100,
			LogicalEdges{InlineEnd: 20}, 180, 20},
		{"end-auto", []string{"margin-left", "30px", "margin-right", "auto"}, 300, 100,
			LogicalEdges{InlineStart: 30}, 30, 170},
		{"negative-space-clamp", []string{"margin-left", "auto", "margin-right", "auto"}, 300, 400,
			LogicalEdges{}, 0, -100},
		{"no-auto", []string{"margin-left", "30px", "margin-right", "20px"}, 300, 100,
			LogicalEdges{InlineStart: 30, InlineEnd: 20}, 30, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveInlineAutoMargins(makeStyle(tc.props...), wdm, tc.available, tc.inline, tc.in)
			assertNear(t, "InlineStart", got.InlineStart, tc.wantStart)
			assertNear(t, "InlineEnd", got.InlineEnd, tc.wantEnd)
		})
	}
}
