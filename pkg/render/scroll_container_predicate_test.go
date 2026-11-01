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
func TestEffectiveBackgroundClip_LocalAttachmentNeedsScrollContainer(t *testing.T) {
	for _, tc := range []struct {
		overflow string
		want     css.BackgroundClipType
	}{
		{"hidden", css.BackgroundClipPaddingBox},
		{"auto", css.BackgroundClipPaddingBox},
		{"clip", css.BackgroundClipBorderBox},
		{"visible", css.BackgroundClipBorderBox},
	} {
		s := css.NewStyle()
		s.Properties["display"] = "block"
		s.Properties["overflow"] = tc.overflow
		s.Properties["background-color"] = "green"
		s.Properties["background-attachment"] = "local"
		layer := newPaintLayer(&layout.Box{Style: s})
		if got := effectiveBackgroundClip(layer); got != tc.want {
			t.Errorf("overflow:%s: effectiveBackgroundClip = %v, want %v", tc.overflow, got, tc.want)
		}
	}
}
