package layout

import (
	"testing"

	"louis14/pkg/css"
)

// getItemAutoMargins maps a flex item's physical margin:auto flags onto
// its main and cross axes through its writing direction, for every writing
// direction, auto-margin combination and flex direction. The oracle maps
// the flags with ToLogicalEdges (1 marks auto), then picks the axes: the
// inline axis is main when isRow, else the block axis.
func TestFlexItemAutoMarginsAllWritingDirections(t *testing.T) {
	modes := []WritingMode{WritingModeHorizontalTB, WritingModeVerticalRL, WritingModeVerticalLR, WritingModeSidewaysRL, WritingModeSidewaysLR}
	sides := []string{"margin-top", "margin-right", "margin-bottom", "margin-left"}
	for _, wm := range modes {
		for _, dir := range []Direction{DirectionLTR, DirectionRTL} {
			wdm := WritingDirectionMode{WM: wm, Dir: dir}
			for mask := 0; mask < 16; mask++ {
				style := css.NewStyle()
				for i, side := range sides {
					if mask&(1<<i) != 0 {
						style.Set(side, "auto")
					}
				}
				on := func(bit int) float64 {
					if mask&(1<<bit) != 0 {
						return 1
					}
					return 0
				}
				l := ToLogicalEdges(PhysicalEdges{Top: on(0), Right: on(1), Bottom: on(2), Left: on(3)}, wdm)
				inline := [2]bool{l.InlineStart == 1, l.InlineEnd == 1}
				block := [2]bool{l.BlockStart == 1, l.BlockEnd == 1}
				for _, isRow := range []bool{false, true} {
					want := [4]bool{block[0], block[1], inline[0], inline[1]}
					if isRow {
						want = [4]bool{inline[0], inline[1], block[0], block[1]}
					}
					ms, me, cs, ce := getItemAutoMargins(style, wdm, isRow)
					if got := [4]bool{ms, me, cs, ce}; got != want {
						t.Errorf("%v mask %04b row %v: main/cross auto %v, want %v", wdm, mask, isRow, got, want)
					}
				}
			}
		}
	}
}

// An auto margin on the container's cross axis keeps an item from
// stretching, whatever the item's own writing mode: Blink's
// ResolvedAlignSelf reads the container's physical cross-axis pair
// (margin-top/bottom in a horizontal flow, else margin-left/right)
// (flex/flex_layout_algorithm.cc:265-278 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b). For an orthogonal item whose
// block axis is the main axis, stretching sets the inline size its flex base
// size is measured at; a child's percentage padding makes that inline size
// visible. want is #i's offset from its 100x100 single-line container #f and
// its size, from Chrome 157 headless.
func TestFlexOrthogonalItemCrossAutoMarginStretch(t *testing.T) {
	cases := []struct {
		name, fStyle, iStyle, content string
		dx, dy, w, h                  float64
	}{
		{"vertical-rl item in a row, margin-top:auto: no stretch", "",
			"writing-mode:vertical-rl;margin-top:auto", `<div style="padding-left:50%">X</div>`, 0, 90, 15, 10},
		{"vertical-rl item in a row, margin-left:auto: stretches", "",
			"writing-mode:vertical-rl;margin-left:auto", `<div style="padding-left:50%">X</div>`, 40, 0, 60, 100},
		{"horizontal item in a vertical-rl row, margin-left:auto: no stretch", "writing-mode:vertical-rl",
			"writing-mode:horizontal-tb;margin-left:auto", `<div style="padding-top:50%">X</div>`, 90, 0, 10, 15},
		{"horizontal item in a vertical-rl row, margin-top:auto: stretches", "writing-mode:vertical-rl",
			"writing-mode:horizontal-tb;margin-top:auto", `<div style="padding-top:50%">X</div>`, 0, 40, 100, 60},
		{"vertical-rl item in a column, margin-left:auto: no stretch", "flex-direction:column",
			"writing-mode:vertical-rl;margin-left:auto", `<div style="padding-top:50%">X</div>`, 90, 0, 10, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0;font:10px/10px Ahem}</style>`+
				`<div id="f" style="display:flex;width:100px;height:100px;`+c.fStyle+`"><div id="i" style="`+c.iStyle+`">`+c.content+`</div></div>`)
			checkItemInFlex(t, r, c.dx, c.dy, c.w, c.h)
		})
	}
}

// An orthogonal item stretched to a definite cross size gets its flex base
// size from its aspect ratio, less its cross-axis (not main-axis) margins.
// resolveFlexBasis takes this path only for a negative flex-basis, which
// louis14 still accepts (LOU-1070); Chrome drops that declaration and gets
// the same base size from flex-basis: auto. A growing sibling with a zero
// base size makes the base size visible: #i's width is its base size plus
// half the free space. want is #i's offset from its 200x100 row container #f
// and its size, from Chrome 157 headless.
func TestFlexOrthogonalItemAspectRatioStretchMargins(t *testing.T) {
	cases := []struct {
		name, iStyle string
		dx, dy, w, h float64
	}{
		{"cross margin", "margin-top:10px", 0, 10, 145, 90},
		{"main margin", "margin-left:10px", 10, 0, 145, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := layoutRootTestHTML(t, `<!DOCTYPE html><style>body{margin:0}</style>`+
				`<div id="f" style="display:flex;width:200px;height:100px">`+
				`<div id="i" style="flex-grow:1;writing-mode:vertical-rl;aspect-ratio:1;flex-basis:-10px;`+c.iStyle+`"></div>`+
				`<div style="flex:1 1 0;height:10px"></div></div>`)
			checkItemInFlex(t, r, c.dx, c.dy, c.w, c.h)
		})
	}
}

// checkItemInFlex checks #i's offset from #f and its size.
func checkItemInFlex(t *testing.T, r *Box, dx, dy, w, h float64) {
	t.Helper()
	f, i := findBoxByID(r, "f"), findBoxByID(r, "i")
	if f == nil || i == nil {
		t.Fatal("no #f or #i box")
	}
	if gx, gy := i.X-f.X, i.Y-f.Y; gx != dx || gy != dy || i.Width != w || i.Height != h {
		t.Errorf("#i at (%v, %v) %vx%v, want (%v, %v) %vx%v", gx, gy, i.Width, i.Height, dx, dy, w, h)
	}
}
