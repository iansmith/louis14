package layout

import "testing"

// nestedOOFInnerMulticol is the inner multicol of the nested-multicol OOF
// probes: two 25px columns, no gap, column-fill:auto.
const nestedOOFInnerMulticol = "column-count:2;column-gap:0;column-fill:auto;width:50px;"

// TestLayoutOOFsInMulticol pins Blink's layout of out-of-flow descendants
// whose containing block is inside a nested inner multicol. They wait for
// the outermost fragmentation context root, which lays them out in the inner
// multicol's columns (OutOfFlowLayoutPart::HandleMulticolsWithPendingOOFs /
// LayoutOOFsInMulticol, out_of_flow_layout_part.cc:1293-1557 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). Columns created for an OOF that
// runs past the inner multicol's last column are appended to the last inner
// multicol fragment that has columns, so they overflow in the inline
// direction (:1488-1535, a documented Blink limitation). A fixed descendant
// the laid-out OOF surfaces is handed back to the outer builder at the
// inner multicol's offset (FragmentBuilder::TransferOutOfFlowCandidates,
// fragment_builder.cc:682-710). The inner multicol's in-flow content is laid
// out once: a fragment resumed after all its children were seen has no
// columns (column_layout_algorithm.cc:687-694 SetHasSeenAllChildren).
// Expected rects are Chrome 155 getClientRects.
func TestLayoutOOFsInMulticol(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want map[string][]fragRect
	}{{
		// Plan probe pH: the inner multicol (h=150) spans two outer
		// columns; `a` (static position 100 in `rel`, 250px of content)
		// starts in the first fragment's second column and continues in
		// two new columns appended to that fragment (x=50, 75), not in the
		// second inner fragment (x=60), which has no columns.
		name: "pH",
		src: `<div id=outer style="column-count:2;column-gap:10px;column-fill:auto;width:110px;height:100px">` +
			`<div id=inner style="` + nestedOOFInnerMulticol + `height:150px"><div id=rel style="position:relative">` +
			`<div style="height:100px"></div>` +
			`<div id=a style="position:absolute;width:25px"><div style="height:250px"></div></div>` +
			`</div></div></div>`,
		want: map[string][]fragRect{
			"rel": {{0, 0, 25, 100}},
			"a":   {{25, 0, 25, 100}, {50, 0, 25, 100}, {75, 0, 25, 50}},
		},
	}, {
		// Plan probe pE: an auto-sized abspos in an auto-height inner
		// multicol; its fixed child, contained by the transformed outer
		// multicol, is placed at the end of the abspos's last fragment.
		name: "pE",
		src: `<div id=outer style="column-count:2;column-gap:10px;column-fill:auto;width:110px;height:100px;transform:translateX(0)">` +
			`<div id=inner style="` + nestedOOFInnerMulticol + `"><div id=rel style="position:relative">` +
			`<div id=a style="position:absolute;width:25px"><div style="height:250px"></div>` +
			`<div id=fx style="position:fixed;width:10px;height:10px"></div></div>` +
			`</div></div></div>`,
		want: map[string][]fragRect{
			"a":  {{0, 0, 25, 100}, {25, 0, 25, 100}, {50, 0, 25, 50}},
			"fx": {{50, 50, 10, 10}},
		},
	}, {
		// fixedpos-static-pos-with-viewport-cb-003's shape: the inner
		// multicol (h=100) has two columns; `a` starts in the second and
		// continues in a new third column (x=50); its fixed child, contained
		// by the viewport, is at the end of a's last fragment.
		name: "static-pos-003",
		src: `<div id=outer style="column-count:2;column-gap:0;column-fill:auto;width:100px;height:100px">` +
			`<div id=inner style="` + nestedOOFInnerMulticol + `height:100px"><div id=rel style="position:relative">` +
			`<div style="height:100px"></div>` +
			`<div id=a style="position:absolute;width:25px"><div style="height:200px"></div>` +
			`<div id=fx style="position:fixed;width:10px;height:10px"></div></div>` +
			`</div></div></div>`,
		want: map[string][]fragRect{
			"rel": {{0, 0, 25, 100}},
			"a":   {{25, 0, 25, 100}, {50, 0, 25, 100}},
			"fx":  {{50, 100, 10, 10}},
		},
	}, {
		// multicol-nested-032's shape: the 50px containing block is laid
		// out once, and the 400px abspos fills the first inner fragment's
		// two columns and two new ones appended to it.
		name: "nested-032",
		src: `<div id=outer style="column-count:2;column-gap:0;column-fill:auto;width:100px;height:100px">` +
			`<div id=nested style="column-count:2;column-gap:0;column-fill:auto;width:100%;height:200px">` +
			`<div id=abscb style="position:relative;width:100%;height:50px">` +
			`<div id=abspos style="position:absolute;top:0;left:0;width:100%;height:400px"></div>` +
			`</div></div></div>`,
		want: map[string][]fragRect{
			"abscb":  {{0, 0, 25, 50}},
			"abspos": {{0, 0, 25, 100}, {25, 0, 25, 100}, {50, 0, 25, 100}, {75, 0, 25, 100}},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+tc.src)
			checkRects(t, got, tc.want)
		})
	}
}
