package layout

import "testing"

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

// In layout the base is the line's percentage resolution inline size, so the
// same padding places the content (and counts towards line breaking).
func TestInlineBoxCalcPercentPaddingInLayout(t *testing.T) {
	src := `<!DOCTYPE html><body style="margin:0"><div style="width:200px"><span style="padding-left:calc(10% + 20px)"><span id="t" style="display:inline-block;width:40px;height:10px"></span></span></div>`
	b := findBoxByID(layoutRootTestHTML(t, src), "t")
	if b == nil {
		t.Fatal("no #t box")
	}
	if b.X != 40 {
		t.Errorf("inline-block x = %v, want 40 (10%% of 200 + 20px padding)", b.X)
	}
}
