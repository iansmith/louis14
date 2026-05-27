package layout

import (
	"testing"

	"louis14/pkg/css"
)

// TestResolveBorderConflict_StylePrecedence verifies CSS 2.1 §17.6.2.1
// border-style precedence: hidden > double > solid > dashed > dotted >
// ridge > outset > groove > inset > none.
func TestResolveBorderConflict_StylePrecedence(t *testing.T) {
	tests := []struct {
		name string
		a, b borderEdgeInfo
		want borderEdgeInfo
	}{
		{
			name: "hidden beats solid",
			a:    borderEdgeInfo{width: 5, style: "hidden"},
			b:    borderEdgeInfo{width: 9, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 0, style: css.BorderStyleNone},
		},
		{
			name: "double beats solid at equal width",
			a:    borderEdgeInfo{width: 5, style: css.BorderStyleDouble},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 5, style: css.BorderStyleDouble},
		},
		{
			name: "solid beats outset at equal width",
			a:    borderEdgeInfo{width: 5, style: "outset"},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
		},
		{
			name: "solid beats inset at equal width",
			a:    borderEdgeInfo{width: 5, style: "inset"},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
		},
		{
			name: "solid beats ridge at equal width",
			a:    borderEdgeInfo{width: 5, style: "ridge"},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
		},
		{
			name: "solid beats groove at equal width",
			a:    borderEdgeInfo{width: 5, style: "groove"},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
		},
		{
			name: "wider wins regardless of style",
			a:    borderEdgeInfo{width: 9, style: css.BorderStyleDashed},
			b:    borderEdgeInfo{width: 5, style: css.BorderStyleSolid},
			want: borderEdgeInfo{width: 9, style: css.BorderStyleDashed},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveBorderConflict(tc.a, tc.b, true)
			if got.width != tc.want.width || got.style != tc.want.style {
				t.Errorf("resolveBorderConflict(%+v, %+v, true) = %+v, want %+v",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestResolveBorderConflict_StartCellWinsTie verifies CSS 2.1 §17.6.2.1
// Rule 5: when borders are equal on width AND style, the start cell wins.
func TestResolveBorderConflict_StartCellWinsTie(t *testing.T) {
	a := borderEdgeInfo{width: 5, style: css.BorderStyleSolid, color: css.Color{R: 0, G: 128, B: 0, A: 1}}
	b := borderEdgeInfo{width: 5, style: css.BorderStyleSolid, color: css.Color{R: 255, G: 0, B: 0, A: 1}}
	got := resolveBorderConflict(a, b, true)
	if got.color.R != 0 || got.color.G != 128 || got.color.B != 0 {
		t.Errorf("on tie with startCellWins=true: expected green, got %+v", got.color)
	}
}

// TestReadBorderEdge_PreservesNonNormativeStyles checks that readBorderEdge
// preserves outset/inset/ridge/groove/hidden style names rather than
// flattening them all to BorderStyleSolid. This is required for
// CSS 2.1 §17.6.2.1 style-precedence conflict resolution.
func TestReadBorderEdge_PreservesNonNormativeStyles(t *testing.T) {
	cases := []struct {
		propValue string
		wantStyle css.BorderStyle
	}{
		{"hidden", "hidden"},
		{"outset", "outset"},
		{"inset", "inset"},
		{"ridge", "ridge"},
		{"groove", "groove"},
		{"solid", css.BorderStyleSolid},
		{"dashed", css.BorderStyleDashed},
		{"dotted", css.BorderStyleDotted},
		{"double", css.BorderStyleDouble},
		{"none", css.BorderStyleNone},
	}
	for _, tc := range cases {
		t.Run(tc.propValue, func(t *testing.T) {
			s := css.NewStyle()
			s.Set("border-top-style", tc.propValue)
			s.Set("border-top-width", "5px")
			s.Set("border-top-color", "red")
			info := readBorderEdge(s, "top")
			if info.style != tc.wantStyle {
				t.Errorf("readBorderEdge style=%q got %q, want %q",
					tc.propValue, info.style, tc.wantStyle)
			}
		})
	}
}

// TestResolveBorderConflict_SubpixelFloor verifies that border widths
// are floor-compared (integer pixel floor) before precedence is decided.
// Per WPT subpixel-collapsed-borders-001/003: floor(5.95)=5 ties floor(5)=5,
// then start-cell wins.
func TestResolveBorderConflict_SubpixelFloor(t *testing.T) {
	// Subpixel-003 case: table=5.95px solid red vs cell=5px solid green.
	// Floor: 5 vs 5 → tie, cell (a) wins.
	cellEdge := borderEdgeInfo{width: 5, style: css.BorderStyleSolid, color: css.Color{R: 0, G: 128, B: 0, A: 1}}
	tableEdge := borderEdgeInfo{width: 5.95, style: css.BorderStyleSolid, color: css.Color{R: 255, G: 0, B: 0, A: 1}}
	got := resolveBorderConflict(cellEdge, tableEdge, true)
	if got.color.R != 0 || got.color.G != 128 {
		t.Errorf("subpixel-003: floor(5.95)==floor(5)==5 ties, expected cell green wins; got %+v", got.color)
	}

	// Subpixel-001 case: table=5px solid green vs cell=4.95px solid red.
	// Floor: 5 vs 4 → table wins.
	cellEdge2 := borderEdgeInfo{width: 4.95, style: css.BorderStyleSolid, color: css.Color{R: 255, G: 0, B: 0, A: 1}}
	tableEdge2 := borderEdgeInfo{width: 5, style: css.BorderStyleSolid, color: css.Color{R: 0, G: 128, B: 0, A: 1}}
	got2 := resolveBorderConflict(cellEdge2, tableEdge2, true)
	if got2.color.R != 0 || got2.color.G != 128 {
		t.Errorf("subpixel-001: floor(5)=5 > floor(4.95)=4, expected table green wins; got %+v", got2.color)
	}
}

// TestResolveCollapsedBorders_MissingNeighborOutwardExtension is the C56
// regression guard. CSS 2.1 §17.6.3 / CSS Tables 3 §4.2: when a cell has no
// neighbor on one side (cell removed from grid, or table outer edge with no
// element-level border to share), the collapsed border still paints centered
// on the cell-edge grid line — half inside the live cell's border-box, half
// outside it. The live cell's own paint covers only the inside half; the
// outside half must be captured separately so the renderer can paint it as
// an outward extension.
//
// Mirrors Blink's TableBorders/CollapsedTableBordersGeometry behavior
// (table_painters.cc:356-362 @ Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f):
// `block_size = edge.BorderWidth()` regardless of whether a neighbor exists.
// In louis14 the cloned-style map carries the HALF the cell uses for its own
// paint; the outwardExtensions map captures the matching OUTSIDE half that
// the renderer paints as an outward strip.
//
// Setup: 2x2 grid with cell (1,1) removed (mimics the
// collapsed-border-remove-cell.html test after JS removes #target).
//
// Expectations:
//   - cell (0,1).bottom: hole-facing — winning width 1px, outward extension 0.5px down
//   - cell (1,0).right:  hole-facing — winning width 1px, outward extension 0.5px right
//   - cell (0,0) all interior edges (right/bottom): zero outward extension (neighbors exist)
func TestResolveCollapsedBorders_MissingNeighborOutwardExtension(t *testing.T) {
	mkCell := func() *css.Style {
		s := css.NewStyle()
		s.Set("border-top-style", "solid")
		s.Set("border-top-width", "1px")
		s.Set("border-top-color", "black")
		s.Set("border-right-style", "solid")
		s.Set("border-right-width", "1px")
		s.Set("border-right-color", "black")
		s.Set("border-bottom-style", "solid")
		s.Set("border-bottom-width", "1px")
		s.Set("border-bottom-color", "black")
		s.Set("border-left-style", "solid")
		s.Set("border-left-width", "1px")
		s.Set("border-left-color", "black")
		return s
	}

	rows := []tableRow{
		{cells: []tableCell{
			{colIndex: 0, colSpan: 1, rowSpan: 1, style: mkCell()},
			{colIndex: 1, colSpan: 1, rowSpan: 1, style: mkCell()},
		}},
		{cells: []tableCell{
			{colIndex: 0, colSpan: 1, rowSpan: 1, style: mkCell()},
			// (1,1) removed — only one cell in this row.
		}},
	}
	grid := newCellBorderGrid(rows, 2)
	wdm := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	rowStyles := []*css.Style{nil, nil}
	groupStyles := []*css.Style{nil, nil}
	colStyles := []*css.Style{nil, nil}
	colGroupStyles := []*css.Style{nil, nil}
	_, outward := grid.resolveCollapsedBorders(wdm, rowStyles, groupStyles, colStyles, colGroupStyles, nil)

	// Hole-facing edges should have 0.5px outward extension (full winning
	// width is 1px; cell paints 0.5px inside its border-box, the remaining
	// 0.5px must extend outside per §17.6.3).
	if got := outward["0,1"].bottom; got != 0.5 {
		t.Errorf("cell (0,1) bottom outward extension = %v, want 0.5 (hole-facing edge)", got)
	}
	if got := outward["1,0"].right; got != 0.5 {
		t.Errorf("cell (1,0) right outward extension = %v, want 0.5 (hole-facing edge)", got)
	}

	// Interior edges (neighbor exists) should have zero outward extension.
	if got := outward["0,0"].right; got != 0 {
		t.Errorf("cell (0,0) right outward extension = %v, want 0 (neighbor (0,1) exists)", got)
	}
	if got := outward["0,0"].bottom; got != 0 {
		t.Errorf("cell (0,0) bottom outward extension = %v, want 0 (neighbor (1,0) exists)", got)
	}
}

// TestReadBorderEdge_FloorsSubpixelWidth verifies that border widths are
// floored to integer pixels at the readBorderEdge boundary, matching Blink's
// collapsed-border model (table_borders.h BorderWidth → LayoutUnit(int
// BorderTopWidth()) at Chromium SHA 4883d11fef4a8713e32cd582ecef6dc5457c8c3f).
//
// Required by WPT subpixel-collapsed-borders-003 (campaign-3 C27): the test
// renders a single-cell table with cell border 5px green vs table border
// 5.95px red. The reference renders the same shape with cell border 1px red
// vs table border 5.95px green. Both must produce a 62-or-60-wide visual
// box. Without flooring, the reference picks up 5.95 (table wins by
// width), the test picks 5 (cell wins on floor-tie), and the resulting
// cell border-boxes differ by 2 pixels.
func TestReadBorderEdge_FloorsSubpixelWidth(t *testing.T) {
	cases := []struct {
		cssWidth string
		want     float64
	}{
		{"5.95px", 5},
		{"5px", 5},
		{"4.95px", 4},
		{"1px", 1},
		{"0.5px", 0}, // floors to 0; CSS thin-line preservation is a paint-level concern, not collapsed-border conflict input
	}
	for _, tc := range cases {
		t.Run(tc.cssWidth, func(t *testing.T) {
			s := css.NewStyle()
			s.Set("border-top-style", "solid")
			s.Set("border-top-width", tc.cssWidth)
			s.Set("border-top-color", "black")
			got := readBorderEdge(s, "top")
			if got.width != tc.want {
				t.Errorf("readBorderEdge(%q) width = %v, want %v",
					tc.cssWidth, got.width, tc.want)
			}
		})
	}
}
