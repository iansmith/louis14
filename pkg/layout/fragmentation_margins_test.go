package layout

import (
	"slices"
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
)

// marginSpec describes one block in a fragmentation-margin fixture: its id,
// its style properties (display:block is implied) and its children.
type marginSpec struct {
	id    string
	props []string
	kids  []marginSpec
}

func ms(id string, props []string, kids ...marginSpec) marginSpec {
	return marginSpec{id: id, props: props, kids: kids}
}

// build creates the DOM subtree for s, registering every node's style and id.
func (s marginSpec) build(styles map[*html.Node]*css.Style, ids map[*html.Node]string) *html.Node {
	var kids []*html.Node
	for _, k := range s.kids {
		kids = append(kids, k.build(styles, ids))
	}
	n := makeNode("div", kids...)
	styles[n] = makeStyle(append([]string{"display", "block"}, s.props...)...)
	ids[n] = s.id
	return n
}

// fragRect is a fragment's border box in the coordinates of the fragment tree
// root it was found under.
type fragRect struct{ x, y, w, h float64 }

// borderBoxRectsByID walks frag and returns, per id, the border-box rect of
// every fragment the node generated, in frag's coordinates. Child offsets are
// relative to the parent's content box, as fragmentToBox reads them
// (engine.go: content origin = border-box origin + border + padding).
func borderBoxRectsByID(frag *PhysicalFragment, ids map[*html.Node]string) map[string][]fragRect {
	out := map[string][]fragRect{}
	var walk func(f *PhysicalFragment, x, y float64)
	walk = func(f *PhysicalFragment, x, y float64) {
		if id, ok := ids[f.Node]; ok {
			out[id] = append(out[id], fragRect{x, y, f.Size.WidthF64(), f.Size.HeightF64()})
		}
		if f.BoxData != nil {
			x += f.BoxData.Border.Left + f.BoxData.Padding.Left
			y += f.BoxData.Border.Top + f.BoxData.Padding.Top
		}
		for _, ch := range f.Children {
			walk(ch.Fragment, x+ch.Offset.Left.Float64(), y+ch.Offset.Top.Float64())
		}
	}
	walk(frag, 0, 0)
	return out
}

