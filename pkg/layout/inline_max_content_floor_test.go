package layout

import "testing"

// LOU-857. An inline formatting context's max-content size is floored at its
// min-content size: InlineNode::ComputeMinMaxSizes ends with
// max_size = max(min_size, max_size) (inline/inline_node.cc:2353-2354 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Here min-content is 100 (one
// inline-block per line) and the unfloored max-content 50 (100 + 0 - 50).
func TestInlineMaxContentFlooredAtMinContent(t *testing.T) {
	const content = `<span style="display:inline-block;width:100px;height:10px"></span><span style="display:inline-block;margin-right:-50px"></span>`
	tests := []struct {
		name  string
		style string
		width float64
	}{
		// min-content-le-max-content.tentative
		{"max-content", "width:max-content", 100},
		// Guards: right before the floor too (shrink-to-fit already clamps
		// the max-content at the min-content).
		{"min-content", "width:min-content", 100},
		{"fit-content in a 300px container", "width:fit-content", 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div style="width:300px;font-size:0"><div id="t" style="` + tc.style + `">` + content + `</div></div>`
			b := findBoxByID(layoutRootTestHTML(t, src), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != tc.width {
				t.Errorf("width = %v, want %v", b.Width, tc.width)
			}
		})
	}
}
