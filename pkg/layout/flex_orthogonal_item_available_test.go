package layout

import "testing"

// An orthogonal item in a row flex container with an indefinite cross size
// lays out against the orthogonal fallback inline-size, not its own
// block-size: Blink's BuildSpaceForLayout passes ChildAvailableSize() and
// SetOrthogonalFallbackInlineSizeIfNeeded (flex/flex_layout_algorithm.cc:
// 697-805 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b). The vertical-rl item's
// block-size (50px, its main size here) must not become the available size
// of its inline axis, which would wrap "aa bb cc" onto three lines.
func TestFlexRowOrthogonalItemUsesFallbackInlineSize(t *testing.T) {
	root := layoutRootTestHTML(t, `<!DOCTYPE html>
<style>
body { margin: 0; font: 10px/10px Ahem; }
.line { display: flex; }
#item { writing-mode: vertical-rl; block-size: 50px; }
</style>
<div class="line"><div id="item">aa bb cc</div></div>`)
	item := findBoxByID(root, "item")
	if item == nil {
		t.Fatal("no #item box")
	}
	// The block-size still sets the item's main size (physical width).
	if item.Width != 50 {
		t.Errorf("#item width = %v, want 50 (block-size)", item.Width)
	}
	// One unwrapped line: the item's inline-size (physical height) is the
	// 80px max-content of "aa bb cc".
	if item.Height != 80 {
		t.Errorf("#item height = %v, want 80 (one line, max-content)", item.Height)
	}
}