// TestFragmentationMargins pins margin truncation at fragmentainer breaks
// against Chrome 155. Every fixture is a 100x100 multicol with column-count 2
// and column-gap 0 (columns are 50 wide; an overflow column sits at x=100),
// whose single child is the wrapper W. Expected rects are the Chrome
// getClientRects() of each node relative to the multicol border box. `rects`
// pins whole border boxes; `tops` pins only fragment origins, for boxes whose
// first-fragment height also depends on stretching a breaking fragment to the
// fragmentainer end (out of scope here).
//
// Blink truncates margins with two cooperating mechanisms
// (@ a9f50e522efa):
//   - AdjustMarginsForFragmentation (fragmentation_utils.h:279-296): a
//     child's block-start margin is zeroed when it resumes (break-inside
//     token) or follows an unforced break-before; a forced break-before
//     keeps it and resets the strut (block_layout_algorithm.cc:2839-2846).
//   - MarginStrut::discard_margins (geometry/margin_strut.h:29-30), set when
//     a container resumes after an unforced break that no column spanner
//     caused (block_layout_algorithm.cc:859-866), so margins adjoining the
//     fragmentainer start are discarded, including ones collapsing up
//     through a pushed child.
//
// A resumed fragment never collapses margins through its block-start
// (block_layout_algorithm.cc:879-909, rule C). A child's fragmentainer offset
// is its border edge with every margin collapsing through its block-start
// resolved (FragmentainerOffsetAtBfc + bfc_block_offset, :3156-3158; the
// child is re-laid out once its BFC offset resolves, :845-849, :2539-2580).
func TestFragmentationMargins(t *testing.T) {
	h := func(v string) []string { return []string{"height", v} }
	p := func(kv ...string) []string { return kv }
	for _, tc := range []struct {
		name    string
		balance bool // column-fill:balance; auto otherwise
		tree    marginSpec
		rects   map[string][]fragRect
		tops    map[string][][2]float64
	}{
		// --- AdjustMarginsForFragmentation: unforced break-before truncates.
		{name: "S5 pushed avoid box drops its margin",
			tree: ms("W", nil, ms("P", h("60px")),
				ms("B", p("margin-top", "20px", "height", "50px", "break-inside", "avoid"))),
			rects: map[string][]fragRect{"B": {{50, 0, 50, 50}}}},
		{name: "S7 child starting past the end breaks before and drops its margin",
			tree: ms("W", nil, ms("P", h("90px")),
				ms("B", p("margin-top", "20px", "height", "30px"))),
			rects: map[string][]fragRect{"P": {{0, 0, 50, 90}}, "B": {{50, 0, 50, 30}}}},
		{name: "N1 pushed new formatting context drops its margin",
			tree: ms("W", nil, ms("P", h("30px")),
				ms("C", p("display", "flow-root", "margin-top", "20px", "height", "80px", "break-inside", "avoid"))),
			rects: map[string][]fragRect{"C": {{50, 0, 50, 80}}}},
		{name: "B1 balanced: pushed avoid box is not lost", balance: true,
			tree: ms("W", nil, ms("A", h("60px")),
				ms("B", p("margin-top", "20px", "height", "60px", "break-inside", "avoid"))),
			rects: map[string][]fragRect{"A": {{0, 0, 50, 60}}, "B": {{50, 0, 50, 60}}}},

		// --- AdjustMarginsForFragmentation: a resumed child drops its margin.
		{name: "E3 sliced leaf keeps its margin only before the first fragment",
			tree:  ms("W", nil, ms("L", p("margin-top", "20px", "height", "150px"))),
			rects: map[string][]fragRect{"L": {{0, 20, 50, 80}, {50, 0, 50, 70}}}},

		// --- discard_margins: margins adjoining the column start are discarded.
		{name: "S1 margin collapsing up through a pushed container",
			tree: ms("W", nil, ms("P", h("30px")),
				ms("C", nil, ms("K", p("margin-top", "20px", "height", "80px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"C": {{50, 0, 50, 80}}, "K": {{50, 0, 50, 80}}}},
		{name: "S10 margin collapsing up through a container pushed past the end",
			tree: ms("W", nil, ms("P", h("90px")),
				ms("Q", nil, ms("K", p("margin-top", "20px", "height", "30px")))),
			rects: map[string][]fragRect{"Q": {{50, 0, 50, 30}}, "K": {{50, 0, 50, 30}}}},
		{name: "S8 margin inside a bordered pushed container is kept",
			tree: ms("W", nil, ms("P", h("60px")),
				ms("C", p("margin-top", "20px", "border-top-width", "5px", "border-top-style", "solid", "break-inside", "avoid"),
					ms("K", p("margin-top", "10px", "height", "30px")))),
			rects: map[string][]fragRect{"C": {{50, 0, 50, 45}}, "K": {{50, 15, 50, 30}}}},
		{name: "S9 resumed bordered container discards its pushed child's margin",
			tree: ms("W", nil, ms("P", h("60px")),
				ms("C", p("border-top-width", "5px", "border-top-style", "solid"),
					ms("X", h("30px")),
					ms("K", p("margin-top", "20px", "height", "30px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"X": {{0, 65, 50, 30}}, "K": {{50, 0, 50, 30}}},
			tops:  map[string][][2]float64{"C": {{0, 60}, {50, 0}}, "W": {{0, 0}, {50, 0}}}},
		{name: "S9c cloned decorations still discard the child's margin",
			tree: ms("W", nil, ms("P", h("60px")),
				ms("C", p("border-top-width", "5px", "border-top-style", "solid", "box-decoration-break", "clone"),
					ms("X", h("30px")),
					ms("K", p("margin-top", "20px", "height", "30px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"X": {{0, 65, 50, 30}}, "K": {{50, 5, 50, 30}}},
			tops:  map[string][][2]float64{"C": {{0, 60}, {50, 0}}}},

		// --- Forced breaks keep the margin; a resumed fragment does not
		// collapse it through its block-start.
		{name: "S4 forced break keeps the margin inside the resumed container",
			tree: ms("W", nil, ms("P", h("30px")),
				ms("B", p("break-before", "column", "margin-top", "20px", "height", "30px"))),
			rects: map[string][]fragRect{"B": {{50, 20, 50, 30}}},
			tops:  map[string][][2]float64{"W": {{0, 0}, {50, 0}}}},
		{name: "G3 margin after a column spanner is kept inside the resumed container",
			tree: ms("W", nil, ms("A", h("10px")),
				ms("S", p("column-span", "all", "height", "10px")),
				ms("B", p("margin-top", "15px", "height", "10px"))),
			rects: map[string][]fragRect{"S": {{0, 5, 100, 10}}, "B": {{0, 30, 50, 10}}},
			tops:  map[string][][2]float64{"W": {{0, 0}, {50, 0}, {0, 15}}}},
		{name: "G4 margin after a column spanner collapsing through a container is kept",
			tree: ms("W", nil, ms("A", h("10px")),
				ms("S", p("column-span", "all", "height", "10px")),
				ms("V", nil, ms("B", p("margin-top", "15px", "height", "10px")))),
			rects: map[string][]fragRect{"V": {{0, 30, 50, 10}}, "B": {{0, 30, 50, 10}}}},

		// --- Termination: a retained margin costs at most one extra push;
		// the content is never dropped.
		{name: "T1 monolithic first child with a margin is pushed once, not lost",
			tree:  ms("W", nil, ms("M", p("margin-top", "20px", "height", "120px", "contain", "size"))),
			rects: map[string][]fragRect{"M": {{50, 0, 50, 120}}}},
		{name: "T2 pushed monolithic child overflows its column instead of vanishing",
			tree: ms("W", nil, ms("P", h("10px")),
				ms("M", p("margin-top", "20px", "height", "120px", "contain", "size"))),
			rects: map[string][]fragRect{"M": {{50, 0, 50, 120}}}},
		{name: "T3 forced break keeps the margin, so the child is pushed once more",
			tree: ms("W", nil, ms("P", h("10px")),
				ms("M", p("break-before", "column", "margin-top", "20px", "height", "90px", "contain", "size"))),
			rects: map[string][]fragRect{"M": {{100, 0, 50, 90}}}},

		// --- The child's fragmentainer offset includes its collapsed margins.
		{name: "S2 margin on the container counts toward its child's break decision",
			tree: ms("W", nil, ms("P", h("10px")),
				ms("A", p("margin-top", "20px"),
					ms("X", h("30px")),
					ms("Y", p("height", "50px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"X": {{0, 30, 50, 30}}, "Y": {{50, 0, 50, 50}}},
			tops:  map[string][][2]float64{"A": {{0, 30}, {50, 0}}}},
		{name: "S11b collapsed parent and child margins push an avoid box that does not fit",
			tree: ms("W", nil, ms("P", h("10px")),
				ms("A", p("margin-top", "20px"),
					ms("X", p("margin-top", "30px", "height", "20px")),
					ms("Y", p("height", "41px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"X": {{0, 40, 50, 20}}, "Y": {{50, 0, 50, 41}}}},
		// Guards: green today, and must stay so — the margins collapse, they
		// do not add up.
		{name: "S11 collapsed parent and child margins leave room for an avoid box",
			tree: ms("W", nil, ms("P", h("10px")),
				ms("A", p("margin-top", "20px"),
					ms("X", p("margin-top", "30px", "height", "20px")),
					ms("Y", p("height", "40px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"X": {{0, 40, 50, 20}}, "Y": {{0, 60, 50, 40}}}},
		{name: "H1 container margin larger than its child's: exact fit",
			tree: ms("W", nil, ms("F", h("30px")),
				ms("C", p("margin-top", "30px"),
					ms("K", p("margin-top", "20px", "height", "40px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"K": {{0, 60, 50, 40}}}},
		{name: "H2 child margin larger than its container's: exact fit",
			tree: ms("W", nil, ms("F", h("30px")),
				ms("C", p("margin-top", "20px"),
					ms("K", p("margin-top", "30px", "height", "40px", "break-inside", "avoid")))),
			rects: map[string][]fragRect{"K": {{0, 60, 50, 40}}}},

		// --- Guards: margins that no break truncates.
		{name: "S3 margin at the start of the first column is kept",
			tree:  ms("W", nil, ms("A", p("margin-top", "20px", "height", "30px"))),
			rects: map[string][]fragRect{"A": {{0, 20, 50, 30}}}},
		{name: "S6 margin after a resumed sibling is kept",
			tree: ms("W", nil, ms("P", h("150px")),
				ms("B", p("margin-top", "20px", "height", "10px"))),
			rects: map[string][]fragRect{"P": {{0, 0, 50, 100}, {50, 0, 50, 50}}, "B": {{50, 70, 50, 10}}}},
		{name: "R1 margin after a resumed child inside a resumed container is kept",
			tree: ms("W", nil, ms("P", h("80px")),
				ms("C", nil, ms("X", h("40px")), ms("K", p("margin-top", "20px", "height", "10px")))),
			rects: map[string][]fragRect{"X": {{0, 80, 50, 20}, {50, 0, 50, 20}}, "K": {{50, 40, 50, 10}}}},
		{name: "I1 margin after a resumed inline formatting context is kept",
			tree: ms("W", nil,
				ms("L", p("margin-bottom", "20px", "font-size", "0", "line-height", "0"),
					ms("I1a", p("display", "inline-block", "width", "50px", "height", "50px")),
					ms("I1b", p("display", "inline-block", "width", "50px", "height", "50px")),
					ms("I1c", p("display", "inline-block", "width", "50px", "height", "50px"))),
				ms("B", h("10px"))),
			rects: map[string][]fragRect{"I1c": {{50, 0, 50, 50}}, "B": {{50, 70, 50, 10}}}},
		{name: "N2 margin inside a pushed new formatting context is kept",
			tree: ms("W", nil, ms("P", h("30px")),
				ms("C", p("display", "flow-root", "break-inside", "avoid"),
					ms("K", p("margin-top", "20px", "height", "60px")))),
			rects: map[string][]fragRect{"C": {{50, 0, 50, 80}}, "K": {{50, 20, 50, 60}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			styles := map[*html.Node]*css.Style{}
			ids := map[*html.Node]string{}
			fill := "auto"
			if tc.balance {
				fill = "balance"
			}
			mc := twoColumnMulticol(tc.tree.build(styles, ids), styles, fill)
			got := borderBoxRectsByID(layoutMulticolForTest(t, mc, styles).Fragment, ids)
			for id, want := range tc.rects {
				if !slices.Equal(got[id], want) {
					t.Errorf("%s fragments = %v, want %v (Chrome 155)", id, got[id], want)
				}
			}
			for id, want := range tc.tops {
				var tops [][2]float64
				for _, r := range got[id] {
					tops = append(tops, [2]float64{r.x, r.y})
				}
				if !slices.Equal(tops, want) {
					t.Errorf("%s fragment origins = %v, want %v (Chrome 155)", id, tops, want)
				}
			}
		})
	}
}
