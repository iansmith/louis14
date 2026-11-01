package css

import "testing"

// TestCreateInheritedDisplayContentsStyleIfNeeded (LOU-567) mirrors Blink
// StyleResolver::CreateInheritedDisplayContentsStyleIfNeeded
// (core/css/resolver/style_resolver.cc:3351-3358 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): nil when the inherited
// properties match, otherwise an anonymous display:inline style inheriting
// from the display:contents element.
func TestCreateInheritedDisplayContentsStyleIfNeeded(t *testing.T) {
	layoutParent := NewStyle()
	layoutParent.Properties["color"] = "black"

	same := NewStyle()
	same.Properties["display"] = "contents"
	same.Properties["color"] = "black"
	same.Properties["border-top-width"] = "4px" // non-inherited: ignored
	same.Properties["--x"] = "1"                // custom: ignored, as in Blink
	if got := CreateInheritedDisplayContentsStyleIfNeeded(same, layoutParent); got != nil {
		t.Errorf("inherited-equal styles: got a wrapper style, want nil")
	}

	diff := NewStyle()
	diff.Properties["display"] = "contents"
	diff.Properties["color"] = "green"
	diff.Properties["border-top-width"] = "4px"
	got := CreateInheritedDisplayContentsStyleIfNeeded(diff, layoutParent)
	if got == nil {
		t.Fatal("color differs: want a wrapper style, got nil")
	}
	if got.GetDisplay() != DisplayInline {
		t.Errorf("wrapper display = %q, want inline", got.GetDisplay())
	}
	if c, _ := got.Get("color"); c != "green" {
		t.Errorf("wrapper color = %q, want green", c)
	}
	if _, ok := got.Properties["border-top-width"]; ok {
		t.Errorf("wrapper must not copy non-inherited border-top-width")
	}
}
