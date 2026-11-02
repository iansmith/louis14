package css

import "testing"

// perspective: 0 must establish a perspective context with used value 1px,
// mirroring Blink: ComputedStyle::HasPerspective() is `Perspective() >= 0`
// (only negative means unset) and UsedPerspective() is `max(1.0f, Perspective())`
// (computed_style.h:1547-1552 @ 5e86b7727894c594c4f2859dbd0efae5d63374f2).
func TestGetPerspective_ZeroClampsToOne(t *testing.T) {
	tests := []struct {
		val    string
		wantD  float64
		wantOK bool
	}{
		{"0", 1, true},
		{"0px", 1, true},
		{"0.5px", 1, true},   // existing sub-1px clamp preserved
		{"-100px", 0, false}, // negative remains "no perspective"
		{"none", 0, false},   // default remains unset
		{"100px", 100, true}, // positive passthrough
	}
	for _, tt := range tests {
		s := NewStyle()
		s.Set("perspective", tt.val)
		d, ok := s.GetPerspective()
		if d != tt.wantD || ok != tt.wantOK {
			t.Errorf("GetPerspective(%q) = (%v, %v), want (%v, %v)", tt.val, d, ok, tt.wantD, tt.wantOK)
		}
	}
}

// scale()/scaleX()/scaleY() must accept <percentage> arguments (pct/100),
// mirroring Blink's transform_builder.cc:140-158 @ 5e86b772 which resolves
// scale factors via CSSPrimitiveValue::ComputeNumber for both number and
// percentage, per CSS Transforms L1 §funcdef-scale.
func TestParseTransforms_ScalePercentages(t *testing.T) {
	tests := []struct {
		val   string
		wantX float64
		wantY float64
	}{
		{"scale(50%, 75%)", 0.5, 0.75},
		{"scale(50%)", 0.5, 0.5},
		{"scaleX(50%)", 0.5, 1},
		{"scaleY(75%)", 1, 0.75},
		{"scale(2, 0.5)", 2, 0.5}, // plain numbers still work
		{"scaleX(2)", 2, 1},
		{"scaleY(0.5)", 1, 0.5},
	}
	for _, tt := range tests {
		ops := CreateTransformOperations(tt.val, initialConversionData())
		var sc *ScaleTransformOperation
		if len(ops.Operations) == 1 {
			sc, _ = ops.Operations[0].(*ScaleTransformOperation)
		}
		if sc == nil {
			t.Errorf("CreateTransformOperations(%q) = %+v, want one scale operation", tt.val, ops.Operations)
			continue
		}
		if sc.X != tt.wantX || sc.Y != tt.wantY {
			t.Errorf("CreateTransformOperations(%q) scale = (%v, %v), want (%v, %v)", tt.val, sc.X, sc.Y, tt.wantX, tt.wantY)
		}
	}
}
