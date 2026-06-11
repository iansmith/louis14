package css

import "testing"

// LOU-236: MarkerShouldBeInside mirrors Blink ComputedStyle::
// MarkerShouldBeInside (computed_style.cc:2817-2827 @
// 4883d11fef4a8713e32cd582ecef6dc5457c8c3f): inside iff the item is a pure
// inline list item or list-style-position is inside. `inline flow-root
// list-item` is a block container and keeps its marker outside.
func TestMarkerShouldBeInside(t *testing.T) {
	cases := []struct {
		name     string
		display  string
		position string // "" = default outside
		want     bool
	}{
		{"block list-item outside", "list-item", "", false},
		{"block list-item inside", "list-item", "inside", true},
		{"inline list-item outside is inside-equivalent", "inline list-item", "", true},
		{"inline list-item inside", "inline list-item", "inside", true},
		{"inline flow-root list-item stays outside", "inline flow-root list-item", "", false},
		{"inline flow-root list-item explicit inside", "inline flow-root list-item", "inside", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStyle()
			s.Properties["display"] = tc.display
			if tc.position != "" {
				s.Properties["list-style-position"] = tc.position
			}
			if got := s.MarkerShouldBeInside(); got != tc.want {
				t.Errorf("display=%q position=%q: MarkerShouldBeInside() = %v, want %v",
					tc.display, tc.position, got, tc.want)
			}
		})
	}
}
