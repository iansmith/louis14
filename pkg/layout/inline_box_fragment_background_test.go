package layout

import "testing"

// TestHasVisibleInlinePaint_Background (LOU-550): an inline box creates a box
// fragment when its style HasBackground — a non-transparent background-color
// (currentcolor included, resolved against the element's color) or ANY
// background-image layer with an image, gradients included, not only url().
// Mirrors LayoutInline::ComputeInitialShouldCreateBoxFragment
// (core/layout/layout_inline.cc:179-227 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) → HasBoxDecorationBackground
// (core/style/computed_style.h:2436-2439 @ same SHA) → HasBackground
// (core/style/computed_style.cc:2486-2504 @ same SHA) → FillLayer
// AnyLayerHasImage (core/style/fill_layer.h:235-238 @ same SHA).
func TestHasVisibleInlinePaint_Background(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
		want  bool
	}{
		{"gradient image", []string{"background-image", "linear-gradient(red, blue)"}, true},
		{"currentcolor gradient", []string{"color", "green", "background-image", "linear-gradient(currentcolor, currentcolor)"}, true},
		{"url image", []string{"background-image", `url("x.png")`}, true},
		{"none image", []string{"background-image", "none"}, false},
		{"currentcolor bg-color", []string{"color", "green", "background-color", "currentcolor"}, true},
		{"transparent currentcolor", []string{"color", "transparent", "background-color", "currentcolor"}, false},
		{"transparent bg-color", []string{"background-color", "transparent"}, false},
	} {
		style := makeStyle(append([]string{"display", "inline"}, tc.props...)...)
		if got := hasVisibleInlinePaint(style); got != tc.want {
			t.Errorf("%s: hasVisibleInlinePaint = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestHasVisibleInlinePaint_BoxShadowAndBorderImage (LOU-560): the rest of
// HasBoxDecorationBackground — a box-shadow (computed_style.h:2436-2439 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b) or a border image
// (HasBorderDecoration, computed_style.h:1446-1448 @ same SHA) — also makes
// an inline create a box fragment. Text runs no longer paint their element's
// box decorations, so without the fragment an inline's box-shadow is lost
// (box-shadow-overlapping-001).
func TestHasVisibleInlinePaint_BoxShadowAndBorderImage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
		want  bool
	}{
		{"box-shadow", []string{"box-shadow", "-3em 0em"}, true},
		{"box-shadow none", []string{"box-shadow", "none"}, false},
		{"border-image gradient", []string{"border-image-source", "linear-gradient(red, blue)"}, true},
		{"border-image url", []string{"border-image-source", `url("x.png")`}, true},
		{"border-image none", []string{"border-image-source", "none"}, false},
	} {
		style := makeStyle(append([]string{"display", "inline"}, tc.props...)...)
		if got := hasVisibleInlinePaint(style); got != tc.want {
			t.Errorf("%s: hasVisibleInlinePaint = %v, want %v", tc.name, got, tc.want)
		}
	}
}
