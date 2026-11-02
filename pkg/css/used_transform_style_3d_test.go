package css

import "testing"

// TestUsedTransformStyle3D locks the grouping-property override of Blink's
// ComputedStyle::UsedTransformStyle3D (core/style/computed_style.h:2131-2147,
// :2216-2267 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a specified
// preserve-3d is used as flat when any grouping property is present.
func TestUsedTransformStyle3D(t *testing.T) {
	for _, tc := range []struct {
		name  string
		props []string
		want  TransformStyle
	}{
		{"flat stays flat", nil, TransformStyleFlat},
		{"preserve-3d alone", []string{"transform-style", "preserve-3d"}, TransformStylePreserve3D},
		{"overflow:hidden", []string{"transform-style", "preserve-3d", "overflow", "hidden"}, TransformStyleFlat},
		{"overflow:clip", []string{"transform-style", "preserve-3d", "overflow", "clip"}, TransformStyleFlat},
		{"overflow-y:scroll", []string{"transform-style", "preserve-3d", "overflow-y", "scroll"}, TransformStyleFlat},
		{"opacity < 1", []string{"transform-style", "preserve-3d", "opacity", "0.5"}, TransformStyleFlat},
		{"opacity 1", []string{"transform-style", "preserve-3d", "opacity", "1"}, TransformStylePreserve3D},
		{"filter", []string{"transform-style", "preserve-3d", "filter", "blur(1px)"}, TransformStyleFlat},
		{"backdrop-filter", []string{"transform-style", "preserve-3d", "backdrop-filter", "blur(1px)"}, TransformStyleFlat},
		{"clip-path", []string{"transform-style", "preserve-3d", "clip-path", "inset(1px)"}, TransformStyleFlat},
		{"isolation", []string{"transform-style", "preserve-3d", "isolation", "isolate"}, TransformStyleFlat},
		{"mask-image", []string{"transform-style", "preserve-3d", "mask-image", "linear-gradient(red, blue)"}, TransformStyleFlat},
		{"mix-blend-mode", []string{"transform-style", "preserve-3d", "mix-blend-mode", "multiply"}, TransformStyleFlat},
		{"box-reflect", []string{"transform-style", "preserve-3d", "-webkit-box-reflect", "below"}, TransformStyleFlat},
		{"view-transition-name", []string{"transform-style", "preserve-3d", "view-transition-name", "a"}, TransformStyleFlat},
		{"clip on abspos", []string{"transform-style", "preserve-3d", "position", "absolute", "clip", "rect(0, 10px, 10px, 0)"}, TransformStyleFlat},
		{"clip on static (clip does not apply)", []string{"transform-style", "preserve-3d", "clip", "rect(0, 10px, 10px, 0)"}, TransformStylePreserve3D},
		{"will-change: opacity", []string{"transform-style", "preserve-3d", "will-change", "opacity"}, TransformStyleFlat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStyle()
			for i := 0; i+1 < len(tc.props); i += 2 {
				s.Properties[tc.props[i]] = tc.props[i+1]
			}
			if got := s.UsedTransformStyle3D(); got != tc.want {
				t.Errorf("UsedTransformStyle3D() = %v, want %v", got, tc.want)
			}
			if got := s.Preserves3D(); got != (tc.want == TransformStylePreserve3D) {
				t.Errorf("Preserves3D() = %v, want %v", got, tc.want == TransformStylePreserve3D)
			}
		})
	}
}
