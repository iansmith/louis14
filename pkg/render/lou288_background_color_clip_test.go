package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// TestEffectiveBackgroundClip_BottomLayerAfterCull reproduces LOU-288.
//
// WPT css-backgrounds/background-color-clip.html declares two empty image
// layers (`background-image: none, none`) with three clip values
// (`background-clip: border-box, content-box, border-box`). Per CSS
// Backgrounds 3 §3.5 the background-color is clipped by the *bottom-most*
// layer's clip — here layer index 1 → content-box.
//
// GetBackgroundColorClip computes this correctly into layer.BackgroundClip,
// but GetBackgroundLayers culls the empty trailing layers (CullEmptyLayers
// always keeps the head), leaving a single surviving layer whose clip is the
// topmost declaration (border-box). effectiveBackgroundClip must trust the
// pre-computed BackgroundClip, not re-derive it from the post-cull list.
func TestEffectiveBackgroundClip_BottomLayerAfterCull(t *testing.T) {
	s := css.NewStyle()
	s.Properties["background-color"] = "green"
	s.Properties["background-image"] = "none, none"
	s.Properties["background-clip"] = "border-box, content-box, border-box"

	box := &layout.Box{Style: s}
	layer := newPaintLayer(box)

	got := effectiveBackgroundClip(layer)
	if got != css.BackgroundClipContentBox {
		t.Fatalf("effectiveBackgroundClip = %v, want content-box (bottom-most layer clip)", got)
	}
}
