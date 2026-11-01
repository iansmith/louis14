package layout

import (
	"testing"

	"louis14/pkg/css"
)

// collapsedTopStyle returns a style whose top border is width/style/color.
func collapsedTopStyle(width, style, color string) *css.Style {
	s := css.NewStyle()
	s.Set("border-top-style", style)
	s.Set("border-top-width", width)
	s.Set("border-top-color", color)
	return s
}

// TestIsSourceMoreSpecificThanEdge pins css-tables-3 conflict resolution as
// Blink's IsSourceMoreSpecificThanEdge (table_borders.cc:24-46 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): hidden wins, then the wider
// border, then the later EBorderStyle; an equal source never displaces the
// edge already there, so the earlier (higher-precedence) box keeps it.
func TestIsSourceMoreSpecificThanEdge(t *testing.T) {
	top := TableBordersEdgeSideTop
	edge := func(width, style string) TableBordersEdge {
		return newTableBordersEdge(collapsedTopStyle(width, style, "red"), top, 0)
	}
	tests := []struct {
		name        string
		sourceStyle css.BorderStyle
		sourceWidth float64
		edge        TableBordersEdge
		want        bool
	}{
		{"hidden beats wider solid", css.BorderStyleHidden, 0, edge("9px", "solid"), true},
		{"nothing beats hidden", css.BorderStyleSolid, 9, edge("5px", "hidden"), false},
		{"double beats solid at equal width", css.BorderStyleDouble, 5, edge("5px", "solid"), true},
		{"solid beats outset (groove) at equal width", css.BorderStyleSolid, 5, edge("5px", "outset"), true},
		{"solid beats inset (ridge) at equal width", css.BorderStyleSolid, 5, edge("5px", "inset"), true},
		{"ridge beats groove", "ridge", 5, edge("5px", "groove"), true},
		{"wider wins regardless of style", css.BorderStyleDashed, 9, edge("5px", "double"), true},
		{"narrower loses", css.BorderStyleDouble, 4, edge("5px", "dotted"), false},
		{"equal keeps the earlier edge", css.BorderStyleSolid, 5, edge("5px", "solid"), false},
		{"empty edge takes any source", css.BorderStyleDotted, 1, TableBordersEdge{}, true},
		{"do-not-fill edge takes nothing", css.BorderStyleHidden, 1, TableBordersEdge{EdgeSide: TableBordersEdgeSideDoNotFill}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSourceMoreSpecificThanEdge(tc.sourceStyle, tc.sourceWidth, tc.edge); got != tc.want {
				t.Errorf("isSourceMoreSpecificThanEdge(%q, %v) = %v, want %v", tc.sourceStyle, tc.sourceWidth, got, tc.want)
			}
		})
	}
}

// TestTableBordersBorderStyle_Collapsed checks the collapsing-model style
// mapping (ComputedStyle::CollapsedBorderStyle, computed_style.h:592-603 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): outset reads as groove, inset as
// ridge, and the other keywords keep their identity for precedence.
func TestTableBordersBorderStyle_Collapsed(t *testing.T) {
	cases := map[string]css.BorderStyle{
		"hidden": "hidden", "outset": "groove", "inset": "ridge", "ridge": "ridge",
		"groove": "groove", "solid": "solid", "dashed": "dashed", "dotted": "dotted",
		"double": "double", "none": "none",
	}
	for in, want := range cases {
		if got := TableBordersBorderStyle(collapsedTopStyle("5px", in, "red"), TableBordersEdgeSideTop); got != want {
			t.Errorf("border-top-style %q = %q, want %q", in, got, want)
		}
	}
}

// TestTableBordersBorderWidth_SnapsToWholePixels pins the computed-width
// snapping collapsed-border conflict resolution sees (ClampLineWidth,
// style_builder_converter.cc:1964-1971 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Required by WPT
// subpixel-collapsed-borders-001/003: 5.95px ties 5px.
func TestTableBordersBorderWidth_SnapsToWholePixels(t *testing.T) {
	for in, want := range map[string]float64{"5.95px": 5, "5px": 5, "4.95px": 4, "1px": 1, "0.5px": 1} {
		if got := TableBordersBorderWidth(collapsedTopStyle(in, "solid", "black"), TableBordersEdgeSideTop); got != want {
			t.Errorf("border-top-width %s = %v, want %v", in, got, want)
		}
	}
}

// TestComputeTableBorders_MissingCellEdgesPaintFullWidth is the C56 guard:
// the collapsed edge around a grid hole (cell (1,1) removed) still exists
// and paints its full width centered on the grid line, as Blink's
// TablePainter::PaintCollapsedBorders paints every edge of the TableBorders
// grid whether or not a cell owns it (table_painters.cc:580-727 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestComputeTableBorders_MissingCellEdgesPaintFullWidth(t *testing.T) {
	mkCell := func() *css.Style {
		s := css.NewStyle()
		for _, side := range []string{"top", "right", "bottom", "left"} {
			s.Set("border-"+side+"-style", "solid")
			s.Set("border-"+side+"-width", "1px")
			s.Set("border-"+side+"-color", "black")
		}
		return s
	}
	rows := []tableRow{
		{cells: []tableCell{{colIndex: 0, colSpan: 1, rowSpan: 1, style: mkCell()}, {colIndex: 1, colSpan: 1, rowSpan: 1, style: mkCell()}}},
		{cells: []tableCell{{colIndex: 0, colSpan: 1, rowSpan: 1, style: mkCell()}}},
	}
	wdm := WritingDirectionMode{WM: WritingModeHorizontalTB, Dir: DirectionLTR}
	tb := ComputeTableBorders(rows, nil, nil, wdm, 2)
	epr := tb.EdgesPerRow()
	// Cell (0,1)'s block-end edge: row 1, column 1, row-axis (odd) slot.
	if e := epr*1 + 1*2 + 1; !tb.canPaint(e) || tb.BorderWidth(e) != 1 {
		t.Errorf("edge under cell (0,1): canPaint=%v width=%v, want true 1", tb.canPaint(e), tb.BorderWidth(e))
	}
	// Cell (1,0)'s inline-end edge: row 1, column 1, column-axis (even) slot.
	if e := epr*1 + 1*2; !tb.canPaint(e) || tb.BorderWidth(e) != 1 {
		t.Errorf("edge right of cell (1,0): canPaint=%v width=%v, want true 1", tb.canPaint(e), tb.BorderWidth(e))
	}
	// The hole's own inline-end and block-end edges have no source.
	if e := epr*1 + 2*2; tb.canPaint(e) {
		t.Errorf("edge right of the hole paints; want empty")
	}
}
