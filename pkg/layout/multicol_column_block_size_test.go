package layout

import "testing"

// LOU-408: a balanced column's block-size rounds up to the next 1/64px, as
// Blink's ContentRun::ColumnBlockSize does with LayoutUnit::FromFloatCeil
// (column_layout_algorithm.cc:1564-1572 @ a9f50e522efa), not to a whole
// pixel: 19.2px of content in two columns gives 615/64 = 9.609375px.
func TestContentRunColumnBlockSize_LOU408(t *testing.T) {
	for _, tc := range []struct {
		content  float64
		implicit int
		want     float64
	}{
		{19.2, 1, 9.609375},
		{100, 1, 50},
		{100, 2, 33.34375},
	} {
		run := contentRun{contentBlockSize: tc.content, implicitBreaksAssumed: tc.implicit}
		if got := run.columnBlockSize(); got != tc.want {
			t.Errorf("columnBlockSize(%v / %d) = %v, want %v", tc.content, tc.implicit+1, got, tc.want)
		}
	}
}
