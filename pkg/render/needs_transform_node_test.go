package render

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/layout"
)

// TestNeedsTransformNode_WillChangeOnInline: NeedsTransform checks the
// direct compositing reasons for the transform property before its IsBox
// return (paint_property_tree_builder.cc:1330-1350 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), and CompositingReasonFinder adds
// will-change reasons to any object with a paint layer (compositing_reason_
// finder.cc:411-419). A non-atomic inline has a layer when it is relatively
// positioned or has a grouping property (LayoutInline::LayerTypeRequired),
// so will-change: transform gives it a node; without a layer it gets none.
func TestNeedsTransformNode_WillChangeOnInline(t *testing.T) {
	inline := func(props map[string]string, pos css.PositionType) *layout.Box {
		s := css.NewStyle()
		s.Properties["display"] = "inline"
		for k, v := range props {
			s.Properties[k] = v
		}
		return &layout.Box{Style: s, Position: pos}
	}
	for _, tc := range []struct {
		name string
		box  *layout.Box
		want bool
	}{
		{"will-change only", inline(map[string]string{"will-change": "transform"}, css.PositionStatic), false},
		{"will-change + opacity", inline(map[string]string{"will-change": "transform", "opacity": "0.5"}, css.PositionStatic), true},
		{"will-change + relpos", inline(map[string]string{"will-change": "transform", "position": "relative"}, css.PositionRelative), true},
		{"opacity only", inline(map[string]string{"opacity": "0.5"}, css.PositionStatic), false},
	} {
		if got := needsTransformNode(tc.box); got != tc.want {
			t.Errorf("%s: needsTransformNode = %v, want %v", tc.name, got, tc.want)
		}
	}
}
