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
