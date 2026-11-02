package css

import (
	"testing"

	"louis14/pkg/graphics"
	"louis14/pkg/html"
)

// image-orientation: from-image | none, inherited, initial from-image
// (css_properties.json5:4038-4052; ImageOrientation::ParseSingleValue,
// longhands_custom.cc:5381-5387; ConvertImageOrientation,
// style_builder_converter.cc:3429-3436 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestImageOrientationProperty(t *testing.T) {
	if NewStyle().ImageOrientation() != graphics.RespectImageOrientation {
		t.Error("initial image-orientation should respect the image orientation")
	}
	for _, bad := range []string{"90deg", "flip", "from-image none"} {
		ss, err := ParseStylesheet("img { image-orientation: none; image-orientation: "+bad+"; }", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ss.Rules[0].Declarations["image-orientation"]; got != "none" {
			t.Errorf("image-orientation: %s should be invalid; got %q", bad, got)
		}
	}
	ss, err := ParseStylesheet("div { image-orientation: none; }", nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := &html.Node{Type: html.ElementNode, TagName: "div"}
	child := &html.Node{Type: html.ElementNode, TagName: "img", Parent: parent}
	parent.Children = []*html.Node{child}
	ps := ComputeStyle(parent, []*Stylesheet{ss}, 800, 600, nil)
	if ps.ImageOrientation() != graphics.DoNotRespectImageOrientation {
		t.Error("image-orientation: none should not respect the orientation")
	}
	cs := ComputeStyle(child, []*Stylesheet{ss}, 800, 600, nil)
	ApplyInheritedProperties(child, cs, map[*html.Node]*Style{parent: ps})
	if cs.ImageOrientation() != graphics.DoNotRespectImageOrientation {
		t.Error("image-orientation should inherit")
	}
}
