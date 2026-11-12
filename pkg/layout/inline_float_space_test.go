package layout

import "testing"

// LOU-859. A float inside inline content is laid out in the space the block
// container gives its inline content: the container's child available and
// percentage sizes, as for a float that is a block child
// (LineBreaker::HandleFloat, inline/line_breaker.cc:3787-3796 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInlineFloatSpace(t *testing.T) {
	tests := []struct {
		name          string
		html          string
		width, height float64
	}{
		{
			name:   "height 100% float in a span",
			html:   `<div style="height:100px"><span><div id="t" style="float:left;height:100%;width:20px"></div></span></div>`,
			width:  20,
			height: 100,
		},
		{
			name:   "control: height 100% float as a block child",
			html:   `<div style="height:100px"><div id="t" style="float:left;height:100%;width:20px"></div></div>`,
			width:  20,
			height: 100,
		},
		{
			// orthogonal-writing-mode-float-in-inline: the four words fit on
			// one 100px line of the vertical-rl float.
			name:   "orthogonal height 100% float in a span inside a shrink-to-fit float",
			html:   `<div style="float:left;height:100px"><span><div id="t" style="float:left;writing-mode:vertical-rl;height:100%;line-height:100px">&nbsp; &nbsp; &nbsp; &nbsp;</div></span></div>`,
			width:  100,
			height: 100,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0">`+tc.html), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != tc.width || b.Height != tc.height {
				t.Errorf("float = %vx%v, want %vx%v", b.Width, b.Height, tc.width, tc.height)
			}
		})
	}
}

// The same float inside a span and as a block child gets the same space, so
// the same size: the available block size half (an orthogonal float whose
// inline size comes from it) and an indefinite container (the percentage
// falls back to auto).
func TestInlineFloatSpaceMatchesBlockChildFloat(t *testing.T) {
	tests := []struct {
		name, container, float string
	}{
		{"orthogonal float, available block size from a 50px container", "height:50px",
			`<div id="t" style="float:left;writing-mode:vertical-rl;line-height:10px">aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa</div>`},
		{"height 100% float in an auto-height container", "",
			`<div id="t" style="float:left;height:100%;width:20px">x</div>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			size := func(html string) (float64, float64) {
				b := findBoxByID(layoutRootTestHTML(t, `<!DOCTYPE html><body style="margin:0"><div style="`+tc.container+`">`+html+`</div>`), "t")
				if b == nil {
					t.Fatal("no #t box")
				}
				return b.Width, b.Height
			}
			bw, bh := size(tc.float)
			sw, sh := size(`<span>` + tc.float + `</span>`)
			if sw != bw || sh != bh {
				t.Errorf("float in a span = %vx%v, as a block child = %vx%v; want equal", sw, sh, bw, bh)
			}
		})
	}
}
