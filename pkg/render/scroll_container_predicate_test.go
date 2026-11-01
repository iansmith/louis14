package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// TestEffectiveBackgroundClip_LocalAttachmentNeedsScrollContainer pins
// FillLayerInfo::is_clipped_with_local_scrolling (paint/
// box_painter_base.cc:639-641, fed PhysicalBoxFragment::IsScrollContainer
// by BoxFragmentPainter::GetFillLayerInfo, box_fragment_painter.cc:
// 2346-2358 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): only a scroll
// container forces a local-attachment background to the padding box.
// overflow:clip is not a scroll container, so its border-box clip stands.
// ContouredBorderRectForClip (box_painter_base.cc:1236-1246 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) tests kContent before the
// local-scrolling padding rule, so a declared content-box clip wins.
func TestEffectiveBackgroundClip_LocalAttachmentNeedsScrollContainer(t *testing.T) {
	for _, tc := range []struct {
		overflow string
		clip     string
		want     css.BackgroundClipType
	}{
		{"hidden", "border-box", css.BackgroundClipPaddingBox},
		{"auto", "border-box", css.BackgroundClipPaddingBox},
		{"clip", "border-box", css.BackgroundClipBorderBox},
		{"visible", "border-box", css.BackgroundClipBorderBox},
		{"hidden", "content-box", css.BackgroundClipContentBox},
		{"hidden", "padding-box", css.BackgroundClipPaddingBox},
	} {
		s := css.NewStyle()
		s.Properties["display"] = "block"
		s.Properties["overflow"] = tc.overflow
		s.Properties["background-color"] = "green"
		s.Properties["background-attachment"] = "local"
		s.Properties["background-clip"] = tc.clip
		layer := newPaintLayer(&layout.Box{Style: s})
		if got := effectiveBackgroundClip(layer); got != tc.want {
			t.Errorf("overflow:%s clip:%s: effectiveBackgroundClip = %v, want %v", tc.overflow, tc.clip, got, tc.want)
		}
	}
}
