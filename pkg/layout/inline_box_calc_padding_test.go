package layout

import (
	"fmt"
	"testing"
)

// LOU-851. An inline box's margins and padding resolve against the same
// percentage base, the margin/padding percentage resolution size with an
// indefinite size clamped to 0, so calc(0% + Npx) keeps its length while
// sizing content (ComputeLinePadding / ComputeLineMarginsForSelf,
// length_utils.h:544-589; ComputePadding, length_utils.cc:1514-1541 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestInlineBoxCalcPercentEdgesInMaxContent(t *testing.T) {
	tests := []struct {
		name  string
		span  string
		width float64 // the max-content container's width
	}{
		// inline-intrinsic-size-calc
		{"margin and padding calc(0% + N)", "margin-left:calc(0% + 30px);padding-left:calc(0% + 50px)", 120},
		{"padding calc(0% + N) only", "padding-left:calc(0% + 50px)", 90},
		{"padding-right calc(10% + N)", "padding-right:calc(10% + 20px)", 60},
		{"control: plain lengths", "margin-left:30px;padding-left:50px", 120},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div id="t" style="width:max-content"><span style="` + tc.span + `"><span style="display:inline-block;width:40px;height:10px"></span></span></div>`
			b := findBoxByID(layoutRootTestHTML(t, src), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.Width != tc.width {
				t.Errorf("container width = %v, want %v", b.Width, tc.width)
			}
		})
	}
}

// In layout an inline box's percentage margins and padding resolve against
// the containing block's inline size (MarginPaddingPercentageResolutionSize),
// not the line's available width, which a float narrows
// (ComputeLineMarginsForSelf /
// ComputeLinePadding, length_utils.h:544-589 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b; LOU-771). The padding also counts
// towards placement.
func TestInlinePercentEdgesInLayout(t *testing.T) {
	const box = `<span id="t" style="display:inline-block;width:40px;height:10px%s"></span>`
	tests := []struct {
		name      string
		container string
		html      string
		wantX     float64
	}{
		{"span padding calc(10% + 20px)", "width:200px",
			`<span style="padding-left:calc(10% + 20px)">` + fmt.Sprintf(box, "") + `</span>`, 40},
		{"nowrap: span padding-left 10%", "width:200px;white-space:nowrap",
			`<span style="padding-left:10%">` + fmt.Sprintf(box, "") + `</span>`, 20},
		{"nowrap: span margin-left 10%", "width:200px;white-space:nowrap",
			`<span style="margin-left:10%">` + fmt.Sprintf(box, "") + `</span>`, 20},
		{"nowrap: atomic inline margin-left 10%", "width:200px;white-space:nowrap",
			fmt.Sprintf(box, ";margin-left:10%"), 20},
		{"float narrows the line: span margin-left 10%", "width:200px",
			`<div style="float:left;width:50px;height:10px"></div><span style="margin-left:10%">` + fmt.Sprintf(box, "") + `</span>`, 70},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := `<!DOCTYPE html><body style="margin:0"><div style="` + tc.container + `">` + tc.html + `</div>`
			b := findBoxByID(layoutRootTestHTML(t, src), "t")
			if b == nil {
				t.Fatal("no #t box")
			}
			if b.X != tc.wantX {
				t.Errorf("inline-block x = %v, want %v", b.X, tc.wantX)
			}
		})
	}
}
