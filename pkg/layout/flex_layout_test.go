package layout

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"louis14/pkg/css"
	"louis14/pkg/html"
	"testing"
)

// fakeImagePNG encodes an opaque-green PNG of the given pixel dimensions.
// Used to give an <img> flex item a deterministic intrinsic size + aspect
// ratio in layout-level tests without touching the filesystem.
func fakeImagePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{0, 128, 0, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// imgContextWithIntrinsic returns a LayoutContext whose ImageFetcher serves a
// PNG of the given natural dimensions for any URI, so GetIntrinsicSizingInfo
// reports the intended aspect ratio.
func imgContextWithIntrinsic(natW, natH int) *LayoutContext {
	data := fakeImagePNG(natW, natH)
	return &LayoutContext{
		ViewportWidth:  800,
		ViewportHeight: 600,
		ImageFetcher:   func(string) ([]byte, error) { return data, nil },
	}
}

// makeImgNode builds an <img> element node with a non-data src so the
// LayoutContext's ImageFetcher supplies its intrinsic dimensions.
func makeImgNode(src string) *html.Node {
	n := makeNode("img")
	n.Attributes = map[string]string{"src": src}
	return n
}

// findFragmentByNode walks the fragment tree and returns the first fragment
// whose layout node wraps the given DOM node, along with its size.
func findFragmentByNode(frag *PhysicalFragment, target *html.Node) *PhysicalFragment {
	if frag.LayoutNode != nil && frag.LayoutNode.DOMNode == target {
		return frag
	}
	for _, ch := range frag.Children {
		if found := findFragmentByNode(ch.Fragment, target); found != nil {
			return found
		}
	}
	return nil
}

// TestFlexLayout_RowMaxHeightClampsLineForPercentResolution mirrors WPT
// css-flexbox/flexbox-definite-sizes-004: a row flex container with
// max-height (no explicit height) must clamp its single flex line's
// cross-size BEFORE the §9.8 re-layout pass, so a child's percentage
// max-height resolves against the clamped (definite) size.
//
// Per css-flexbox §9.4 step 8, the single line's cross-size is clamped to
// the container's min/max cross sizes.  Blink applies this by computing
// total_block_size_ (min/max-clamped, flex_layout_algorithm.cc:1266 @
// 4883d11f) before GiveItemsFinalPositionAndSize sets the single line's
// cross size to it (:1741) and re-lays-out items against it.
//
// DOM: flex(row, max-height:100px, align-items:flex-end)
//
//	> block(max-height:100%) > inner(height:9999px)
//
// Buggy behavior: the line cross-size seen by the re-layout pass is the
// unclamped content size (9999), so max-height:100% resolves against 9999
// and block stays 9999 tall.  Expected: container 100 tall, block 100 tall.
func TestFlexLayout_RowMaxHeightClampsLineForPercentResolution(t *testing.T) {
	inner := makeNode("div")
	block := makeNode("div", inner)
	flex := makeNode("div", block)

	styles := map[*html.Node]*css.Style{
		flex:  makeStyle("display", "flex", "width", "100px", "max-height", "100px", "align-items", "flex-end"),
		block: makeStyle("display", "block", "width", "100px", "max-height", "100%"),
		inner: makeStyle("display", "block", "height", "9999px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("flex container height: got %.1f, want 100 (max-height clamp)", got)
	}
	blockFrag := findFragmentByNode(result.Fragment, block)
	if blockFrag == nil {
		t.Fatal("block fragment not found")
	}
	if got := blockFrag.Size.HeightF64(); got != 100 {
		t.Errorf("block height: got %.1f, want 100 (max-height:100%% must resolve against the clamped 100px line cross-size)", got)
	}
}

// TestFlexLayout_OuterMaxHeightStretchMakesItemCrossDefinite mirrors WPT
// css-flexbox/flexbox-definite-sizes-003: the max-height sits on the OUTER
// flex container; the inner flex container is a default align-items:stretch
// item, so it must stretch to the clamped 100px line and present that as a
// definite height to ITS child's max-height:100%.
//
// DOM: outer(row flex, max-height:100px)
//
//	> innerFlex(row flex, align-items:flex-end)
//	  > block(max-height:100%) > inner(height:9999px)
func TestFlexLayout_OuterMaxHeightStretchMakesItemCrossDefinite(t *testing.T) {
	inner := makeNode("div")
	block := makeNode("div", inner)
	innerFlex := makeNode("div", block)
	outer := makeNode("div", innerFlex)

	styles := map[*html.Node]*css.Style{
		outer:     makeStyle("display", "flex", "width", "100px", "max-height", "100px"),
		innerFlex: makeStyle("display", "flex", "width", "100px", "align-items", "flex-end"),
		block:     makeStyle("display", "block", "width", "100px", "max-height", "100%"),
		inner:     makeStyle("display", "block", "height", "9999px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(outer, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("outer flex height: got %.1f, want 100 (max-height clamp)", got)
	}
	innerFrag := findFragmentByNode(result.Fragment, innerFlex)
	if innerFrag == nil {
		t.Fatal("inner flex fragment not found")
	}
	if got := innerFrag.Size.HeightF64(); got != 100 {
		t.Errorf("inner flex height: got %.1f, want 100 (stretch to clamped line)", got)
	}
	blockFrag := findFragmentByNode(result.Fragment, block)
	if blockFrag == nil {
		t.Fatal("block fragment not found")
	}
	if got := blockFrag.Size.HeightF64(); got != 100 {
		t.Errorf("block height: got %.1f, want 100 (max-height:100%% against stretched definite 100px)", got)
	}
}

// TestFlexLayout_RowMinHeightExpandsLineForStretch covers the clamp-UP
// direction of §9.4 step 8: a row flex container with min-height (no
// explicit height) and short content must expand its single line to the
// min-height, and a default align-items:stretch item must stretch to it.
// A flex-start sibling keeps its content height, pinning that only the
// line (not every item) grows.
func TestFlexLayout_RowMinHeightExpandsLineForStretch(t *testing.T) {
	stretchItem := makeNode("div")
	startItem := makeNode("div")
	flex := makeNode("div", stretchItem, startItem)

	styles := map[*html.Node]*css.Style{
		flex:        makeStyle("display", "flex", "width", "100px", "min-height", "100px"),
		stretchItem: makeStyle("display", "block", "width", "50px", "height", "auto"),
		startItem:   makeStyle("display", "block", "width", "50px", "height", "30px", "align-self", "flex-start"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("flex container height: got %.1f, want 100 (min-height expands the line)", got)
	}
	stretchFrag := findFragmentByNode(result.Fragment, stretchItem)
	if stretchFrag == nil {
		t.Fatal("stretch item fragment not found")
	}
	if got := stretchFrag.Size.HeightF64(); got != 100 {
		t.Errorf("stretch item height: got %.1f, want 100 (stretch to min-height-expanded line)", got)
	}
	startFrag := findFragmentByNode(result.Fragment, startItem)
	if startFrag == nil {
		t.Fatal("flex-start item fragment not found")
	}
	if got := startFrag.Size.HeightF64(); got != 30 {
		t.Errorf("flex-start item height: got %.1f, want 30 (non-stretch item keeps content height)", got)
	}
}

// TestFlexLayout_WrapLinesNotClampedByMaxHeight guards that the §9.4 step 8
// clamp applies to single-line (nowrap) containers ONLY: in a wrapping
// container, individual flex lines keep their content-derived cross sizes
// even when the container's max-height clamps the container box itself.
func TestFlexLayout_WrapLinesNotClampedByMaxHeight(t *testing.T) {
	item1 := makeNode("div")
	item2 := makeNode("div")
	flex := makeNode("div", item1, item2)

	styles := map[*html.Node]*css.Style{
		flex:  makeStyle("display", "flex", "flex-wrap", "wrap", "width", "100px", "max-height", "50px"),
		item1: makeStyle("display", "block", "width", "60px", "height", "60px"),
		item2: makeStyle("display", "block", "width", "60px", "height", "60px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	// 60px items don't fit side-by-side in 100px → two lines of 60px each.
	// The container box clamps to max-height:50px; the lines (and items)
	// keep their 60px content sizes and overflow.
	if got := result.Fragment.Size.HeightF64(); got != 50 {
		t.Errorf("flex container height: got %.1f, want 50 (max-height clamps the box)", got)
	}
	for i, n := range []*html.Node{item1, item2} {
		frag := findFragmentByNode(result.Fragment, n)
		if frag == nil {
			t.Fatalf("item%d fragment not found", i+1)
		}
		if got := frag.Size.HeightF64(); got != 60 {
			t.Errorf("item%d height: got %.1f, want 60 (wrapped lines are not clamped)", i+1, got)
		}
	}
}

// TestFlexLayout_RowMinHeightWinsOverMaxHeight pins CSS 2.1 §10.7: when
// min-height > max-height, max-height is clamped up to min-height (apply
// max first, then min — min wins).  Matches the column main-axis clamp in
// this file and Blink's ClampSizeToMinAndMax.
func TestFlexLayout_RowMinHeightWinsOverMaxHeight(t *testing.T) {
	stretchItem := makeNode("div")
	flex := makeNode("div", stretchItem)

	styles := map[*html.Node]*css.Style{
		flex:        makeStyle("display", "flex", "width", "100px", "min-height", "100px", "max-height", "50px"),
		stretchItem: makeStyle("display", "block", "width", "100px", "height", "auto"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 100 {
		t.Errorf("flex container height: got %.1f, want 100 (min-height wins over smaller max-height)", got)
	}
	stretchFrag := findFragmentByNode(result.Fragment, stretchItem)
	if stretchFrag == nil {
		t.Fatal("stretch item fragment not found")
	}
	if got := stretchFrag.Size.HeightF64(); got != 100 {
		t.Errorf("stretch item height: got %.1f, want 100 (line cross-size must clamp max-first-then-min, so min-height wins)", got)
	}
}

// TestFlexLayout_RowMaxHeightDoesNotShrinkSmallContent guards the other
// direction: max-height must only CLAMP — a row flex container whose content
// is shorter than max-height keeps its content height, and the line is not
// inflated to the max-height value.  (Blink: total_block_size_ resolves the
// intrinsic size through min/max; it does not substitute the max.)
func TestFlexLayout_RowMaxHeightDoesNotShrinkSmallContent(t *testing.T) {
	item := makeNode("div")
	flex := makeNode("div", item)

	styles := map[*html.Node]*css.Style{
		flex: makeStyle("display", "flex", "width", "100px", "max-height", "100px"),
		item: makeStyle("display", "block", "width", "100px", "height", "30px"),
	}

	ctx := testContext()
	layoutRoot := buildTestTree(flex, styles)
	wdm := WritingDirectionMode{WritingModeHorizontalTB, DirectionLTR}
	space := NewConstraintSpaceBuilder(wdm, wdm, true).
		SetAvailableSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		SetPercentageResolutionSize(LogicalSize{InlineSize: 800, BlockSize: 600}).
		Build()

	result := layoutElement(ctx, layoutRoot, space)

	if got := result.Fragment.Size.HeightF64(); got != 30 {
		t.Errorf("flex container height: got %.1f, want 30 (content below max-height must not grow)", got)
	}
	itemFrag := findFragmentByNode(result.Fragment, item)
	if itemFrag == nil {
		t.Fatal("item fragment not found")
	}
	if got := itemFrag.Size.HeightF64(); got != 30 {
		t.Errorf("item height: got %.1f, want 30", got)
	}
}

// TestFlexLayout_ColumnImgPercentMainSizeTransfersThroughAspectRatio mirrors
// WPT css-flexbox/flex-aspect-ratio-img-column-004: a column flex container
// with no definite main size (only min-height) holds a replaced <img>
// (intrinsic 300×150, ratio 2:1) sized width:100% height:100%. The img's
// percentage main-size (height:100%) is unresolvable because the container's
// main size is indefinite, so per CSS Flexbox §9.2 / Blink's
// resolve_main_length() it must FALL BACK to the content (transferred) size:
// the definite cross-size (width:100% = 100px) carried through the aspect
// ratio yields a main (block) size of 50px.
//
// Blink: flex_layout_algorithm.cc ConstructAndAppendFlexItems →
// resolve_main_length() ("we weren't able to resolve the length … fallback to
// the max-content size") @ Chromium main 3da458a33174a6422f35b4e603f4090f028670ae.
//
// Buggy behavior: resolveFlexBasis passes a DEFINITE zero as the percentage
// resolution block-size, so height:100% resolves to 0 (ok=true) and short-
// circuits before the aspect-ratio fallback — the img collapses to 100×0.
func TestFlexLayout_ColumnImgPercentMainSizeTransfersThroughAspectRatio(t *testing.T) {
	img := makeImgNode("col004.png")
	flex := makeNode("div", img)

	styles := map[*html.Node]*css.Style{
		flex: makeStyle("display", "flex", "width", "100px", "min-height", "500px", "flex-direction", "column"),
		img:  makeStyle("max-width", "100%", "height", "100%", "width", "100%"),
	}

	ctx := imgContextWithIntrinsic(300, 150)
	layoutRoot := buildTestTree(flex, styles)
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
	if got := imgFrag.Size.WidthF64(); got != 100 {
		t.Errorf("img width: got %.1f, want 100 (width:100%%)", got)
	}
	if got := imgFrag.Size.HeightF64(); got != 50 {
		t.Errorf("img height: got %.1f, want 50 (100px width transferred through 2:1 aspect ratio)", got)
	}
}

// TestFlexLayout_RowImgMaxHeightTransfersThroughAspectRatio mirrors WPT
// css-flexbox/flex-aspect-ratio-img-row-016: a row flex container 100×100
// holds a replaced <img> (intrinsic 1000×1000, ratio 1:1) with min-width:0
// and max-height:100%. The max-height resolves against the container's
// definite cross-size (100px) and transfers through the aspect ratio to clamp
// the main (inline) size to 100px — so the img is 100×100, not its 1000px
// max-content width.
//
// Blink: flex_layout_algorithm.cc ComputeTransferredMinMaxBlockSizes carries
// the block-axis max constraint through LogicalAspectRatio() into the inline
// (main) size @ Chromium main 3da458a33174a6422f35b4e603f4090f028670ae.
//
// Buggy behavior: itemMaxContentMainSize builds its cross constraint space
// without a percentage-resolution block-size, so max-height:100% resolves to
// 0, clamping the block size to 0 and collapsing the img to 0×0.
func TestFlexLayout_RowImgMaxHeightTransfersThroughAspectRatio(t *testing.T) {
	img := makeImgNode("row016.png")
	flex := makeNode("div", img)

	styles := map[*html.Node]*css.Style{
		flex: makeStyle("display", "flex", "width", "100px", "height", "100px", "align-items", "start"),
		img:  makeStyle("min-width", "0px", "max-height", "100%"),
	}

	ctx := imgContextWithIntrinsic(1000, 1000)
	layoutRoot := buildTestTree(flex, styles)
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
	if got := imgFrag.Size.HeightF64(); got != 100 {
		t.Errorf("img height: got %.1f, want 100 (max-height:100%% of 100px container)", got)
	}
	if got := imgFrag.Size.WidthF64(); got != 100 {
		t.Errorf("img width: got %.1f, want 100 (100px height transferred through 1:1 aspect ratio)", got)
	}
}
