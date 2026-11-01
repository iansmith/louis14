package css

import "testing"

// TestGetBorderImage_WidthCalc covers border-image-calc.html: calc() lengths,
// calc() percentages and mixed calc(% + px) in border-image-width. Mirrors
// ConsumeBorderImageWidth (css_parsing_utils.cc:5688 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), which accepts any
// <length-percentage> including math functions.
func TestGetBorderImage_WidthCalc(t *testing.T) {
	s := NewStyle()
	s.Set("border-image-width", "100px calc(100px) calc(100%) calc(50% + 50px)")
	b := s.GetBorderImage().BorderSlices
	for i, c := range []struct {
		got         BorderImageLength
		extent, out float64
	}{{b.Top, 200, 100}, {b.Right, 200, 100}, {b.Bottom, 100, 100}, {b.Left, 100, 100}} {
		if c.got.IsNumber() || c.got.IsAuto() {
			t.Fatalf("side %d: want a length, got %+v", i, c.got)
		}
		if v := c.got.Length().ValueForLength(c.extent); v != c.out {
			t.Errorf("side %d: ValueForLength(%v) = %v, want %v", i, c.extent, v, c.out)
		}
	}
}

// TestGetBorderImage_SliceFillAndPerSideType covers fill in leading
// position and mixed number / percent sides (ConsumeBorderImageSlice
// css_parsing_utils.cc:5648; numbers rounded per ConvertBorderImageSliceSide
// css_to_style_map.cc:618 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestGetBorderImage_SliceFillAndPerSideType(t *testing.T) {
	s := NewStyle()
	s.Set("border-image-slice", "fill 10.4 25%")
	n := s.GetBorderImage()
	if !n.Fill {
		t.Errorf("fill = false, want true")
	}
	sl := n.ImageSlices
	if sl.Top != (Length{Fixed: 10}) || sl.Bottom != (Length{Fixed: 10}) {
		t.Errorf("top/bottom = %+v/%+v, want fixed 10", sl.Top, sl.Bottom)
	}
	if sl.Right != (Length{Percent: 25}) || sl.Left != (Length{Percent: 25}) {
		t.Errorf("right/left = %+v/%+v, want 25%%", sl.Right, sl.Left)
	}
}

// TestGetBorderImage_Repeat maps border-image-repeat keywords
// (MapNinePieceImageRepeat css_to_style_map.cc:685 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestGetBorderImage_Repeat(t *testing.T) {
	s := NewStyle()
	s.Set("border-image-repeat", "space round")
	n := s.GetBorderImage()
	if n.HorizontalRule != SpaceImageRule || n.VerticalRule != RoundImageRule {
		t.Errorf("rules = %v/%v, want space/round", n.HorizontalRule, n.VerticalRule)
	}
}
