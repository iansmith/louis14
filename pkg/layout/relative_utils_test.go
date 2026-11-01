package layout

import (
	"testing"
)

// TestRelativeOffsetBakedIntoChildOffset_LOU491: the parent applies a
// position:relative child's offset when it places the child, so the child's
// link offset (and everything under it) is at the shifted position. Mirrors
// Blink's BoxFragmentBuilder::AddChild, which computes
// ComputeRelativeOffsetForBoxFragment against the builder's
// child_available_size_ and adds it via AddChildInternal(&child,
// child_offset + *relative_offset) (box_fragment_builder.cc:255-267,
// :372-375 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Expected rects are
// border boxes in the root's coordinates, as getBoundingClientRect reports
// them (relative positioning included). They are derived from CSS 2.1
// §9.4.3 / CSS Position 3 §3.4 and the cited Blink code, not a Chrome probe
// (checkRects' "(Chrome 155)" suffix is its generic wording).
func TestRelativeOffsetBakedIntoChildOffset_LOU491(t *testing.T) {
	tests := []struct {
		name string
		tree marginSpec
		want map[string][]fragRect
	}{
		{
			name: "block left/top",
			tree: ms("W", nil,
				ms("rel", props("position", "relative", "left", "5px", "top", "10px"),
					ms("in", props("height", "20px")))),
			want: map[string][]fragRect{
				"rel": {{5, 10, 200, 20}},
				"in":  {{5, 10, 200, 20}},
			},
		},
		{
			name: "block right/bottom",
			tree: ms("W", nil,
				ms("rel", props("position", "relative", "right", "5px", "bottom", "10px", "height", "20px"))),
			want: map[string][]fragRect{
				"rel": {{-5, -10, 200, 20}},
			},
		},
		{
			// Percentages resolve against the parent's child available size
			// (W's content box, 200x100).
			name: "percentages against a definite parent",
			tree: ms("W", props("height", "100px"),
				ms("rel", props("position", "relative", "left", "10%", "top", "10%", "height", "20px"))),
			want: map[string][]fragRect{
				"rel": {{20, 10, 200, 20}},
			},
		},
		{
			// A percentage of an indefinite size is auto (Blink ResolveInset,
			// relative_utils.cc:28-36 @ a9f50e522efa; CSS 2.1 §9.3.2), so
			// bottom wins: top = -bottom.
			name: "percentage of an indefinite block size is auto",
			tree: ms("W", nil,
				ms("rel", props("position", "relative", "top", "10%", "bottom", "5px", "height", "20px"))),
			want: map[string][]fragRect{
				"rel": {{0, -5, 200, 20}},
			},
		},
		{
			// A relative multicol container is shifted by its parent like
			// any other box (LOU-363 defect 4; multicol-rule-004).
			name: "multicol container",
			tree: ms("W", nil,
				ms("mc", props("column-count", "2", "column-gap", "0", "position", "relative", "right", "80px", "bottom", "100px"),
					ms("b", props("height", "100px")))),
			want: map[string][]fragRect{
				"mc": {{-80, -100, 200, 50}},
			},
		},
		{
			name: "flex container",
			tree: ms("W", nil,
				ms("fx", props("display", "flex", "position", "relative", "left", "30px", "top", "40px"),
					ms("b", props("height", "10px", "width", "10px")))),
			want: map[string][]fragRect{
				"fx": {{30, 40, 200, 10}},
				"b":  {{30, 40, 10, 10}},
			},
		},
		{
			// Grid items' percentages resolve against their grid area
			// (grid_layout_algorithm.cc:1879-1888 @ a9f50e522efa).
			name: "grid item against its grid area",
			tree: ms("W", nil,
				ms("g", props("display", "grid", "grid-template-columns", "100px 100px", "grid-template-rows", "50px"),
					ms("a", nil),
					ms("b", props("position", "relative", "left", "10%", "top", "20%")))),
			want: map[string][]fragRect{
				"b": {{110, 10, 100, 50}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frag, ids := layoutInFlowRoot(t, tc.tree)
			checkRects(t, borderBoxRectsByID(frag, ids), tc.want)
		})
	}
}
