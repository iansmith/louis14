package layout

import (
	"encoding/base64"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// TestImgDataURIIntrinsicSizeWithoutFetcher: an <img> whose src is a data:
// URI and which has no width/height must take its natural size from the
// decoded image even when the LayoutContext has no ImageFetcher. data: URIs
// are self-contained, so the fetcher is irrelevant (images.LoadImageWithFetcher
// decodes them without calling it), and the renderer paints them without a
// fetcher too.
//
// Buggy behavior: getImgIntrinsicInfo returned no intrinsic size whenever
// ImageFetcher was nil, so the img fell back to the CSS 2.1 §10.3.2 default
// 300×150 box and the painted image was stretched into it.
func TestImgDataURIIntrinsicSizeWithoutFetcher(t *testing.T) {
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(fakeImagePNG(40, 20))
	img := makeImgNode(src)
	container := makeNode("div", img)

	styles := map[*html.Node]*css.Style{
		container: makeStyle("display", "block"),
		img:       makeStyle(),
	}

	ctx := &LayoutContext{ViewportWidth: 800, ViewportHeight: 600} // no ImageFetcher
	layoutRoot := buildTestTree(container, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)
	imgFrag := findFragmentByNode(result.Fragment, img)
	if imgFrag == nil {
		t.Fatal("img fragment not found")
	}
	if got := imgFrag.Size.WidthF64(); got != 40 {
		t.Errorf("img width: got %.1f, want 40 (natural width of the data: image)", got)
	}
	if got := imgFrag.Size.HeightF64(); got != 20 {
		t.Errorf("img height: got %.1f, want 20 (natural height of the data: image)", got)
	}
}
