package css

import "testing"

// TestBorderStyleInitialNone: border-style's initial value is none
// (css_properties.json5:2820 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// so a border width with no border-style computes to a used width of 0
// (ComputedStyle::BorderWidth, computed_style.h:585-590 @ same SHA).
// WPT css-backgrounds/border-image-slice-003.
func TestBorderStyleInitialNone(t *testing.T) {
	s := ParseInlineStyle("border-width: 50px; border-color: red", nil)
	if bs := s.GetBorderStyle(); bs.Top != BorderStyleNone || bs.Left != BorderStyleNone {
		t.Errorf("unset border-style = %+v, want none on every side", bs)
	}
	if bw := s.GetBorderWidth(); bw != (BoxEdge{}) {
		t.Errorf("border-width:50px with no border-style used width = %+v, want 0", bw)
	}
}

// TestBorderWidthInitialMedium: border-*-width's initial value is medium
// (3px; css_properties.json5:2841 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b),
// used whenever the style is visible.
func TestBorderWidthInitialMedium(t *testing.T) {
	s := ParseInlineStyle("border-style: solid", nil)
	if bw := s.GetBorderWidth(); bw != (BoxEdge{Top: 3, Right: 3, Bottom: 3, Left: 3}) {
		t.Errorf("border-style:solid with no border-width = %+v, want 3px (medium) per side", bw)
	}
}

// TestBorderStyleHiddenZeroWidth: hidden, like none, gives a used width of 0
// (computed_style.h:586 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and is
// a <line-style> in the border shorthands, not a color.
func TestBorderStyleHiddenZeroWidth(t *testing.T) {
	for _, decl := range []string{
		"border-style: hidden; border-width: 5px",
		"border: 5px hidden red",
		"border-top: 5px hidden red; border-right: 5px hidden red; border-bottom: 5px hidden red; border-left: 5px hidden red",
	} {
		s := ParseInlineStyle(decl, nil)
		if bs := s.GetBorderStyle(); bs.Top != BorderStyle("hidden") {
			t.Errorf("%q: border-top-style = %q, want hidden", decl, bs.Top)
		}
		if bw := s.GetBorderWidth(); bw != (BoxEdge{}) {
			t.Errorf("%q: used border width = %+v, want 0", decl, bw)
		}
		if decl != "border-style: hidden; border-width: 5px" {
			if c, _ := s.Get("border-top-color"); c != "red" {
				t.Errorf("%q: border-top-color = %q, want red", decl, c)
			}
		}
	}
}
