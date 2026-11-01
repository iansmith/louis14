package layout

import (
	"strconv"
	"testing"
)

func alignRect(t *testing.T, src, id string) fragRect {
	t.Helper()
	got := oofPageRects(t, `<!DOCTYPE html><body style="margin:0">`+src)[id]
	if len(got) != 1 {
		t.Fatalf("%s: want 1 box, got %d (%v)", id, len(got), got)
	}
	return got[0]
}

// TestButtonIsBlockFlow: a <button> is a LayoutBlockFlow, so block children
// stack in the block axis (html_button_element.cc:59-74 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and the whole content is
// centered by -internal-align-content-block (html.css:422-425).
func TestButtonIsBlockFlow(t *testing.T) {
	src := `<button style="display:block;width:100px;height:50px;padding:0;border:0">` +
		`<div id="a" style="height:10px"></div><div id="b" style="height:10px"></div></button>`
	if got, want := alignRect(t, src, "a"), (fragRect{0, 15, 100, 10}); got != want {
		t.Errorf("a: got %+v, want %+v", got, want)
	}
	if got, want := alignRect(t, src, "b"), (fragRect{0, 25, 100, 10}); got != want {
		t.Errorf("b: got %+v, want %+v", got, want)
	}
}

// TestButtonAlignmentIsSafe: button content taller than the button is not
// shifted above it (AlignBlockContent, "Buttons have safe alignment",
// block_layout_algorithm_utils.cc:196-203 @ a9f50e522efa).
func TestButtonAlignmentIsSafe(t *testing.T) {
	got := alignRect(t, `<button style="display:block;width:100px;height:10px;padding:0;border:0">`+
		`<div id="c" style="height:30px"></div></button>`, "c")
	if want := (fragRect{0, 0, 100, 30}); got != want {
		t.Errorf("c: got %+v, want %+v", got, want)
	}
}

// TestAlignContentOnBlockContainer: align-content shifts a block container's
// content in the block axis; overflow position defaults to safe
// (ComputeContentAlignment / AlignBlockContent,
// block_layout_algorithm_utils.cc:17-122, :186-232 @ a9f50e522efa).
func TestAlignContentOnBlockContainer(t *testing.T) {
	for _, tc := range []struct {
		align  string
		height int
		wantY  float64
	}{
		{"center", 100, 40},
		{"end", 100, 80},
		{"space-around", 100, 40},
		{"space-between", 100, 0},
		{"end", 10, 0},          // safe by default: no negative shift
		{"unsafe end", 10, -10}, // unsafe overflows the start edge
	} {
		src := `<div style="display:flow-root;width:100px;height:` + strconv.Itoa(tc.height) +
			`px;align-content:` + tc.align + `"><div id="c" style="height:20px"></div></div>`
		if got := alignRect(t, src, "c"); got.y != tc.wantY {
			t.Errorf("align-content:%s height:%d: got y=%v, want %v", tc.align, tc.height, got.y, tc.wantY)
		}
	}
}
