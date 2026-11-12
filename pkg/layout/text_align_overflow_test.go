package layout

import (
	"fmt"
	"testing"
)

// TestLineOffsetForTextAlign_OverflowingLine checks where text-align puts
// a line wider than its box: an LTR line stays at the start edge and spills
// out of the end edge for every alignment, and an RTL line spills out of the
// left edge for every alignment. Mirrors Blink's LineOffsetForTextAlign
// (core/layout/length_utils.cc:1607-1653 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
//
// The line is 200px of Ahem in a 100px box. A line that fits keeps its
// alignment offset.
func TestLineOffsetForTextAlign_OverflowingLine(t *testing.T) {
	cases := []struct {
		dir, align, text string
		wantX            float64
	}{
		{"ltr", "left", "pppppppppp", 0},
		{"ltr", "right", "pppppppppp", 0},
		{"ltr", "center", "pppppppppp", 0},
		{"ltr", "end", "pppppppppp", 0},
		{"ltr", "start", "pppppppppp", 0},
		{"rtl", "left", "pppppppppp", -100},
		{"rtl", "right", "pppppppppp", -100},
		{"rtl", "center", "pppppppppp", -100},
		{"rtl", "end", "pppppppppp", -100},
		{"rtl", "start", "pppppppppp", -100},
		{"ltr", "center", "pp", 30},
		{"ltr", "right", "pp", 60},
		{"rtl", "left", "pp", 0},
		{"rtl", "center", "pp", 30},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s %s %s", tc.dir, tc.align, tc.text), func(t *testing.T) {
			root := layoutRootTestHTML(t, fmt.Sprintf(
				`<!doctype html><style>body{margin:0}</style>`+
					`<div id=t style="font:20px/1 Ahem;width:100px;white-space:nowrap;direction:%s;text-align:%s">%s</div>`,
				tc.dir, tc.align, tc.text))
			div := findBoxByID(root, "t")
			if div == nil {
				t.Fatal("no box for #t")
			}
			boxes := collectTextBoxes(div, nil)
			if len(boxes) == 0 {
				t.Fatal("no text box under #t")
			}
			x := boxes[0].X
			if x != tc.wantX {
				t.Errorf("text x = %g, want %g", x, tc.wantX)
			}
		})
	}
}
