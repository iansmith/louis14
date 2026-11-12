package layout

import (
	"fmt"
	"testing"
)

// TestLineBox_InlineSizeIsContentExtent checks that a line box is as wide as
// its content, not as the line's available width, and that text-align moves
// the line box as a whole: Blink sets the line box's inline size to the extent
// ComputeInlinePositions returns and adds the text-align offset to the line
// box's own offset (InlineLayoutAlgorithm::CreateLine,
// inline/inline_layout_algorithm.cc:380, :479-486, :622 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The text keeps its position.
//
// The block is 200px wide; "pp" in 20px Ahem is 40px.
func TestLineBox_InlineSizeIsContentExtent(t *testing.T) {
	cases := []struct {
		name, style, body string
		lineX, lineW      float64
		textX             float64
	}{
		{"left", "", "pp", 0, 40, 0},
		{"center", "text-align:center", "pp", 80, 40, 80},
		{"right", "text-align:right", "pp", 160, 40, 160},
		{"rtl start", "direction:rtl", "pp", 160, 40, 160},
		{"rtl overflowing", "direction:rtl;white-space:nowrap", "pppppppppppp", -40, 240, -40},
		{"after a float on the line", "font-size:10px", `<span style="float:left">X</span>X`, 10, 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := layoutRootTestHTML(t, fmt.Sprintf(
				`<!doctype html><style>body{margin:0}</style><div id=t style="width:200px;font:20px/1 Ahem;%s">%s</div>`,
				tc.style, tc.body))
			div := findBoxByID(root, "t")
			if div == nil {
				t.Fatal("no box for #t")
			}
			// The line box is the div's anonymous child (no node, no style)
			// that holds the in-flow text.
			var line, text *Box
			for _, c := range div.Children {
				if c.Node != nil || c.Style != nil {
					continue
				}
				for _, g := range c.Children {
					if g.Text != "" {
						line, text = c, g
						break
					}
				}
			}
			if line == nil {
				t.Fatal("no line box with text under #t")
			}
			if line.X != tc.lineX || line.Width != tc.lineW {
				t.Errorf("line box x=%g w=%g, want x=%g w=%g", line.X, line.Width, tc.lineX, tc.lineW)
			}
			if text.X != tc.textX {
				t.Errorf("text x=%g, want %g", text.X, tc.textX)
			}
		})
	}
}
