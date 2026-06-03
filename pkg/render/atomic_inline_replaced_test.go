package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/html"
	"louis14/pkg/layout"
)

// TestIsAtomicInlineForPaint_ReplacedWithClip is a regression guard for C108.
// Replaced inline elements with non-visible overflow (e.g. UA `overflow: clip`
// on <img>) must paint atomically through paintLayer so the established clip
// rectangle constrains the replaced content paint to the content-box. Without
// this routing, a sub-pixel content size that pixel-snaps wider than the
// content-box spills past the content-box into the border ring — diverging
// from a sibling flex / inline-block path that already routes through
// paintLayer.
//
// Fixes flexbox-basic-img-horiz-001 where the reference's inline <img>
// painted its image content over its own right vertical dotted border, while
// the test's flex-item <img> respected the clip; the 20-pixel diff was the
// right border ring at column x=207 across rows y=63..82.
func TestIsAtomicInlineForPaint_ReplacedWithClip(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		overflow   string
		display    string
		isAtomic   bool
		isFlexItem bool
	}{
		{"img-display-inline-overflow-clip", "img", "clip", "inline", true, false},
		{"img-display-inline-overflow-visible", "img", "visible", "inline", false, false},
		{"video-display-inline-overflow-clip", "video", "clip", "inline", true, false},
		{"iframe-display-inline-overflow-hidden", "iframe", "hidden", "inline", true, false},
		{"div-display-inline-overflow-clip", "div", "clip", "inline", false, false},
		{"img-display-inline-block", "img", "visible", "inline-block", true, false},
		{"img-flex-item", "img", "visible", "inline", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := css.NewStyle()
			s.Set("display", tt.display)
			s.Set("overflow", tt.overflow)
			box := &layout.Box{
				Style: s,
				Node:  &html.Node{Type: html.ElementNode, TagName: tt.tag},
			}
			if tt.isFlexItem {
				parentS := css.NewStyle()
				parentS.Set("display", "flex")
				box.Parent = &layout.Box{Style: parentS}
			}
			layer := &PaintLayer{Box: box}
			got := isAtomicInlineForPaint(layer)
			if got != tt.isAtomic {
				t.Errorf("isAtomicInlineForPaint(%s display=%s overflow=%s flex=%v) = %v, want %v",
					tt.tag, tt.display, tt.overflow, tt.isFlexItem, got, tt.isAtomic)
			}
		})
	}
}
