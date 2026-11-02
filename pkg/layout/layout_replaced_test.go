package layout

import (
	"testing"

	"louis14/pkg/css"
	"louis14/pkg/geometry"
)

// LOU-590: LayoutReplaced::ComputeObjectFitAndPositionRect
// (layout_replaced.cc:422-484 @ a9f50e522efa9005e6ec765a9a785c74f5c2c86b).
func TestComputeObjectFitAndPositionRect(t *testing.T) {
	content := geometry.NewPhysicalRect(10, 20, 48, 32)
	natural := MakeFixedPhysicalNaturalSizingInfo(geometry.NewPhysicalSize(16, 8))
	center := css.InitialObjectPosition
	topRight := css.LengthPoint{X: css.Length{Percent: 100}, Y: css.Length{Percent: 0}}
	farEdge := css.LengthPoint{X: css.Length{Fixed: -2, Percent: 100}, Y: css.Length{Fixed: -1, Percent: 100}}
	for _, tc := range []struct {
		name string
		fit  css.ObjectFit
		pos  css.LengthPoint
		want geometry.PhysicalRect
	}{
		{"fill initial returns content box", css.ObjectFitFill, center, content},
		{"fill positioned stays content-sized", css.ObjectFitFill, farEdge, geometry.NewPhysicalRect(8, 19, 48, 32)},
		{"contain centred", css.ObjectFitContain, center, geometry.NewPhysicalRect(10, 24, 48, 24)},
		{"contain top right", css.ObjectFitContain, topRight, geometry.NewPhysicalRect(10, 20, 48, 24)},
		{"cover centred", css.ObjectFitCover, center, geometry.NewPhysicalRect(2, 20, 64, 32)},
		{"none far edges", css.ObjectFitNone, farEdge, geometry.NewPhysicalRect(40, 43, 16, 8)},
		{"scale-down larger box keeps natural size", css.ObjectFitScaleDown, center, geometry.NewPhysicalRect(26, 32, 16, 8)},
	} {
		if got := ComputeObjectFitAndPositionRect(tc.fit, tc.pos, content, natural); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// scale-down in a box smaller than the image behaves as contain.
	small := geometry.NewPhysicalRect(0, 0, 8, 8)
	if got, want := ComputeObjectFitAndPositionRect(css.ObjectFitScaleDown, center, small, natural), geometry.NewPhysicalRect(0, 2, 8, 4); got != want {
		t.Errorf("scale-down small: got %+v, want %+v", got, want)
	}
}

// ConcreteObjectSize (natural_sizing_info.cc:54-101 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b): a ratio-only object is
// contain-fitted into the default size; a missing dimension comes from the
// ratio or the default.
func TestConcreteObjectSize(t *testing.T) {
	def := geometry.NewPhysicalSize(300, 150)
	ratio := geometry.NewPhysicalSize(2, 1)
	for _, tc := range []struct {
		info PhysicalNaturalSizingInfo
		want geometry.PhysicalSize
	}{
		{PhysicalNaturalSizingInfo{AspectRatio: ratio}, geometry.NewPhysicalSize(300, 150)},
		{PhysicalNaturalSizingInfo{AspectRatio: geometry.NewPhysicalSize(1, 2)}, geometry.NewPhysicalSize(75, 150)},
		{PhysicalNaturalSizingInfo{Size: geometry.NewPhysicalSize(40, 0), AspectRatio: ratio, HasWidth: true}, geometry.NewPhysicalSize(40, 20)},
		{PhysicalNaturalSizingInfo{Size: geometry.NewPhysicalSize(0, 40), HasHeight: true}, geometry.NewPhysicalSize(300, 40)},
		{PhysicalNaturalSizingInfo{}, def},
	} {
		if got := ConcreteObjectSize(tc.info, def); got != tc.want {
			t.Errorf("ConcreteObjectSize(%+v) = %+v, want %+v", tc.info, got, tc.want)
		}
	}
}

// LayoutReplaced::ClipsToContentBox (layout_replaced.cc:621-640 @
// a9f50e522efa9005e6ec765a9a785c74f5c2c86b), RespectsCSSOverflow branch.
func TestClipsToContentBox(t *testing.T) {
	content := css.OverflowClipMargin{Box: css.OverflowClipMarginContentBox}
	if !ClipsToContentBox(true, content) {
		t.Error("overflow:clip + overflow-clip-margin:content-box clips to the content box")
	}
	if ClipsToContentBox(false, content) {
		t.Error("overflow:visible does not clip")
	}
	if ClipsToContentBox(true, css.OverflowClipMargin{Box: css.OverflowClipMarginContentBox, Length: 2}) {
		t.Error("a non-zero overflow-clip-margin does not clip to the content box")
	}
	if ClipsToContentBox(true, css.OverflowClipMargin{Box: css.OverflowClipMarginPaddingBox}) {
		t.Error("a padding-box reference box (or no overflow-clip-margin) does not clip to the content box")
	}
}
